// Package resource 资源库域:F7 链接/文件统一入库 + 标签检索。
// 资源共享(全组可见);修改/删除仅上传者或 admin。
// v2(2026-08-25):去掉 paper 类型与 DOI/arXiv/metadata,新增 description/original_name/mime_type/file_size。
package resource

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// 资源类型(v2:仅 link/file;paper 重做后再引入)
const (
	TypeLink = "link"
	TypeFile = "file"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("resource: not found")

// Resource 资源(schema.sql: resources)
type Resource struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Type         string    `gorm:"size:20" json:"type"`
	Title        string    `gorm:"size:300" json:"title"`
	Description  string    `gorm:"type:text" json:"description"`
	URL          string    `gorm:"size:500" json:"url,omitempty"`
	FilePath     string    `gorm:"size:500" json:"-"`
	OriginalName string    `gorm:"size:255" json:"original_name,omitempty"`
	MimeType     string    `gorm:"size:100" json:"mime_type,omitempty"`
	FileSize     int64     `json:"file_size,omitempty"`
	UploaderID   string    `gorm:"type:uuid;index" json:"uploader_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewResource 构造资源。
func NewResource(typ, title string, uploaderID string) *Resource {
	now := time.Now()
	return &Resource{
		ID:         uuid.NewString(),
		Type:       typ,
		Title:      title,
		UploaderID: uploaderID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// ResourceTag 资源-标签关联(schema.sql: resource_tags)
type ResourceTag struct {
	ResourceID string `gorm:"type:uuid;primaryKey" json:"resource_id"`
	TagID      string `gorm:"type:uuid;primaryKey" json:"tag_id"`
}

// ListFilter 资源列表筛选
type ListFilter struct {
	Type     string
	TagID    string
	Keyword  string
	Page     int
	PageSize int
}

// Repository 资源数据访问接口
type Repository interface {
	Create(ctx context.Context, r *Resource) error
	GetByID(ctx context.Context, id string) (*Resource, error)
	Update(ctx context.Context, r *Resource) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, f ListFilter) ([]*Resource, int64, error)
	SetTags(ctx context.Context, resourceID string, tagIDs []string) error
}

// GormRepository Resource 的 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, res *Resource) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(res).Error
}

func (r *GormRepository) GetByID(ctx context.Context, id string) (*Resource, error) {
	var res Resource
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&res, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *GormRepository) Update(ctx context.Context, res *Resource) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(res).Error
}

func (r *GormRepository) Delete(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&Resource{}, "id = ?", id).Error
}

// List 筛选列表:type / tag_id(JOIN resource_tags)/ keyword(标题 ILIKE),分页。
func (r *GormRepository) List(ctx context.Context, f ListFilter) ([]*Resource, int64, error) {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx).Model(&Resource{})

	if f.Type != "" {
		db = db.Where("resources.type = ?", f.Type)
	}
	if f.Keyword != "" {
		db = db.Where("resources.title ILIKE ?", "%"+escapeLike(f.Keyword)+"%")
	}
	if f.TagID != "" {
		db = db.Joins("JOIN resource_tags ON resource_tags.resource_id = resources.id").
			Where("resource_tags.tag_id = ?", f.TagID)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := f.Page
	pageSize := f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}

	var list []*Resource
	err := db.Order("resources.created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *GormRepository) SetTags(ctx context.Context, resourceID string, tagIDs []string) error {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	if err := db.Where("resource_id = ?", resourceID).Delete(&ResourceTag{}).Error; err != nil {
		return err
	}
	if len(tagIDs) == 0 {
		return nil
	}
	rows := make([]ResourceTag, 0, len(tagIDs))
	for _, tid := range tagIDs {
		rows = append(rows, ResourceTag{ResourceID: resourceID, TagID: tid})
	}
	return db.Create(&rows).Error
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
