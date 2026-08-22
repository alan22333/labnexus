// Package space 个人空间域:F2 目录(Folder)模型与数据访问(对应 schema.sql folders 表)。
package space

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// Folder 目录(schema.sql: folders,树形,parent_id 可空 = 根目录)
type Folder struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	SpaceID   string    `gorm:"type:uuid;index" json:"space_id"`
	ParentID  *string   `gorm:"type:uuid;index" json:"parent_id,omitempty"`
	Name      string    `gorm:"size:100" json:"name"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewFolder 构造目录。
func NewFolder(spaceID string, parentID *string, name string, sortOrder int) *Folder {
	now := time.Now()
	return &Folder{
		ID:        uuid.NewString(),
		SpaceID:   spaceID,
		ParentID:  parentID,
		Name:      name,
		SortOrder: sortOrder,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// FolderRepository 目录数据访问接口
type FolderRepository interface {
	Create(ctx context.Context, f *Folder) error
	GetByID(ctx context.Context, id string) (*Folder, error)
	ListBySpace(ctx context.Context, spaceID string) ([]*Folder, error)
	Update(ctx context.Context, f *Folder) error
	Delete(ctx context.Context, id string) error
	CountChildren(ctx context.Context, parentID string) (int64, error)
}

// GormFolderRepository Folder 的 GORM 实现
type GormFolderRepository struct {
	db *gorm.DB
}

func NewGormFolderRepository(db *gorm.DB) *GormFolderRepository {
	return &GormFolderRepository{db: db}
}

func (r *GormFolderRepository) Create(ctx context.Context, f *Folder) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(f).Error
}

func (r *GormFolderRepository) GetByID(ctx context.Context, id string) (*Folder, error) {
	var f Folder
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&f, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *GormFolderRepository) ListBySpace(ctx context.Context, spaceID string) ([]*Folder, error) {
	var folders []*Folder
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("space_id = ?", spaceID).
		Order("sort_order ASC, created_at ASC").
		Find(&folders).Error
	return folders, err
}

func (r *GormFolderRepository) Update(ctx context.Context, f *Folder) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(f).Error
}

func (r *GormFolderRepository) Delete(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&Folder{}, "id = ?", id).Error
}

func (r *GormFolderRepository) CountChildren(ctx context.Context, parentID string) (int64, error) {
	var n int64
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Folder{}).Where("parent_id = ?", parentID).Count(&n).Error
	return n, err
}
