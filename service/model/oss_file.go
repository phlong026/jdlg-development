package model

import (
	"database/sql"
	"time"
)

// OSSFile OSS文件记录
type OSSFile struct {
	ID          int64     `json:"id" db:"id"`
	ObjectKey   string    `json:"object_key" db:"object_key"`     // COS对象键（完整路径）
	FileName    string    `json:"file_name" db:"file_name"`       // 文件名
	FileSize    int64     `json:"file_size" db:"file_size"`       // 文件大小（字节）
	ContentType string    `json:"content_type" db:"content_type"` // MIME类型
	FileURL     string    `json:"file_url" db:"file_url"`         // 文件访问URL
	SourceType  string    `json:"source_type" db:"source_type"`   // 来源类型：user_ai（用户AI生成）、admin_upload（管理员上传）
	SourceID    int64     `json:"source_id" db:"source_id"`       // 来源ID：用户ID或管理员ID
	SourceName  string    `json:"source_name" db:"source_name"`   // 来源名称：用户名或管理员名
	TaskNo      string    `json:"task_no" db:"task_no"`           // 关联的AI任务编号（如果是用户AI生成）
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// OSSFileModel OSS文件数据访问层
type OSSFileModel struct {
	DB *sql.DB
}

// NewOSSFileModel 创建OSS文件模型
func NewOSSFileModel(db *sql.DB) *OSSFileModel {
	return &OSSFileModel{DB: db}
}

// Create 创建OSS文件记录
func (m *OSSFileModel) Create(file *OSSFile) error {
	query := `INSERT INTO oss_files (object_key, file_name, file_size, content_type, file_url, source_type, source_id, source_name, task_no, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	result, err := m.DB.Exec(query, file.ObjectKey, file.FileName, file.FileSize, file.ContentType, file.FileURL, 
		file.SourceType, file.SourceID, file.SourceName, file.TaskNo)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	file.ID = id
	return nil
}

// GetByID 根据ID获取文件记录
func (m *OSSFileModel) GetByID(id int64) (*OSSFile, error) {
	file := &OSSFile{}
	query := `SELECT id, object_key, file_name, file_size, content_type, file_url, source_type, source_id, source_name, task_no, created_at, updated_at 
	          FROM oss_files WHERE id = ?`
	err := m.DB.QueryRow(query, id).Scan(
		&file.ID, &file.ObjectKey, &file.FileName, &file.FileSize, &file.ContentType, &file.FileURL,
		&file.SourceType, &file.SourceID, &file.SourceName, &file.TaskNo, &file.CreatedAt, &file.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return file, nil
}

// GetByObjectKey 根据对象键获取文件记录
func (m *OSSFileModel) GetByObjectKey(objectKey string) (*OSSFile, error) {
	file := &OSSFile{}
	query := `SELECT id, object_key, file_name, file_size, content_type, file_url, source_type, source_id, source_name, task_no, created_at, updated_at 
	          FROM oss_files WHERE object_key = ?`
	err := m.DB.QueryRow(query, objectKey).Scan(
		&file.ID, &file.ObjectKey, &file.FileName, &file.FileSize, &file.ContentType, &file.FileURL,
		&file.SourceType, &file.SourceID, &file.SourceName, &file.TaskNo, &file.CreatedAt, &file.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func appendOSSFileTypeWhere(query string, args []interface{}, fileType string) (string, []interface{}) {
	switch fileType {
	case "image":
		query += " AND content_type LIKE ?"
		args = append(args, "image/%")
	case "video":
		query += " AND content_type LIKE ?"
		args = append(args, "video/%")
	case "audio":
		query += " AND content_type LIKE ?"
		args = append(args, "audio/%")
	case "document":
		query += " AND (content_type LIKE ? OR content_type = ? OR content_type LIKE ? OR content_type LIKE ? OR content_type LIKE ? OR content_type LIKE ?)"
		args = append(args, "text/%", "application/pdf", "%document%", "%word%", "%excel%", "%powerpoint%")
	case "other":
		query += " AND content_type NOT LIKE ? AND content_type NOT LIKE ? AND content_type NOT LIKE ? AND content_type NOT LIKE ? AND content_type <> ? AND content_type NOT LIKE ? AND content_type NOT LIKE ? AND content_type NOT LIKE ?"
		args = append(args, "image/%", "video/%", "audio/%", "text/%", "application/pdf", "%document%", "%word%", "%excel%")
	}
	return query, args
}

// GetAll 获取文件列表（支持分页和筛选）
func (m *OSSFileModel) GetAll(limit, offset int, sourceType, keyword, fileType string) ([]*OSSFile, error) {
	query := `SELECT id, object_key, file_name, file_size, content_type, file_url, source_type, source_id, source_name, task_no, created_at, updated_at 
	          FROM oss_files WHERE 1=1`
	args := []interface{}{}

	if sourceType != "" && sourceType != "all" {
		query += " AND source_type = ?"
		args = append(args, sourceType)
	}

	if keyword != "" {
		query += " AND (file_name LIKE ? OR object_key LIKE ?)"
		keywordPattern := "%" + keyword + "%"
		args = append(args, keywordPattern, keywordPattern)
	}

	query, args = appendOSSFileTypeWhere(query, args, fileType)

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*OSSFile
	for rows.Next() {
		file := &OSSFile{}
		err := rows.Scan(
			&file.ID, &file.ObjectKey, &file.FileName, &file.FileSize, &file.ContentType, &file.FileURL,
			&file.SourceType, &file.SourceID, &file.SourceName, &file.TaskNo, &file.CreatedAt, &file.UpdatedAt,
		)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

// Count 统计文件总数（支持筛选）
func (m *OSSFileModel) Count(sourceType, keyword, fileType string) (int64, error) {
	query := `SELECT COUNT(*) FROM oss_files WHERE 1=1`
	args := []interface{}{}

	if sourceType != "" && sourceType != "all" {
		query += " AND source_type = ?"
		args = append(args, sourceType)
	}

	if keyword != "" {
		query += " AND (file_name LIKE ? OR object_key LIKE ?)"
		keywordPattern := "%" + keyword + "%"
		args = append(args, keywordPattern, keywordPattern)
	}

	query, args = appendOSSFileTypeWhere(query, args, fileType)

	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// Delete 删除文件记录
func (m *OSSFileModel) Delete(id int64) error {
	query := `DELETE FROM oss_files WHERE id = ?`
	_, err := m.DB.Exec(query, id)
	return err
}

// DeleteBatch 批量删除文件记录
func (m *OSSFileModel) DeleteBatch(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	query := `DELETE FROM oss_files WHERE id IN (`
	args := []interface{}{}
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, id)
	}
	query += ")"
	_, err := m.DB.Exec(query, args...)
	return err
}

// InitTable 初始化oss_files表
func (m *OSSFileModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS oss_files (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	object_key VARCHAR(512) NOT NULL UNIQUE COMMENT 'COS对象键（完整路径）',
	file_name VARCHAR(255) NOT NULL COMMENT '文件名',
	file_size BIGINT UNSIGNED NOT NULL COMMENT '文件大小（字节）',
	content_type VARCHAR(128) NOT NULL COMMENT 'MIME类型',
	file_url VARCHAR(512) NOT NULL COMMENT '文件访问URL',
	source_type VARCHAR(32) NOT NULL COMMENT '来源类型：user_ai（用户AI生成）、admin_upload（管理员上传）',
	source_id BIGINT UNSIGNED NOT NULL COMMENT '来源ID：用户ID或管理员ID',
	source_name VARCHAR(128) NOT NULL COMMENT '来源名称：用户名或管理员名',
	task_no VARCHAR(32) DEFAULT NULL COMMENT '关联的AI任务编号（如果是用户AI生成）',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_source_type (source_type),
	INDEX idx_source_id (source_id),
	INDEX idx_task_no (task_no),
	INDEX idx_created_at (created_at),
	INDEX idx_file_name (file_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}
