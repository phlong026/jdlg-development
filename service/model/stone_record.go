package model

import (
	"database/sql"
	"time"
)

// StoneRecord 灵石明细记录
// type: consume-消耗, recharge-充值, checkin-签到, task-任务奖励/退回/模板付费
type StoneRecord struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int64     `json:"user_id" db:"user_id"`
	Type       string    `json:"type" db:"type"`             // consume, recharge, checkin, task
	Amount     int64     `json:"amount" db:"amount"`         // 绝对值，单位灵石
	SceneDesc  string    `json:"scene_desc" db:"scene_desc"` // 场景描述
	Remark     string    `json:"remark" db:"remark"`
	TemplateID *int64    `json:"template_id,omitempty" db:"template_id"` // 模板付费时关联模板ID
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// StoneRecordModel 灵石明细数据访问层
type StoneRecordModel struct {
	DB *sql.DB
}

// NewStoneRecordModel 创建模型
func NewStoneRecordModel(db *sql.DB) *StoneRecordModel {
	return &StoneRecordModel{DB: db}
}

type stoneRecordExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// InitTable 初始化表
func (m *StoneRecordModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS stone_records (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  type VARCHAR(32) NOT NULL COMMENT '类型: consume/recharge/checkin/task',
  amount BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '灵石数量(绝对值)',
  scene_desc VARCHAR(255) DEFAULT '' COMMENT '场景描述',
  remark VARCHAR(255) DEFAULT '' COMMENT '备注',
  template_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '模板付费时关联模板ID',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_user_created (user_id, created_at),
  KEY idx_user_type (user_id, type),
  KEY idx_template_id (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='灵石明细记录';
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	// 兼容旧表：增加 template_id 列（若已存在则忽略错误）
	_, _ = m.DB.Exec(`ALTER TABLE stone_records ADD COLUMN template_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '模板付费时关联模板ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE stone_records ADD INDEX idx_template_id (template_id)`)
	return nil
}

// Create 插入一条明细
func (m *StoneRecordModel) Create(userID int64, typ string, amount int64, sceneDesc, remark string) error {
	return m.CreateWithTemplateID(userID, typ, amount, sceneDesc, remark, 0)
}

// CreateWithTemplateID 插入一条明细（可选 template_id，0 表示不关联）
func (m *StoneRecordModel) CreateWithTemplateID(userID int64, typ string, amount int64, sceneDesc, remark string, templateID int64) error {
	return m.createWithExecutor(m.DB, userID, typ, amount, sceneDesc, remark, templateID)
}

func (m *StoneRecordModel) CreateWithTx(tx *sql.Tx, userID int64, typ string, amount int64, sceneDesc, remark string) error {
	return m.CreateWithTemplateIDTx(tx, userID, typ, amount, sceneDesc, remark, 0)
}

func (m *StoneRecordModel) CreateWithTemplateIDTx(tx *sql.Tx, userID int64, typ string, amount int64, sceneDesc, remark string, templateID int64) error {
	return m.createWithExecutor(tx, userID, typ, amount, sceneDesc, remark, templateID)
}

func (m *StoneRecordModel) createWithExecutor(executor stoneRecordExecutor, userID int64, typ string, amount int64, sceneDesc, remark string, templateID int64) error {
	if amount < 0 {
		amount = -amount
	}
	var tid interface{}
	if templateID > 0 {
		tid = templateID
	} else {
		tid = nil
	}
	query := `INSERT INTO stone_records (user_id, type, amount, scene_desc, remark, template_id, created_at)
	          VALUES (?, ?, ?, ?, ?, ?, NOW())`
	_, err := executor.Exec(query, userID, typ, amount, sceneDesc, remark, tid)
	return err
}

// List 分页查询用户明细，typ 为空表示全部
func (m *StoneRecordModel) List(userID int64, typ string, limit, offset int) ([]*StoneRecord, int64, error) {
	where := "user_id = ?"
	args := []interface{}{userID}
	if typ != "" && typ != "all" {
		where += " AND type = ?"
		args = append(args, typ)
	}

	// 总数
	var total int64
	countQuery := "SELECT COUNT(*) FROM stone_records WHERE " + where
	err := m.DB.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 列表
	args = append(args, limit, offset)
	query := `SELECT id, user_id, type, amount, scene_desc, remark, created_at
	          FROM stone_records WHERE ` + where + `
	          ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*StoneRecord
	for rows.Next() {
		r := &StoneRecord{}
		err := rows.Scan(&r.ID, &r.UserID, &r.Type, &r.Amount, &r.SceneDesc, &r.Remark, &r.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	return list, total, nil
}

// Summary 统计：近30天消耗、近30天获得、累计签到获得
func (m *StoneRecordModel) Summary(userID int64) (recentConsume, recentGain, checkinTotal int64, err error) {
	// 近30天消耗（consume + manual_deduct）
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type IN ('consume','manual_deduct') AND created_at >= DATE_SUB(NOW(), INTERVAL 30 DAY)
	`, userID).Scan(&recentConsume)
	if err != nil {
		return 0, 0, 0, err
	}

	// 近30天获得（recharge + checkin + task + invite + invite_reward + manual_grant）
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type IN ('recharge','checkin','task','invite','invite_reward','manual_grant') AND created_at >= DATE_SUB(NOW(), INTERVAL 30 DAY)
	`, userID).Scan(&recentGain)
	if err != nil {
		return 0, 0, 0, err
	}

	// 累计签到获得
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type = 'checkin'
	`, userID).Scan(&checkinTotal)
	if err != nil {
		return 0, 0, 0, err
	}

	return recentConsume, recentGain, checkinTotal, nil
}

// InviteRewardSummary 邀请奖励汇总：总奖励、本月奖励（type in invite, invite_reward）
func (m *StoneRecordModel) InviteRewardSummary(inviterUserID int64) (totalReward, monthReward int64, err error) {
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type IN ('invite','invite_reward')
	`, inviterUserID).Scan(&totalReward)
	if err != nil {
		return 0, 0, err
	}
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type IN ('invite','invite_reward')
		  AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`, inviterUserID).Scan(&monthReward)
	if err != nil {
		return 0, 0, err
	}
	return totalReward, monthReward, nil
}

// TemplateEarningsSummary 模板付费收益汇总：总收益、本月收益（type=task, scene_desc=模板付费）
func (m *StoneRecordModel) TemplateEarningsSummary(userID int64) (total, monthTotal int64, err error) {
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type = 'task' AND scene_desc = '模板付费'
	`, userID).Scan(&total)
	if err != nil {
		return 0, 0, err
	}
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND type = 'task' AND scene_desc = '模板付费'
		  AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`, userID).Scan(&monthTotal)
	if err != nil {
		return 0, 0, err
	}
	return total, monthTotal, nil
}

// EarningsByTemplateID 某模板的累计收益（发布者 userID 从该模板获得的灵石）
func (m *StoneRecordModel) EarningsByTemplateID(userID int64, templateID int64) (int64, error) {
	var sum int64
	err := m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM stone_records
		WHERE user_id = ? AND template_id = ? AND type = 'task' AND scene_desc = '模板付费'
	`, userID, templateID).Scan(&sum)
	return sum, err
}

// ListTemplateIncome 模板付费收益明细（分页）
func (m *StoneRecordModel) ListTemplateIncome(userID int64, limit, offset int) ([]*StoneRecord, int64, error) {
	var total int64
	err := m.DB.QueryRow(`
		SELECT COUNT(*) FROM stone_records
		WHERE user_id = ? AND type = 'task' AND scene_desc = '模板付费'
	`, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT id, user_id, type, amount, scene_desc, remark, template_id, created_at
	          FROM stone_records WHERE user_id = ? AND type = 'task' AND scene_desc = '模板付费'
	          ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*StoneRecord
	for rows.Next() {
		r := &StoneRecord{}
		var tid sql.NullInt64
		err := rows.Scan(&r.ID, &r.UserID, &r.Type, &r.Amount, &r.SceneDesc, &r.Remark, &tid, &r.CreatedAt)
		if err != nil {
			return nil, 0, err
		}
		if tid.Valid {
			r.TemplateID = &tid.Int64
		}
		list = append(list, r)
	}
	return list, total, nil
}
