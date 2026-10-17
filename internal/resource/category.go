// Package resource 资源分类字典:管理员维护、成员只读(见 docs/specs/resource-library-v2.md §3.2)。
package resource

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// ResourceCategory 资源分类(schema.sql: resource_categories)。
type ResourceCategory struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:50;uniqueIndex" json:"name"`
	Sort      int       `gorm:"default:0" json:"sort"`
	Builtin   bool      `gorm:"default:false" json:"builtin"`
	Active    bool      `gorm:"default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// NewResourceCategory 构造分类。
func NewResourceCategory(name string, sort int, builtin bool) *ResourceCategory {
	return &ResourceCategory{
		ID: uuid.NewString(), Name: name, Sort: sort, Builtin: builtin,
		Active: true, CreatedAt: time.Now(),
	}
}

// ResourceFavorite 资源收藏(schema.sql: resource_favorites)。
type ResourceFavorite struct {
	UserID     string    `gorm:"type:uuid;primaryKey" json:"user_id"`
	ResourceID string    `gorm:"type:uuid;primaryKey" json:"resource_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// CategoryRepository 分类数据访问接口(admin 维护;公开只读 active)。
type CategoryRepository interface {
	ListCategories(ctx context.Context, includeInactive bool) ([]*ResourceCategory, error)
	GetCategory(ctx context.Context, id string) (*ResourceCategory, error)
	CreateCategory(ctx context.Context, c *ResourceCategory) error
	UpdateCategory(ctx context.Context, c *ResourceCategory) error
	DeleteCategory(ctx context.Context, id string) error
	// ClearResourcesCategory 删除分类前把引用资源的 category_id 置空。
	ClearResourcesCategory(ctx context.Context, categoryID string) error
}

func (r *GormRepository) ListCategories(ctx context.Context, includeInactive bool) ([]*ResourceCategory, error) {
	list := make([]*ResourceCategory, 0)
	db := database.TxFromContext(ctx, r.db).WithContext(ctx).Model(&ResourceCategory{})
	if !includeInactive {
		db = db.Where("active = ?", true)
	}
	err := db.Order("sort ASC, name ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) GetCategory(ctx context.Context, id string) (*ResourceCategory, error) {
	var c ResourceCategory
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCategoryNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *GormRepository) CreateCategory(ctx context.Context, c *ResourceCategory) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(c).Error
}

func (r *GormRepository) UpdateCategory(ctx context.Context, c *ResourceCategory) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(c).Error
}

func (r *GormRepository) DeleteCategory(ctx context.Context, id string) error {
	res := database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&ResourceCategory{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (r *GormRepository) ClearResourcesCategory(ctx context.Context, categoryID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Resource{}).Where("category_id = ?", categoryID).
		Update("category_id", nil).Error
}
