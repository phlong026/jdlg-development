package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"
	"github.com/redis/go-redis/v9"
)

// CodeSessionRedisModel Redis操作层
type CodeSessionRedisModel struct {
	DB    *sql.DB
	Redis *redis.Client
	ctx   context.Context
}

// NewCodeSessionRedisModel 创建Redis模型
func NewCodeSessionRedisModel(db *sql.DB, rdb *redis.Client) *CodeSessionRedisModel {
	return &CodeSessionRedisModel{
		DB:    db,
		Redis: rdb,
		ctx:   context.Background(),
	}
}

// LoadFromMySQL 从MySQL加载所有数据到Redis
func (m *CodeSessionRedisModel) LoadFromMySQL() error {
	query := `SELECT id, code, device_id, session_id, user_id, is_banned, created_at, updated_at 
	          FROM code_sessions`
	rows, err := m.DB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var cs CodeSession
		err := rows.Scan(
			&cs.ID, &cs.Code, &cs.DeviceID, &cs.SessionID, &cs.UserID, &cs.IsBanned,
			&cs.CreatedAt, &cs.UpdatedAt,
		)
		if err != nil {
			continue
		}

		// 存储到Redis
		if err := m.saveToRedis(&cs); err != nil {
			log.Printf("save to redis failed: %v", err)
			continue
		}
		count++
	}

	log.Printf("从 MySQL 加载了 %d 条 code_sessions 至 Redis", count)
	return nil
}

// saveToRedis 保存到Redis
func (m *CodeSessionRedisModel) saveToRedis(cs *CodeSession) error {
	data, err := json.Marshal(cs)
	if err != nil {
		return err
	}

	// 使用Hash存储，key为code、device_id和session_id
	pipe := m.Redis.Pipeline()
	pipe.HSet(m.ctx, "code_session:code:"+cs.Code, "data", data)
	pipe.HSet(m.ctx, "code_session:session:"+cs.SessionID, "data", data)
	if cs.DeviceID != "" {
		pipe.HSet(m.ctx, "code_session:device:"+cs.DeviceID, "data", data)
		pipe.HSet(m.ctx, "code_session:device:"+cs.DeviceID, "is_banned", cs.IsBanned)
		pipe.HSet(m.ctx, "code_session:device:"+cs.DeviceID, "user_id", cs.UserID)
		pipe.HSet(m.ctx, "code_session:device:"+cs.DeviceID, "session_id", cs.SessionID)
		pipe.HSet(m.ctx, "code_session:device:"+cs.DeviceID, "code", cs.Code)
	}
	pipe.HSet(m.ctx, "code_session:code:"+cs.Code, "is_banned", cs.IsBanned)
	pipe.HSet(m.ctx, "code_session:session:"+cs.SessionID, "is_banned", cs.IsBanned)
	pipe.HSet(m.ctx, "code_session:code:"+cs.Code, "user_id", cs.UserID)
	pipe.HSet(m.ctx, "code_session:session:"+cs.SessionID, "user_id", cs.UserID)
	pipe.HSet(m.ctx, "code_session:code:"+cs.Code, "session_id", cs.SessionID)
	pipe.HSet(m.ctx, "code_session:session:"+cs.SessionID, "code", cs.Code)
	_, err = pipe.Exec(m.ctx)
	return err
}

// GetByCode 从Redis获取
func (m *CodeSessionRedisModel) GetByCode(code string) (*CodeSession, error) {
	data, err := m.Redis.HGet(m.ctx, "code_session:code:"+code, "data").Result()
	if err == redis.Nil {
		return nil, errors.New("not found")
	}
	if err != nil {
		return nil, err
	}

	var cs CodeSession
	if err := json.Unmarshal([]byte(data), &cs); err != nil {
		return nil, err
	}
	return m.normalizeEffectiveUserID(&cs)
}

// GetBySessionID 从Redis获取
func (m *CodeSessionRedisModel) GetBySessionID(sessionID string) (*CodeSession, error) {
	data, err := m.Redis.HGet(m.ctx, "code_session:session:"+sessionID, "data").Result()
	if err == redis.Nil {
		return nil, errors.New("not found")
	}
	if err != nil {
		return nil, err
	}

	var cs CodeSession
	if err := json.Unmarshal([]byte(data), &cs); err != nil {
		return nil, err
	}
	return m.normalizeEffectiveUserID(&cs)
}

// GetByDeviceID 从Redis获取（优先使用设备指纹）
func (m *CodeSessionRedisModel) GetByDeviceID(deviceID string) (*CodeSession, error) {
	if deviceID == "" {
		return nil, errors.New("device_id is empty")
	}
	data, err := m.Redis.HGet(m.ctx, "code_session:device:"+deviceID, "data").Result()
	if err == redis.Nil {
		return nil, errors.New("not found")
	}
	if err != nil {
		return nil, err
	}

	var cs CodeSession
	if err := json.Unmarshal([]byte(data), &cs); err != nil {
		return nil, err
	}
	return m.normalizeEffectiveUserID(&cs)
}

func (m *CodeSessionRedisModel) normalizeEffectiveUserID(cs *CodeSession) (*CodeSession, error) {
	if cs == nil || cs.UserID <= 0 {
		return cs, nil
	}
	effectiveUserID, err := NewUserModel(m.DB).ResolveMergedUserID(cs.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cs, nil
		}
		return nil, err
	}
	if effectiveUserID == cs.UserID {
		return cs, nil
	}
	cs.UserID = effectiveUserID
	cs.UpdatedAt = time.Now()
	if err := m.saveToRedis(cs); err != nil {
		return nil, err
	}
	go m.syncToMySQL(cs, "rebind_user")
	return cs, nil
}

// Create 创建并保存到Redis，异步同步到MySQL
func (m *CodeSessionRedisModel) Create(cs *CodeSession) error {
	// 如果ID为0，需要先获取一个ID（从MySQL获取自增ID）
	if cs.ID == 0 {
		// 设置时间字段
		now := time.Now()
		if cs.CreatedAt.IsZero() {
			cs.CreatedAt = now
		}
		if cs.UpdatedAt.IsZero() {
			cs.UpdatedAt = now
		}
		
		// 先插入MySQL获取ID
		query := `INSERT INTO code_sessions (code, device_id, session_id, user_id, is_banned, created_at, updated_at) 
		          VALUES (?, ?, ?, ?, ?, ?, ?)`
		result, err := m.DB.Exec(query, cs.Code, cs.DeviceID, cs.SessionID, cs.UserID, cs.IsBanned, cs.CreatedAt, cs.UpdatedAt)
		if err != nil {
			// 如果是因为唯一键冲突，尝试查询
			existing, err2 := m.GetByCode(cs.Code)
			if err2 == nil {
				cs.ID = existing.ID
				cs.CreatedAt = existing.CreatedAt
				cs.UpdatedAt = existing.UpdatedAt
			} else {
				return err
			}
		} else {
			id, _ := result.LastInsertId()
			cs.ID = id
		}
	} else {
		// 如果ID已存在，确保时间字段已设置
		now := time.Now()
		if cs.CreatedAt.IsZero() {
			cs.CreatedAt = now
		}
		if cs.UpdatedAt.IsZero() {
			cs.UpdatedAt = now
		}
	}

	// 保存到Redis
	if err := m.saveToRedis(cs); err != nil {
		return err
	}

	// 异步同步到MySQL（使用UPSERT确保数据一致性）
	go m.syncToMySQL(cs, "create")
	return nil
}

// BanSession 封禁session，操作Redis，异步同步到MySQL
func (m *CodeSessionRedisModel) BanSession(sessionID string) error {
	// 从Redis获取
	cs, err := m.GetBySessionID(sessionID)
	if err != nil {
		return err
	}

	// 更新Redis
	cs.IsBanned = true
	if err := m.saveToRedis(cs); err != nil {
		return err
	}

	// 异步同步到MySQL
	go m.syncToMySQL(cs, "update")
	return nil
}

// UnbanSession 解封session，操作Redis，异步同步到MySQL
func (m *CodeSessionRedisModel) UnbanSession(sessionID string) error {
	// 从Redis获取
	cs, err := m.GetBySessionID(sessionID)
	if err != nil {
		return err
	}

	// 更新Redis
	cs.IsBanned = false
	if err := m.saveToRedis(cs); err != nil {
		return err
	}

	// 异步同步到MySQL
	go m.syncToMySQL(cs, "update")
	return nil
}

// Delete 删除指定 code_session 在 Redis 中的全部 key（用于用户已删除等无效 session）
func (m *CodeSessionRedisModel) Delete(cs *CodeSession) error {
	if cs == nil {
		return nil
	}
	keys := []string{
		"code_session:code:" + cs.Code,
		"code_session:session:" + cs.SessionID,
	}
	if cs.DeviceID != "" {
		keys = append(keys, "code_session:device:"+cs.DeviceID)
	}
	return m.Redis.Del(m.ctx, keys...).Err()
}

// syncToMySQL 异步同步到MySQL
func (m *CodeSessionRedisModel) syncToMySQL(cs *CodeSession, operation string) {
	switch operation {
	case "create":
		// 确保时间字段有效
		now := time.Now()
		createdAt := cs.CreatedAt
		updatedAt := cs.UpdatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		if updatedAt.IsZero() {
			updatedAt = now
		}
		
		query := `INSERT INTO code_sessions (id, code, device_id, session_id, user_id, is_banned, created_at, updated_at) 
		          VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		          ON DUPLICATE KEY UPDATE 
		          device_id = VALUES(device_id),
		          session_id = VALUES(session_id),
		          user_id = VALUES(user_id),
		          is_banned = VALUES(is_banned),
		          updated_at = VALUES(updated_at)`
		_, err := m.DB.Exec(query, cs.ID, cs.Code, cs.DeviceID, cs.SessionID, cs.UserID, cs.IsBanned, createdAt, updatedAt)
		if err != nil {
			log.Printf("sync create to MySQL failed: %v", err)
		}
	case "update":
		query := `UPDATE code_sessions SET is_banned = ?, updated_at = ? WHERE session_id = ?`
		_, err := m.DB.Exec(query, cs.IsBanned, time.Now(), cs.SessionID)
		if err != nil {
			log.Printf("sync update to MySQL failed: %v", err)
		}
	case "rebind_user":
		query := `UPDATE code_sessions SET user_id = ?, updated_at = ? WHERE session_id = ?`
		_, err := m.DB.Exec(query, cs.UserID, time.Now(), cs.SessionID)
		if err != nil {
			log.Printf("sync rebind user to MySQL failed: %v", err)
		}
	}
}

// InitTable 初始化code_sessions表（仅用于首次创建表）
func (m *CodeSessionRedisModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS code_sessions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	code VARCHAR(128) NOT NULL,
	device_id VARCHAR(128) DEFAULT NULL,
	session_id VARCHAR(64) NOT NULL,
	user_id BIGINT UNSIGNED NOT NULL,
	is_banned TINYINT(1) NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_code (code),
	UNIQUE KEY uk_session_id (session_id),
	INDEX idx_device_id (device_id),
	INDEX idx_user_id (user_id),
	INDEX idx_is_banned (is_banned)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}

	// 检查并添加device_id字段（如果表已存在但没有该字段）
	err = m.migrateDeviceIDColumn()
	if err != nil {
		log.Printf("迁移device_id字段警告: %v（继续运行）", err)
	}

	return nil
}

// migrateDeviceIDColumn 迁移添加device_id字段
func (m *CodeSessionRedisModel) migrateDeviceIDColumn() error {
	// 检查device_id字段是否存在
	checkQuery := `SELECT COUNT(*) FROM information_schema.COLUMNS 
	               WHERE TABLE_SCHEMA = DATABASE() 
	               AND TABLE_NAME = 'code_sessions' 
	               AND COLUMN_NAME = 'device_id'`
	var count int
	err := m.DB.QueryRow(checkQuery).Scan(&count)
	if err != nil {
		return err
	}

	// 如果字段不存在，添加它
	if count == 0 {
		alterQuery := `ALTER TABLE code_sessions ADD COLUMN device_id VARCHAR(128) DEFAULT NULL`
		_, err = m.DB.Exec(alterQuery)
		if err != nil {
			return err
		}
		log.Println("已添加device_id字段到code_sessions表")
	}

	// 检查并添加device_id索引
	indexCheckQuery := `SELECT COUNT(*) FROM information_schema.STATISTICS 
	                   WHERE TABLE_SCHEMA = DATABASE() 
	                   AND TABLE_NAME = 'code_sessions' 
	                   AND INDEX_NAME = 'idx_device_id'`
	err = m.DB.QueryRow(indexCheckQuery).Scan(&count)
	if err != nil {
		return err
	}

	// 如果索引不存在，添加它
	if count == 0 {
		indexQuery := `ALTER TABLE code_sessions ADD INDEX idx_device_id (device_id)`
		_, err = m.DB.Exec(indexQuery)
		if err != nil {
			return err
		}
		log.Println("已添加device_id索引到code_sessions表")
	}

	return nil
}
