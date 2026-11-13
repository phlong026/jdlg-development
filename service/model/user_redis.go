package model

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/redis/go-redis/v9"
)

// UserRedisModel retains the legacy API; MySQL is the only wallet authority.
type UserRedisModel struct {
	DB    *sql.DB
	Redis *redis.Client
}

func NewUserRedisModel(db *sql.DB, rdb *redis.Client) *UserRedisModel {
	return &UserRedisModel{DB: db, Redis: rdb}
}

var ErrInsufficientStones = errors.New("余额不足")
var ErrWalletUnavailable = errors.New("wallet database unavailable")

func (m *UserRedisModel) available() error {
	if m == nil || m.DB == nil {
		return ErrWalletUnavailable
	}
	return nil
}

// Retained startup compatibility: never overwrite legacy Redis reconciliation evidence.
func (m *UserRedisModel) LoadStonesFromMySQL() error { return m.available() }
func (m *UserRedisModel) GetStones(userID int64) (int64, error) {
	if err := m.available(); err != nil {
		return 0, err
	}
	var n int64
	err := m.DB.QueryRow("SELECT stones FROM users WHERE id=?", userID).Scan(&n)
	return n, err
}
func (m *UserRedisModel) DeductStones(userID, amount int64) error {
	if amount < 0 {
		return errors.New("deduction must be nonnegative")
	}
	if err := m.available(); err != nil {
		return err
	}
	if amount == 0 {
		_, err := m.GetStones(userID)
		return err
	}
	r, err := m.DB.Exec("UPDATE users SET stones=stones-? WHERE id=? AND stones>=? AND stones<=?", amount, userID, amount, int64(math.MaxInt64))
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInsufficientStones
	}
	return nil
}
func (m *UserRedisModel) AddStones(userID, amount int64) error {
	if amount < 0 {
		return errors.New("addition must be nonnegative")
	}
	if err := m.available(); err != nil {
		return err
	}
	if amount == 0 {
		_, err := m.GetStones(userID)
		return err
	}
	r, err := m.DB.Exec("UPDATE users SET stones=stones+? WHERE id=? AND stones<=?", amount, userID, int64(math.MaxInt64)-amount)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("wallet missing or balance overflow")
	}
	return nil
}
func (m *UserRedisModel) SetStones(userID, amount int64) error {
	if amount < 0 {
		return errors.New("balance must be nonnegative")
	}
	if err := m.available(); err != nil {
		return err
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old int64
	if err = tx.QueryRow("SELECT stones FROM users WHERE id=? FOR UPDATE", userID).Scan(&old); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE users SET stones=? WHERE id=?", amount, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStonesWithRecord serializes the actual delta and its ledger with all wallet writes.
func (m *UserRedisModel) SetStonesWithRecord(userID, amount int64, remark string) (int64, error) {
	if amount < 0 {
		return 0, errors.New("balance must be nonnegative")
	}
	if err := m.available(); err != nil {
		return 0, err
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var old int64
	if err = tx.QueryRow("SELECT stones FROM users WHERE id=? FOR UPDATE", userID).Scan(&old); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("UPDATE users SET stones=? WHERE id=?", amount, userID); err != nil {
		return 0, err
	}
	delta := amount - old
	if delta != 0 {
		typ := "manual_grant"
		if delta < 0 {
			typ = "manual_deduct"
			delta = -delta
		}
		if err = NewStoneRecordModel(m.DB).CreateWithTx(tx, userID, typ, delta, "管理员设置灵石", remark); err != nil {
			return 0, err
		}
	}
	return old, tx.Commit()
}

// AdjustStonesWithRecord commits the administrator delta and its audit record together.
func (m *UserRedisModel) AdjustStonesWithRecord(userID, delta int64, remark string) (int64, int64, error) {
	if err := m.available(); err != nil {
		return 0, 0, err
	}
	if delta == math.MinInt64 {
		return 0, 0, errors.New("adjustment overflow")
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var old int64
	if err = tx.QueryRow("SELECT stones FROM users WHERE id=? FOR UPDATE", userID).Scan(&old); err != nil {
		return 0, 0, err
	}
	if delta < 0 && old < -delta {
		return 0, 0, ErrInsufficientStones
	}
	if delta > 0 && old > math.MaxInt64-delta {
		return 0, 0, errors.New("balance overflow")
	}
	next := old + delta
	if _, err = tx.Exec("UPDATE users SET stones=? WHERE id=?", next, userID); err != nil {
		return 0, 0, err
	}
	if delta != 0 {
		typ := "manual_grant"
		amount := delta
		if amount < 0 {
			typ = "manual_deduct"
			amount = -amount
		}
		if err = NewStoneRecordModel(m.DB).CreateWithTx(tx, userID, typ, amount, "管理员调整灵石", remark); err != nil {
			return 0, 0, err
		}
	}
	return old, next, tx.Commit()
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
