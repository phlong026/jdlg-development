package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

// UserRedisModel 用户Redis操作层
type UserRedisModel struct {
	DB    *sql.DB
	Redis *redis.Client
	ctx   context.Context
}

// NewUserRedisModel 创建用户Redis模型
func NewUserRedisModel(db *sql.DB, rdb *redis.Client) *UserRedisModel {
	return &UserRedisModel{
		DB:    db,
		Redis: rdb,
		ctx:   context.Background(),
	}
}

// ErrInsufficientStones 灵石余额不足，由 DeductStones 在原子扣费失败时返回
var ErrInsufficientStones = errors.New("余额不足")

// LoadStonesFromMySQL 从MySQL加载所有用户余额到Redis
func (m *UserRedisModel) LoadStonesFromMySQL() error {
	query := `SELECT id, COALESCE(stones, 0) as stones FROM users`
	rows, err := m.DB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var userID int64
		var stones int64
		if err := rows.Scan(&userID, &stones); err != nil {
			continue
		}

		// 存储到Redis
		key := "user:stones:" + strconv.FormatInt(userID, 10)
		if err := m.Redis.Set(m.ctx, key, stones, 0).Err(); err != nil {
			log.Printf("save user stones to redis failed: %v", err)
			continue
		}
		count++
	}

	log.Printf("从 MySQL 加载了 %d 条用户灵石余额至 Redis", count)
	return nil
}

// GetStones 获取用户灵石余额（从Redis）
func (m *UserRedisModel) GetStones(userID int64) (int64, error) {
	key := "user:stones:" + strconv.FormatInt(userID, 10)
	val, err := m.Redis.Get(m.ctx, key).Result()
	if err == redis.Nil {
		// Redis中没有，从MySQL加载
		var stones int64
		err := m.DB.QueryRow("SELECT COALESCE(stones, 0) FROM users WHERE id = ?", userID).Scan(&stones)
		if err != nil {
			return 0, err
		}
		// 写入Redis
		m.Redis.Set(m.ctx, key, stones, 0)
		return stones, nil
	}
	if err != nil {
		return 0, err
	}

	stones, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, err
	}
	return stones, nil
}

// DeductStones 扣除用户灵石（原子操作，先检查余额，再扣费）
func (m *UserRedisModel) DeductStones(userID int64, amount int64) error {
	key := "user:stones:" + strconv.FormatInt(userID, 10)

	// 使用Lua脚本保证原子性
	luaScript := `
		local current = redis.call('GET', KEYS[1])
		if not current then
			return {err = 'user_not_found'}
		end
		current = tonumber(current)
		if current < tonumber(ARGV[1]) then
			return {err = 'insufficient_balance'}
		end
		local new_balance = current - tonumber(ARGV[1])
		redis.call('SET', KEYS[1], new_balance)
		return {ok = new_balance}
	`

	result, err := m.Redis.Eval(m.ctx, luaScript, []string{key}, amount).Result()
	if err != nil {
		// go-redis 会把 Lua 的 {err='xxx'} 作为一个普通 error 抛出，
		// 这里需要识别 insufficient_balance 并映射成 ErrInsufficientStones，
		// 以便调用方用 errors.Is 判定余额不足。
		if strings.Contains(err.Error(), "insufficient_balance") {
			return ErrInsufficientStones
		}
		return err
	}

	// Lua 返回 {ok=new_balance} 时，go-redis 会解析成普通 int/string；
	// 但在某些情况下也可能返回 []interface{}。这里只做兜底识别，主路径走上面的 err 分支。
	if resultMap, ok := result.([]interface{}); ok {
		if len(resultMap) > 1 {
			if errStr, ok := resultMap[0].(string); ok && errStr == "err" {
				errMsg, _ := resultMap[1].(string)
				if errMsg == "insufficient_balance" {
					return ErrInsufficientStones
				}
				return errors.New(errMsg)
			}
		}
	}

	// 异步同步到MySQL
	go m.syncStonesToMySQL(userID)

	return nil
}

// AddStones 增加用户灵石
func (m *UserRedisModel) AddStones(userID int64, amount int64) error {
	key := "user:stones:" + strconv.FormatInt(userID, 10)
	_, err := m.Redis.IncrBy(m.ctx, key, amount).Result()
	if err != nil {
		return err
	}

	// 异步同步到MySQL
	go m.syncStonesToMySQL(userID)
	return nil
}

// SetStones 直接设置用户灵石余额（管理员操作）
func (m *UserRedisModel) SetStones(userID int64, amount int64) error {
	key := "user:stones:" + strconv.FormatInt(userID, 10)

	// 与异步同步共用同一把用户锁，避免"设置"与"异步同步"交错写 MySQL
	mu := lockStonesSync(userID)
	mu.Lock()
	defer mu.Unlock()

	// 直接设置新余额到Redis
	if err := m.Redis.Set(m.ctx, key, amount, 0).Err(); err != nil {
		return err
	}

	// 同步到MySQL（同步执行以确保数据一致性）
	query := `UPDATE users SET stones = ? WHERE id = ?`
	_, err := m.DB.Exec(query, amount, userID)
	if err != nil {
		// 如果MySQL更新失败，记录日志但不回滚Redis（管理员可以重试）
		log.Printf("sync stones to MySQL failed: %v", err)
		return err
	}

	return nil
}

// syncStonesToMySQL 异步同步余额到MySQL。
//
// 同一用户的同步必须串行且写入前重读 Redis 最新值：多个 goroutine 并发
// "读 Redis → 写 MySQL" 时，慢的那个会用自己的旧读值覆盖刚写入的新值，
// 服务重启回填（LoadStonesFromMySQL）后表现为用户余额回退。
// 进程内按用户加锁 + 锁内重读，保证后执行的同步一定写入更新的值。
var stonesSyncLocks sync.Map // map[int64]*sync.Mutex

func lockStonesSync(userID int64) *sync.Mutex {
	muAny, _ := stonesSyncLocks.LoadOrStore(userID, &sync.Mutex{})
	return muAny.(*sync.Mutex)
}

func (m *UserRedisModel) syncStonesToMySQL(userID int64) {
	if m.DB == nil {
		return
	}
	mu := lockStonesSync(userID)
	mu.Lock()
	defer mu.Unlock()

	key := "user:stones:" + strconv.FormatInt(userID, 10)
	val, err := m.Redis.Get(m.ctx, key).Result()
	if err != nil {
		log.Printf("get stones from redis failed: %v", err)
		return
	}

	stones, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		log.Printf("parse stones failed: %v", err)
		return
	}

	query := `UPDATE users SET stones = ? WHERE id = ?`
	_, err = m.DB.Exec(query, stones, userID)
	if err != nil {
		log.Printf("sync stones to MySQL failed: %v", err)
	}
}

// InitStonesColumn 初始化用户表的stones字段
func (m *UserRedisModel) InitStonesColumn() error {
	// 先检查字段是否存在
	var exists int
	checkQuery := `SELECT COUNT(*) FROM information_schema.COLUMNS 
	               WHERE TABLE_SCHEMA = DATABASE() 
	               AND TABLE_NAME = 'users' 
	               AND COLUMN_NAME = 'stones'`
	err := m.DB.QueryRow(checkQuery).Scan(&exists)
	if err != nil {
		return fmt.Errorf("检查stones字段失败: %v", err)
	}

	// 如果字段不存在，则添加
	if exists == 0 {
		query := `ALTER TABLE users ADD COLUMN stones BIGINT UNSIGNED NOT NULL DEFAULT 50`
		_, err := m.DB.Exec(query)
		if err != nil {
			return fmt.Errorf("添加stones字段失败: %v", err)
		}
		log.Println("用户表 stones 字段添加成功")
	} else {
		query := `ALTER TABLE users MODIFY COLUMN stones BIGINT UNSIGNED NOT NULL DEFAULT 50`
		_, err := m.DB.Exec(query)
		if err != nil {
			return fmt.Errorf("更新stones字段默认值失败: %v", err)
		}
		log.Println("用户表 stones 字段默认值已更新为 50")
	}
	return nil
}
