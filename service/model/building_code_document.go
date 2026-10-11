package model

import (
	"database/sql"
	"time"
)

const (
	BuildingCodeScopePublic  = "public"
	BuildingCodeScopePrivate = "private"

	BuildingCodeReviewPending  = "pending"
	BuildingCodeReviewApproved = "approved"
	BuildingCodeReviewRejected = "rejected"

	BuildingCodeIndexUploaded = "uploaded"
	BuildingCodeIndexIndexing = "indexing"
	BuildingCodeIndexReady    = "ready"
	BuildingCodeIndexFailed   = "failed"
)

type BuildingCodeDocument struct {
	ID                  int64     `json:"id" db:"id"`
	OwnerUserID         int64     `json:"owner_user_id" db:"owner_user_id"`
	Title               string    `json:"title" db:"title"`
	FileName            string    `json:"file_name" db:"file_name"`
	FileType            string    `json:"file_type" db:"file_type"`
	FileSize            int64     `json:"file_size" db:"file_size"`
	ContentType         string    `json:"content_type" db:"content_type"`
	COSKey              string    `json:"cos_key" db:"cos_key"`
	FileURL             string    `json:"file_url" db:"file_url"`
	Scope               string    `json:"scope" db:"scope"`
	ReviewStatus        string    `json:"review_status" db:"review_status"`
	IndexStatus         string    `json:"index_status" db:"index_status"`
	FastGPTDatasetID    string    `json:"fastgpt_dataset_id" db:"fastgpt_dataset_id"`
	FastGPTCollectionID string    `json:"fastgpt_collection_id" db:"fastgpt_collection_id"`
	RejectReason        string    `json:"reject_reason" db:"reject_reason"`
	ErrorMessage        string    `json:"error_message" db:"error_message"`
	IsEnabled           bool      `json:"is_enabled" db:"is_enabled"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" db:"updated_at"`
}

type BuildingCodeDocumentModel struct {
	DB *sql.DB
}

func NewBuildingCodeDocumentModel(db *sql.DB) *BuildingCodeDocumentModel {
	return &BuildingCodeDocumentModel{DB: db}
}

func (m *BuildingCodeDocumentModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS building_code_documents (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	owner_user_id BIGINT UNSIGNED NOT NULL COMMENT '上传用户ID',
	title VARCHAR(255) NOT NULL COMMENT '文档标题',
	file_name VARCHAR(255) NOT NULL COMMENT '原始文件名',
	file_type VARCHAR(16) NOT NULL COMMENT 'pdf/docx/txt/md',
	file_size BIGINT UNSIGNED NOT NULL COMMENT '文件大小',
	content_type VARCHAR(128) NOT NULL COMMENT 'MIME类型',
	cos_key VARCHAR(512) NOT NULL COMMENT 'COS对象键',
	file_url VARCHAR(512) NOT NULL COMMENT '文件访问URL',
	scope VARCHAR(16) NOT NULL DEFAULT 'public' COMMENT 'public/private，二期私有知识库隔离预留',
	review_status VARCHAR(16) NOT NULL DEFAULT 'approved' COMMENT 'pending/approved/rejected',
	index_status VARCHAR(16) NOT NULL DEFAULT 'uploaded' COMMENT 'uploaded/indexing/ready/failed',
	fastgpt_dataset_id VARCHAR(128) DEFAULT '' COMMENT 'FastGPT知识库ID',
	fastgpt_collection_id VARCHAR(128) DEFAULT '' COMMENT 'FastGPT集合ID',
	reject_reason VARCHAR(512) DEFAULT '' COMMENT '审核拒绝原因',
	error_message TEXT COMMENT '索引失败信息',
	is_enabled TINYINT(1) NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_cos_key (cos_key),
	INDEX idx_owner_user_id (owner_user_id),
	INDEX idx_scope_review (scope, review_status),
	INDEX idx_index_status (index_status),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}

func (m *BuildingCodeDocumentModel) Create(doc *BuildingCodeDocument) error {
	query := `INSERT INTO building_code_documents
		(owner_user_id, title, file_name, file_type, file_size, content_type, cos_key, file_url, scope, review_status, index_status, fastgpt_dataset_id, fastgpt_collection_id, reject_reason, error_message, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	result, err := m.DB.Exec(query,
		doc.OwnerUserID, doc.Title, doc.FileName, doc.FileType, doc.FileSize, doc.ContentType,
		doc.COSKey, doc.FileURL, doc.Scope, doc.ReviewStatus, doc.IndexStatus,
		doc.FastGPTDatasetID, doc.FastGPTCollectionID, doc.RejectReason, doc.ErrorMessage, boolToTinyInt(doc.IsEnabled),
	)
	if err != nil {
		return err
	}
	doc.ID, err = result.LastInsertId()
	return err
}

func (m *BuildingCodeDocumentModel) GetByID(id int64) (*BuildingCodeDocument, error) {
	query := `SELECT id, owner_user_id, title, file_name, file_type, file_size, content_type, cos_key, file_url, scope, review_status, index_status, COALESCE(fastgpt_dataset_id,''), COALESCE(fastgpt_collection_id,''), COALESCE(reject_reason,''), COALESCE(error_message,''), is_enabled, created_at, updated_at
		FROM building_code_documents WHERE id = ?`
	row := m.DB.QueryRow(query, id)
	return scanBuildingCodeDocument(row)
}

func (m *BuildingCodeDocumentModel) ListByOwner(ownerUserID int64, limit, offset int) ([]*BuildingCodeDocument, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT id, owner_user_id, title, file_name, file_type, file_size, content_type, cos_key, file_url, scope, review_status, index_status, COALESCE(fastgpt_dataset_id,''), COALESCE(fastgpt_collection_id,''), COALESCE(reject_reason,''), COALESCE(error_message,''), is_enabled, created_at, updated_at
		FROM building_code_documents WHERE owner_user_id = ? AND is_enabled = 1 ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, ownerUserID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBuildingCodeDocumentRows(rows)
}

func (m *BuildingCodeDocumentModel) ListAll(limit, offset int, scope, reviewStatus, indexStatus string) ([]*BuildingCodeDocument, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT id, owner_user_id, title, file_name, file_type, file_size, content_type, cos_key, file_url, scope, review_status, index_status, COALESCE(fastgpt_dataset_id,''), COALESCE(fastgpt_collection_id,''), COALESCE(reject_reason,''), COALESCE(error_message,''), is_enabled, created_at, updated_at
		FROM building_code_documents WHERE 1=1`
	args := []interface{}{}
	if scope != "" {
		query += " AND scope = ?"
		args = append(args, scope)
	}
	if reviewStatus != "" {
		query += " AND review_status = ?"
		args = append(args, reviewStatus)
	}
	if indexStatus != "" {
		query += " AND index_status = ?"
		args = append(args, indexStatus)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBuildingCodeDocumentRows(rows)
}

func (m *BuildingCodeDocumentModel) MarkIndexing(id int64, datasetID string) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET index_status = ?, fastgpt_dataset_id = ?, error_message = '', updated_at = NOW() WHERE id = ?`, BuildingCodeIndexIndexing, datasetID, id)
	return err
}

func (m *BuildingCodeDocumentModel) MarkIndexed(id int64, collectionID string) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET index_status = ?, fastgpt_collection_id = ?, error_message = '', updated_at = NOW() WHERE id = ?`, BuildingCodeIndexReady, collectionID, id)
	return err
}

func (m *BuildingCodeDocumentModel) MarkIndexFailed(id int64, message string) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET index_status = ?, error_message = ?, updated_at = NOW() WHERE id = ?`, BuildingCodeIndexFailed, trimDBMessage(message), id)
	return err
}

func (m *BuildingCodeDocumentModel) Approve(id int64) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET review_status = ?, reject_reason = '', updated_at = NOW() WHERE id = ?`, BuildingCodeReviewApproved, id)
	return err
}

func (m *BuildingCodeDocumentModel) Reject(id int64, reason string) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET review_status = ?, reject_reason = ?, updated_at = NOW() WHERE id = ?`, BuildingCodeReviewRejected, trimDBMessage(reason), id)
	return err
}

func (m *BuildingCodeDocumentModel) Disable(id int64) error {
	_, err := m.DB.Exec(`UPDATE building_code_documents SET is_enabled = 0, updated_at = NOW() WHERE id = ?`, id)
	return err
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanBuildingCodeDocument(row rowScanner) (*BuildingCodeDocument, error) {
	doc := &BuildingCodeDocument{}
	var enabled int
	err := row.Scan(
		&doc.ID, &doc.OwnerUserID, &doc.Title, &doc.FileName, &doc.FileType, &doc.FileSize,
		&doc.ContentType, &doc.COSKey, &doc.FileURL, &doc.Scope, &doc.ReviewStatus, &doc.IndexStatus,
		&doc.FastGPTDatasetID, &doc.FastGPTCollectionID, &doc.RejectReason, &doc.ErrorMessage,
		&enabled, &doc.CreatedAt, &doc.UpdatedAt,
	)
	doc.IsEnabled = enabled != 0
	return doc, err
}

func scanBuildingCodeDocumentRows(rows *sql.Rows) ([]*BuildingCodeDocument, error) {
	docs := []*BuildingCodeDocument{}
	for rows.Next() {
		doc, err := scanBuildingCodeDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func boolToTinyInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func trimDBMessage(message string) string {
	if len(message) <= 500 {
		return message
	}
	return message[:500]
}
