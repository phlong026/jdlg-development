package model

import (
	"database/sql"
	"time"
)

type DesignerReview struct {
	ID             int64     `json:"id" db:"id"`
	OrderID         int64     `json:"order_id" db:"order_id"`
	OrderNo         string    `json:"order_no" db:"order_no"`
	DesignerUserID int64     `json:"designer_user_id" db:"designer_user_id"`
	ReviewerUserID int64     `json:"reviewer_user_id" db:"reviewer_user_id"`
	ReviewerName   string    `json:"reviewer_name" db:"reviewer_name"`
	ReviewerAvatar string    `json:"reviewer_avatar" db:"reviewer_avatar"`
	Rating         int       `json:"rating" db:"rating"`
	Content        string    `json:"content" db:"content"`
	Sentiment      string    `json:"sentiment" db:"sentiment"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type DesignerReviewModel struct {
	DB *sql.DB
}

func NewDesignerReviewModel(db *sql.DB) *DesignerReviewModel {
	return &DesignerReviewModel{DB: db}
}

func (m *DesignerReviewModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS designer_reviews (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '关联订单ID',
  order_no VARCHAR(64) NOT NULL DEFAULT '' COMMENT '关联订单号',
  designer_user_id BIGINT UNSIGNED NOT NULL COMMENT '设计师用户ID',
  reviewer_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '评价用户ID',
  reviewer_name VARCHAR(64) NOT NULL DEFAULT '' COMMENT '评价人昵称',
  reviewer_avatar VARCHAR(512) NOT NULL DEFAULT '' COMMENT '评价人头像',
  rating TINYINT NOT NULL DEFAULT 5 COMMENT '评分1-5',
  content VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '评价内容',
  sentiment VARCHAR(16) NOT NULL DEFAULT 'positive' COMMENT 'positive/negative',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_order_id (order_id),
  KEY idx_designer_created (designer_user_id, created_at),
  KEY idx_designer_sentiment (designer_user_id, sentiment)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='设计师评价';
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE designer_reviews ADD COLUMN order_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '关联订单ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE designer_reviews ADD COLUMN order_no VARCHAR(64) NOT NULL DEFAULT '' COMMENT '关联订单号'`)
	_, _ = m.DB.Exec(`ALTER TABLE designer_reviews ADD UNIQUE KEY uk_order_id (order_id)`)
	return nil
}

func (m *DesignerReviewModel) ListByDesignerUserID(designerUserID int64, limit, offset int) ([]*DesignerReview, int64, error) {
	var total int64
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_reviews WHERE designer_user_id = ?`, designerUserID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := m.DB.Query(`
		SELECT id, order_id, order_no, designer_user_id, reviewer_user_id, reviewer_name, reviewer_avatar, rating, content, sentiment, created_at
		FROM designer_reviews
		WHERE designer_user_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, designerUserID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*DesignerReview, 0, limit)
	for rows.Next() {
		item := &DesignerReview{}
		if err := rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.OrderNo,
			&item.DesignerUserID,
			&item.ReviewerUserID,
			&item.ReviewerName,
			&item.ReviewerAvatar,
			&item.Rating,
			&item.Content,
			&item.Sentiment,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (m *DesignerReviewModel) SummaryByDesignerUserID(designerUserID int64) (int64, int64, error) {
	positiveCount := int64(0)
	negativeCount := int64(0)
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_reviews WHERE designer_user_id = ? AND sentiment = 'positive'`, designerUserID).Scan(&positiveCount); err != nil {
		return 0, 0, err
	}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_reviews WHERE designer_user_id = ? AND sentiment = 'negative'`, designerUserID).Scan(&negativeCount); err != nil {
		return 0, 0, err
	}
	return positiveCount, negativeCount, nil
}

func (m *DesignerReviewModel) GetByOrderID(orderID int64) (*DesignerReview, error) {
	item := &DesignerReview{}
	err := m.DB.QueryRow(`
		SELECT id, order_id, order_no, designer_user_id, reviewer_user_id, reviewer_name, reviewer_avatar, rating, content, sentiment, created_at
		FROM designer_reviews WHERE order_id = ?
	`, orderID).Scan(
		&item.ID,
		&item.OrderID,
		&item.OrderNo,
		&item.DesignerUserID,
		&item.ReviewerUserID,
		&item.ReviewerName,
		&item.ReviewerAvatar,
		&item.Rating,
		&item.Content,
		&item.Sentiment,
		&item.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (m *DesignerReviewModel) Create(review *DesignerReview) error {
	query := `
		INSERT INTO designer_reviews (order_id, order_no, designer_user_id, reviewer_user_id, reviewer_name, reviewer_avatar, rating, content, sentiment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())
	`
	result, err := m.DB.Exec(query, review.OrderID, review.OrderNo, review.DesignerUserID, review.ReviewerUserID, review.ReviewerName, review.ReviewerAvatar, review.Rating, review.Content, review.Sentiment)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	review.ID = id
	return nil
}
