package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type AIVideoStudioMedia struct {
	ID              string    `json:"media_id"`
	UserID          int64     `json:"-"`
	OwnerID         string    `json:"owner_id"`
	Kind            string    `json:"media_type"`
	MIME            string    `json:"mime"`
	SizeBytes       int64     `json:"size_bytes"`
	DurationSeconds float64   `json:"duration_seconds"`
	StorageKey      string    `json:"-"`
	Status          string    `json:"status"`
	SourceTaskID    string    `json:"source_task_id,omitempty"`
	SHA256          string    `json:"-"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	Name            string    `json:"name"`
	URL             string    `json:"url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AIVideoStudioMediaStore struct{ DB *sql.DB }

func NewAIVideoStudioMediaStore(db *sql.DB) *AIVideoStudioMediaStore {
	return &AIVideoStudioMediaStore{DB: db}
}

func (s *AIVideoStudioMediaStore) InitTable() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS ai_video_studio_media (
		id CHAR(32) NOT NULL PRIMARY KEY,user_id BIGINT UNSIGNED NOT NULL,owner_id VARCHAR(128) NOT NULL,
		kind VARCHAR(16) NOT NULL,mime VARCHAR(96) NOT NULL,size_bytes BIGINT UNSIGNED NOT NULL,
		duration_seconds DECIMAL(12,4) NOT NULL DEFAULT 0,storage_key CHAR(32) NOT NULL,status VARCHAR(24) NOT NULL,
		source_task_id CHAR(36) NULL,sha256 CHAR(64) NOT NULL,width INT NOT NULL DEFAULT 0,height INT NOT NULL DEFAULT 0,
		name VARCHAR(200) NOT NULL,created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY uk_studio_media_source(user_id,source_task_id),KEY idx_studio_media_owner(user_id,kind,created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	if err != nil {
		return fmt.Errorf("init ai video studio media: %w", err)
	}
	return nil
}

const studioMediaColumns = `id,user_id,owner_id,kind,mime,size_bytes,duration_seconds,storage_key,status,COALESCE(source_task_id,''),sha256,width,height,name,created_at,updated_at`

func scanStudioMedia(row interface{ Scan(...any) error }) (*AIVideoStudioMedia, error) {
	m := &AIVideoStudioMedia{}
	err := row.Scan(&m.ID, &m.UserID, &m.OwnerID, &m.Kind, &m.MIME, &m.SizeBytes, &m.DurationSeconds, &m.StorageKey, &m.Status, &m.SourceTaskID, &m.SHA256, &m.Width, &m.Height, &m.Name, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}
func (s *AIVideoStudioMediaStore) Get(ctx context.Context, userID int64, id string) (*AIVideoStudioMedia, error) {
	return scanStudioMedia(s.DB.QueryRowContext(ctx, `SELECT `+studioMediaColumns+` FROM ai_video_studio_media WHERE id=? AND user_id=? AND status='ready'`, id, userID))
}

// Signed reads are authorized by a short-lived, domain-separated bearer URL.
func (s *AIVideoStudioMediaStore) GetSigned(ctx context.Context, id string) (*AIVideoStudioMedia, error) {
	return scanStudioMedia(s.DB.QueryRowContext(ctx, `SELECT `+studioMediaColumns+` FROM ai_video_studio_media WHERE id=? AND status='ready'`, id))
}
func (s *AIVideoStudioMediaStore) BySource(ctx context.Context, userID int64, taskID string) (*AIVideoStudioMedia, error) {
	return scanStudioMedia(s.DB.QueryRowContext(ctx, `SELECT `+studioMediaColumns+` FROM ai_video_studio_media WHERE user_id=? AND source_task_id=? AND status='ready'`, userID, taskID))
}
func (s *AIVideoStudioMediaStore) Insert(ctx context.Context, m *AIVideoStudioMedia) error {
	var source any
	if m.SourceTaskID != "" {
		source = m.SourceTaskID
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_video_studio_media (id,user_id,owner_id,kind,mime,size_bytes,duration_seconds,storage_key,status,source_task_id,sha256,width,height,name) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.UserID, m.OwnerID, m.Kind, m.MIME, m.SizeBytes, m.DurationSeconds, m.StorageKey, m.Status, source, m.SHA256, m.Width, m.Height, m.Name)
	return err
}
func (s *AIVideoStudioMediaStore) List(ctx context.Context, userID int64, kind, keyword string, limit, offset int) ([]*AIVideoStudioMedia, int64, error) {
	where := `user_id=? AND status='ready'`
	args := []any{userID}
	if kind != "" {
		where += ` AND kind=?`
		args = append(args, kind)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+keyword+"%")
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_media WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+studioMediaColumns+` FROM ai_video_studio_media WHERE `+where+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*AIVideoStudioMedia, 0)
	for rows.Next() {
		item, err := scanStudioMedia(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}
