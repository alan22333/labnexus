// Package tag 标签域:F5 全局标签(对应 schema.sql tags 表)。
// 文档/资源共用标签库;本包不依赖 document(内容聚合在 document 层,防循环依赖)。
package tag

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("tag: not found")

// Tag 全局标签(schema.sql: tags)
type Tag struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:50;uniqueIndex" json:"name"`
	Color     string    `gorm:"size:20;default:#3b82f6" json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// NewTag 构造标签(color 为空用默认蓝)。
func NewTag(name, color string) *Tag {
	if color == "" {
		color = "#3b82f6"
	}
	return &Tag{ID: uuid.NewString(), Name: name, Color: color, CreatedAt: time.Now()}
}

// Repository 标签数据访问接口
type Repository interface {
	Create(ctx context.Context, t *Tag) error
	GetByID(ctx context.Context, id string) (*Tag, error)
	GetByName(ctx context.Context, name string) (*Tag, error)
	List(ctx context.Context) ([]*Tag, error)
	// ListByDocumentIDs 批量查询多个文档的标签(docID → tags),防 N+1(规范 §5)
	ListByDocumentIDs(ctx context.Context, docIDs []string) (map[string][]*Tag, error)
	// ListDocumentIDsByTag 查询打了某标签的文档 ID(F5 内容页)
	ListDocumentIDsByTag(ctx context.Context, tagID string) ([]string, error)
}

// GormRepository Tag 的 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, t *Tag) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(t).Error
}

func (r *GormRepository) GetByID(ctx context.Context, id string) (*Tag, error) {
	var t Tag
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *GormRepository) GetByName(ctx context.Context, name string) (*Tag, error) {
	var t Tag
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&t, "name = ?", name).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *GormRepository) List(ctx context.Context) ([]*Tag, error) {
	var tags []*Tag
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("name ASC").Find(&tags).Error
	return tags, err
}

func (r *GormRepository) ListByDocumentIDs(ctx context.Context, docIDs []string) (map[string][]*Tag, error) {
	result := make(map[string][]*Tag, len(docIDs))
	if len(docIDs) == 0 {
		return result, nil
	}
	type row struct {
		DocumentID string
		TagID      string
		TagName    string
		TagColor   string
	}
	var rows []row
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Table("document_tags").
		Select("document_tags.document_id, tags.id AS tag_id, tags.name AS tag_name, tags.color AS tag_color").
		Joins("JOIN tags ON tags.id = document_tags.tag_id").
		Where("document_tags.document_id IN ?", docIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, rw := range rows {
		result[rw.DocumentID] = append(result[rw.DocumentID], &Tag{
			ID: rw.TagID, Name: rw.TagName, Color: rw.TagColor,
		})
	}
	return result, nil
}

func (r *GormRepository) ListDocumentIDsByTag(ctx context.Context, tagID string) ([]string, error) {
	var ids []string
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Table("document_tags").
		Where("tag_id = ?", tagID).
		Pluck("document_id", &ids).Error
	return ids, err
}
