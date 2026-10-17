// Package resource 资源库域:F7 链接/文件统一入库 + 标签检索。
// 资源共享(全组可见);修改/删除仅上传者或 admin。
// v2(2026-08-25):去掉 paper 类型与 DOI/arXiv/metadata,新增 description/original_name/mime_type/file_size。
// v3(2026-10-17):分类、置顶、浏览/下载计数、个人收藏(见 docs/specs/resource-library-v2.md)。
package resource

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	ID            string    `gorm:"type:uuid;primaryKey" json:"id"`
	Type          string    `gorm:"size:20" json:"type"`
	Title         string    `gorm:"size:300" json:"title"`
	Description   string    `gorm:"type:text" json:"description"`
	URL           string    `gorm:"size:500" json:"url,omitempty"`
	FilePath      string    `gorm:"size:500" json:"-"`
	OriginalName  string    `gorm:"size:255" json:"original_name,omitempty"`
	MimeType      string    `gorm:"size:100" json:"mime_type,omitempty"`
	FileSize      int64     `json:"file_size,omitempty"`
	CategoryID    *string   `gorm:"type:uuid;index" json:"category_id,omitempty"`
	Pinned        bool      `gorm:"default:false" json:"pinned"`
	ViewCount     int64     `gorm:"default:0" json:"view_count"`
	DownloadCount int64     `gorm:"default:0" json:"download_count"`
	UploaderID    string    `gorm:"type:uuid;index" json:"uploader_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

// ListFilter 资源列表筛选(v2 扩展跨字段检索/分类/收藏/排序)。
type ListFilter struct {
	Type          string
	TagID         string
	TagIDs        []string
	Keyword       string
	CategoryID    string
	Uncategorized bool
	UploaderID    string
	FavoritedBy   string
	PinnedOnly    bool
	Sort          string
	// ViewerID 仅内部使用:标记当前请求用户,用于组装 favorited 字段,不参与筛选。
	ViewerID string
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

	// FindLinkByURL 查找完全相同 URL 的 link 资源(用于去重)。
	FindLinkByURL(ctx context.Context, url string) (*Resource, error)
	// IncrementViewCount / IncrementDownloadCount 计数(展示用途,尽力而为)。
	IncrementViewCount(ctx context.Context, id string) error
	IncrementDownloadCount(ctx context.Context, id string) error
	// 收藏(个人维度,幂等)。
	AddFavorite(ctx context.Context, userID, resourceID string) error
	RemoveFavorite(ctx context.Context, userID, resourceID string) error
	FavoriteResourceIDs(ctx context.Context, userID string, resourceIDs []string) (map[string]bool, error)
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

// List 筛选列表:type / 多标签 AND / 跨字段关键词 / 分类 / 上传者 / 收藏 / 置顶 + 排序分页。
func (r *GormRepository) List(ctx context.Context, f ListFilter) ([]*Resource, int64, error) {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx).Model(&Resource{})

	if f.Type != "" {
		db = db.Where("resources.type = ?", f.Type)
	}
	if f.Keyword != "" {
		like := "%" + escapeLike(f.Keyword) + "%"
		db = db.Where(`(
			resources.title ILIKE ? OR
			resources.description ILIKE ? OR
			resources.original_name ILIKE ? OR
			resources.url ILIKE ? OR
			EXISTS (
				SELECT 1 FROM resource_tags rt_kw
				JOIN tags t_kw ON t_kw.id = rt_kw.tag_id
				WHERE rt_kw.resource_id = resources.id AND t_kw.name ILIKE ?
			)
		)`, like, like, like, like, like)
	}
	for _, tagID := range uniqueNonEmpty(append([]string{f.TagID}, f.TagIDs...)) {
		db = db.Where(`EXISTS (
			SELECT 1 FROM resource_tags rt_filter
			WHERE rt_filter.resource_id = resources.id AND rt_filter.tag_id = ?
		)`, tagID)
	}
	if f.CategoryID != "" {
		db = db.Where("resources.category_id = ?", f.CategoryID)
	}
	if f.Uncategorized {
		db = db.Where("resources.category_id IS NULL")
	}
	if f.UploaderID != "" {
		db = db.Where("resources.uploader_id = ?", f.UploaderID)
	}
	if f.FavoritedBy != "" {
		db = db.Where(`EXISTS (
			SELECT 1 FROM resource_favorites rf_filter
			WHERE rf_filter.resource_id = resources.id AND rf_filter.user_id = ?
		)`, f.FavoritedBy)
	}
	if f.PinnedOnly {
		db = db.Where("resources.pinned = ?", true)
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

	order := "resources.created_at DESC"
	switch f.Sort {
	case "", SortLatest:
		order = "resources.created_at DESC"
	case SortOldest:
		order = "resources.created_at ASC"
	case SortTitle:
		order = "resources.title ASC, resources.created_at DESC"
	case SortSize:
		order = "COALESCE(resources.file_size, 0) DESC, resources.created_at DESC"
	case SortUseful:
		order = "(resources.view_count + resources.download_count) DESC, resources.created_at DESC"
	}

	var list []*Resource
	err := db.Order("resources.pinned DESC").Order(order).
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

func (r *GormRepository) FindLinkByURL(ctx context.Context, rawURL string) (*Resource, error) {
	var res Resource
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("type = ? AND url = ?", TypeLink, strings.TrimSpace(rawURL)).
		Order("created_at ASC").First(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *GormRepository) IncrementViewCount(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Resource{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}

func (r *GormRepository) IncrementDownloadCount(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Resource{}).Where("id = ?", id).
		UpdateColumn("download_count", gorm.Expr("download_count + 1")).Error
}

func (r *GormRepository) AddFavorite(ctx context.Context, userID, resourceID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&ResourceFavorite{UserID: userID, ResourceID: resourceID, CreatedAt: time.Now()}).Error
}

func (r *GormRepository) RemoveFavorite(ctx context.Context, userID, resourceID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("user_id = ? AND resource_id = ?", userID, resourceID).
		Delete(&ResourceFavorite{}).Error
}

func (r *GormRepository) FavoriteResourceIDs(ctx context.Context, userID string, resourceIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if userID == "" || len(resourceIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&ResourceFavorite{}).
		Where("user_id = ? AND resource_id IN ?", userID, resourceIDs).
		Pluck("resource_id", &ids).Error
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
