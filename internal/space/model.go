// Package space 个人空间域:Space 模型与数据访问(对应 schema.sql spaces 表)。
// 注册时自动为每个用户创建个人空间。
package space

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("space: not found")

// Space 个人空间(schema.sql: spaces,与用户 1:1)
type Space struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    string    `gorm:"type:uuid;uniqueIndex" json:"user_id"`
	Name      string    `gorm:"size:50;default:我的空间" json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// NewSpace 为用户创建默认空间。
func NewSpace(userID string) *Space {
	return &Space{
		ID:        uuid.NewString(),
		UserID:    userID,
		Name:      "我的空间",
		CreatedAt: time.Now(),
	}
}

// Repository 空间数据访问接口
type Repository interface {
	Create(ctx context.Context, s *Space) error
	GetByUserID(ctx context.Context, userID string) (*Space, error)
}

// GormRepository Space 的 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, s *Space) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(s).Error
}

func (r *GormRepository) GetByUserID(ctx context.Context, userID string) (*Space, error) {
	var s Space
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&s, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
