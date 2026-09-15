package model

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

// CertificationType 认证类型：个人设计师 / 企业
const (
	CertificationTypeDesigner   = "designer"
	CertificationTypeEnterprise = "enterprise"
)

// CertificationStatus 认证申请状态
const (
	CertificationStatusPendingPayment = "pending_payment" // 待支付（提交后需支付认证费）
	CertificationStatusPendingReview  = "pending_review"   // 待管理员审核（支付完成/阿里云基础核验通过后）
	CertificationStatusApproved       = "approved"
	CertificationStatusRejected       = "rejected"
)

// CertificationApplication 认证申请（即工单：用户提交后由管理员审核其他证件）
// 系统先调用阿里云二要素/三要素完成基础核验，通过后创建本记录，状态 pending_review，由管理员审核施工队/设计师证、企业其他证件等
type CertificationApplication struct {
	ID     int64  `json:"id" db:"id"`
	UserID int64  `json:"user_id" db:"user_id"`
	Type   string `json:"type" db:"type"` // designer | enterprise

	// 二要素（个人）/ 三要素（企业）核验结果（阿里云接口返回，TODO 接入后写入）
	RealName     string `json:"real_name" db:"real_name"`
	IDCardNo     string `json:"id_card_no" db:"id_card_no"`
	CompanyName  string `json:"company_name" db:"company_name"`   // 企业：企业名称
	CreditCode   string `json:"credit_code" db:"credit_code"`     // 企业：统一社会信用代码
	LegalPerson  string `json:"legal_person" db:"legal_person"`   // 企业：法人姓名
	AliyunPassed bool   `json:"aliyun_passed" db:"aliyun_passed"` // 阿里云基础核验是否通过
	AliyunMsg    string `json:"aliyun_msg" db:"aliyun_msg"`       // 阿里云返回信息

	// 用户填写的其他证件说明（施工队、设计师证、企业执照等），供管理员审核
	ExtraDocsRemark string `json:"extra_docs_remark" db:"extra_docs_remark"`
	// 认证身份（个人：设计师/施工队等；企业：企业主等）
	IdentityType string `json:"identity_type" db:"identity_type"`

	Status      string     `json:"status" db:"status"` // pending_payment | pending_review | approved | rejected
	AdminRemark string     `json:"admin_remark" db:"admin_remark"`
	ReviewedAt  *time.Time `json:"reviewed_at" db:"reviewed_at"`
	ReviewedBy  int64      `json:"reviewed_by" db:"reviewed_by"` // 管理员 user_id

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// CertificationApplicationModel 认证申请数据访问
type CertificationApplicationModel struct {
	DB *sql.DB
}

func scanCertificationApplicationRow(scanner interface{ Scan(dest ...interface{}) error }) (*CertificationApplication, error) {
	app := &CertificationApplication{}
	var passed int
	var reviewedAt sql.NullTime
	err := scanner.Scan(
		&app.ID, &app.UserID, &app.Type, &app.RealName, &app.IDCardNo,
		&app.CompanyName, &app.CreditCode, &app.LegalPerson,
		&passed, &app.AliyunMsg, &app.ExtraDocsRemark, &app.IdentityType, &app.Status,
		&app.AdminRemark, &reviewedAt, &app.ReviewedBy, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	app.AliyunPassed = passed != 0
	if reviewedAt.Valid {
		app.ReviewedAt = &reviewedAt.Time
	}
	return app, nil
}

func certificationSubjectSignature(app *CertificationApplication) (string, string) {
	if app == nil {
		return "", ""
	}
	switch strings.TrimSpace(app.Type) {
	case CertificationTypeEnterprise:
		creditCode := strings.ToUpper(strings.TrimSpace(app.CreditCode))
		if creditCode == "" {
			return CertificationTypeEnterprise, ""
		}
		return CertificationTypeEnterprise, creditCode
	default:
		realName := strings.TrimSpace(app.RealName)
		idCardNo := strings.TrimSpace(app.IDCardNo)
		if realName == "" && idCardNo == "" {
			return "personal", ""
		}
		return "personal", realName + "|" + idCardNo
	}
}

// NewCertificationApplicationModel 创建模型
func NewCertificationApplicationModel(db *sql.DB) *CertificationApplicationModel {
	return &CertificationApplicationModel{DB: db}
}

// InitTable 初始化表
func (m *CertificationApplicationModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS certification_applications (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
	type VARCHAR(32) NOT NULL COMMENT 'designer-个人设计师, enterprise-企业',
	real_name VARCHAR(64) NOT NULL DEFAULT '' COMMENT '真实姓名',
	id_card_no VARCHAR(32) NOT NULL DEFAULT '' COMMENT '身份证号',
	company_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT '企业名称(企业认证)',
	credit_code VARCHAR(64) NOT NULL DEFAULT '' COMMENT '统一社会信用代码(企业认证)',
	legal_person VARCHAR(64) NOT NULL DEFAULT '' COMMENT '法人姓名(企业认证)',
	aliyun_passed TINYINT(1) NOT NULL DEFAULT 0 COMMENT '阿里云二要素/三要素是否通过',
	aliyun_msg VARCHAR(255) DEFAULT '' COMMENT '阿里云返回信息',
	extra_docs_remark TEXT COMMENT '用户填写的其他证件说明(施工队/设计师证/企业证件等)',
	identity_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '认证身份(设计师/施工队/企业主等)',
	status VARCHAR(32) NOT NULL DEFAULT 'pending_payment' COMMENT 'pending_payment/pending_review/approved/rejected',
	admin_remark TEXT COMMENT '管理员审核备注',
	reviewed_at DATETIME NULL DEFAULT NULL,
	reviewed_by BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '审核人user_id',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_id (user_id),
	INDEX idx_status (status),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='认证申请(工单)-阿里云基础核验后由管理员审核其他证件';
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	// 兼容老库：补充缺失的列 / 修正字段类型
	if err := m.InitIdentityTypeColumn(); err != nil {
		return err
	}
	return m.InitReviewedByColumnType()
}

// InitIdentityTypeColumn 为已有表添加 identity_type 列（兼容老库）
func (m *CertificationApplicationModel) InitIdentityTypeColumn() error {
	var exists int
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'certification_applications' AND COLUMN_NAME = 'identity_type'`).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		_, err = m.DB.Exec(`ALTER TABLE certification_applications ADD COLUMN identity_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '认证身份(设计师/施工队/企业主等)' AFTER extra_docs_remark`)
		return err
	}
	return nil
}

// InitReviewedByColumnType 兼容老库：将 reviewed_by 字段类型修正为 BIGINT，避免扫描失败导致列表 data 为空
func (m *CertificationApplicationModel) InitReviewedByColumnType() error {
	var dataType string
	err := m.DB.QueryRow(`SELECT DATA_TYPE FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'certification_applications' AND COLUMN_NAME = 'reviewed_by'`).Scan(&dataType)
	if err != nil {
		// 没查到列就忽略
		return nil
	}
	if dataType != "bigint" {
		_, err = m.DB.Exec(`ALTER TABLE certification_applications MODIFY COLUMN reviewed_by BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '审核人user_id'`)
		return err
	}
	return nil
}

// Create 创建申请
func (m *CertificationApplicationModel) Create(app *CertificationApplication) error {
	query := `INSERT INTO certification_applications 
	(user_id, type, real_name, id_card_no, company_name, credit_code, legal_person, aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	passed := 0
	if app.AliyunPassed {
		passed = 1
	}
	res, err := m.DB.Exec(query,
		app.UserID, app.Type, app.RealName, app.IDCardNo,
		app.CompanyName, app.CreditCode, app.LegalPerson,
		passed, app.AliyunMsg, app.ExtraDocsRemark, app.IdentityType, app.Status,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	app.ID = id
	app.CreatedAt = time.Now()
	app.UpdatedAt = app.CreatedAt
	return nil
}

// GetByID 根据 ID 获取
func (m *CertificationApplicationModel) GetByID(id int64) (*CertificationApplication, error) {
	q := `SELECT id, user_id, type, real_name, id_card_no, company_name, credit_code, legal_person,
	       aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, IFNULL(admin_remark,''), reviewed_at, reviewed_by, created_at, updated_at
	      FROM certification_applications WHERE id = ?`
	return scanCertificationApplicationRow(m.DB.QueryRow(q, id))
}

// List 分页列表，status 为空则不过滤
func (m *CertificationApplicationModel) List(status, keyword string, limit, offset int) ([]*CertificationApplication, int64, error) {
	where := "1=1"
	args := []interface{}{}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword != "" {
		likeKeyword := "%" + trimmedKeyword + "%"
		where += ` AND (
			CAST(user_id AS CHAR) LIKE ?
			OR COALESCE(real_name, '') LIKE ?
			OR COALESCE(company_name, '') LIKE ?
			OR COALESCE(id_card_no, '') LIKE ?
			OR COALESCE(credit_code, '') LIKE ?
			OR COALESCE(legal_person, '') LIKE ?
			OR COALESCE(identity_type, '') LIKE ?
		)`
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	var total int64
	if err := m.DB.QueryRow("SELECT COUNT(*) FROM certification_applications WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	q := `SELECT id, user_id, type, real_name, id_card_no, company_name, credit_code, legal_person,
	       aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, IFNULL(admin_remark,''), reviewed_at, reviewed_by, created_at, updated_at
	      FROM certification_applications WHERE ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	// 初始化为空切片，避免返回 null
	list := make([]*CertificationApplication, 0, limit)
	for rows.Next() {
		app, err := scanCertificationApplicationRow(rows)
		if err != nil {
			log.Printf("[CertificationApplicationModel.List] scan row error: %v", err)
			return nil, 0, err
		}
		list = append(list, app)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[CertificationApplicationModel.List] rows error: %v", err)
		return nil, 0, err
	}
	return list, total, nil
}

// UpdateReview 更新审核结果
func (m *CertificationApplicationModel) UpdateReview(id int64, status, adminRemark string, reviewedBy int64) error {
	query := `UPDATE certification_applications SET status = ?, admin_remark = ?, reviewed_at = NOW(), reviewed_by = ?, updated_at = NOW() WHERE id = ?`
	_, err := m.DB.Exec(query, status, adminRemark, reviewedBy, id)
	return err
}

func (m *CertificationApplicationModel) RejectBySystem(id int64, adminRemark string) error {
	query := `UPDATE certification_applications SET status = ?, admin_remark = ?, reviewed_at = NOW(), reviewed_by = 0, updated_at = NOW() WHERE id = ?`
	_, err := m.DB.Exec(query, CertificationStatusRejected, adminRemark, id)
	return err
}

// GetPendingByUser 获取用户最近一条待支付或待审核申请（用于防止重复提交）
func (m *CertificationApplicationModel) GetPendingByUser(userID int64) (*CertificationApplication, error) {
	q := `SELECT id, user_id, type, real_name, id_card_no, company_name, credit_code, legal_person,
	       aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, IFNULL(admin_remark,''), reviewed_at, reviewed_by, created_at, updated_at
	      FROM certification_applications WHERE user_id = ? AND status IN ('pending_payment','pending_review') ORDER BY id DESC LIMIT 1`
	return scanCertificationApplicationRow(m.DB.QueryRow(q, userID))
}

func (m *CertificationApplicationModel) ListEffectiveByUser(userID, excludeID int64) ([]*CertificationApplication, error) {
	q := `SELECT id, user_id, type, real_name, id_card_no, company_name, credit_code, legal_person,
	       aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, IFNULL(admin_remark,''), reviewed_at, reviewed_by, created_at, updated_at
	      FROM certification_applications
	      WHERE user_id = ? AND status IN ('pending_payment','pending_review','approved')`
	args := []interface{}{userID}
	if excludeID > 0 {
		q += ` AND id <> ?`
		args = append(args, excludeID)
	}
	q += ` ORDER BY id DESC`
	rows, err := m.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*CertificationApplication, 0)
	for rows.Next() {
		app, scanErr := scanCertificationApplicationRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (m *CertificationApplicationModel) HasApprovedByUser(userID int64) (bool, error) {
	var count int
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM certification_applications WHERE user_id = ? AND status = ?`, userID, CertificationStatusApproved).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *CertificationApplicationModel) SyncUserCanWithdraw(userModel *UserModel, userID int64) (bool, error) {
	if userModel == nil || userID <= 0 {
		return false, nil
	}
	hasApproved, err := m.HasApprovedByUser(userID)
	if err != nil {
		return false, err
	}
	if err := userModel.UpdateCanWithdraw(userID, hasApproved); err != nil {
		return false, err
	}
	return hasApproved, nil
}

func (m *CertificationApplicationModel) ValidateAccountConsistency(app *CertificationApplication, excludeID int64) error {
	if app == nil || app.UserID <= 0 {
		return nil
	}
	currentType := strings.TrimSpace(app.Type)
	currentSubjectKind, currentSubjectKey := certificationSubjectSignature(app)
	effectiveApps, err := m.ListEffectiveByUser(app.UserID, excludeID)
	if err != nil {
		return err
	}
	for _, existing := range effectiveApps {
		existingType := strings.TrimSpace(existing.Type)
		existingSubjectKind, existingSubjectKey := certificationSubjectSignature(existing)
		if existingType == currentType {
			if existingSubjectKind == currentSubjectKind && existingSubjectKey == currentSubjectKey {
				return fmt.Errorf("当前账号已存在该类型认证申请或已通过认证，无需重复提交")
			}
			return fmt.Errorf("当前账号已存在其他主体的同类型认证，不能重复提交，请更换账号或联系管理员处理")
		}
		if currentSubjectKind != "" && existingSubjectKind != "" && (currentSubjectKind != existingSubjectKind || currentSubjectKey != existingSubjectKey) {
			return fmt.Errorf("当前账号已绑定其他认证主体，暂不允许在同一账号下挂载新的认证主体，请更换账号或联系管理员处理")
		}
	}
	return nil
}

// GetLatestByUser 获取用户最近一条认证申请（任意状态，用于状态展示）
func (m *CertificationApplicationModel) GetLatestByUser(userID int64) (*CertificationApplication, error) {
	q := `SELECT id, user_id, type, real_name, id_card_no, company_name, credit_code, legal_person,
	       aliyun_passed, aliyun_msg, extra_docs_remark, identity_type, status, IFNULL(admin_remark,''), reviewed_at, reviewed_by, created_at, updated_at
	      FROM certification_applications
	      WHERE user_id = ?
	      ORDER BY CASE status
			WHEN 'approved' THEN 0
			WHEN 'pending_review' THEN 1
			WHEN 'pending_payment' THEN 2
			ELSE 3
		END,
		id DESC
	      LIMIT 1`
	return scanCertificationApplicationRow(m.DB.QueryRow(q, userID))
}

func (m *CertificationApplicationModel) PromoteToPendingReviewIfConsistent(id int64) error {
	app, err := m.GetByID(id)
	if err != nil {
		return err
	}
	if app == nil {
		return sql.ErrNoRows
	}
	app.Status = CertificationStatusPendingReview
	if err := m.ValidateAccountConsistency(app, app.ID); err != nil {
		rejectMsg := "系统自动关闭：支付成功后复核发现该账号当前认证主体或有效认证状态已发生变化，当前申请不能继续进入管理员审核，请联系管理员处理。"
		if rejectErr := m.RejectBySystem(id, rejectMsg); rejectErr != nil {
			return rejectErr
		}
		return err
	}
	return m.UpdateStatus(id, CertificationStatusPendingReview)
}

// UpdateStatus 更新申请状态（支付回调将 pending_payment 改为 pending_review）
func (m *CertificationApplicationModel) UpdateStatus(id int64, status string) error {
	_, err := m.DB.Exec("UPDATE certification_applications SET status = ?, updated_at = NOW() WHERE id = ?", status, id)
	return err
}
