// Package document 文档域:F3 文档(笔记=帖子)+ F4 信息流/点赞/评论。
// 统一内容模型:Document 是唯一内容实体,visibility 切换 private=笔记 / public=帖子。
package document

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// 可见性
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("document: not found")

// Document 文档(schema.sql: documents;DeletedAt = 软删除)
type Document struct {
	ID         string         `gorm:"type:uuid;primaryKey" json:"id"`
	AuthorID   string         `gorm:"type:uuid;index" json:"author_id"`
	SpaceID    string         `gorm:"type:uuid;index" json:"space_id"`
	FolderID   *string        `gorm:"type:uuid;index" json:"folder_id,omitempty"`
	Title      string         `gorm:"size:200" json:"title"`
	Content    string         `gorm:"type:text" json:"content"`
	Visibility string         `gorm:"size:20;default:private" json:"visibility"`
	Pinned     bool           `gorm:"default:false" json:"pinned"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`

	// ReactionCount 仅 hot 排序 Scan 用,不落库
	ReactionCount int64 `gorm:"-" json:"-"`
}

// NewDocument 构造文档。
func NewDocument(authorID, spaceID string, folderID *string, title, content, visibility string) *Document {
	now := time.Now()
	return &Document{
		ID:         uuid.NewString(),
		AuthorID:   authorID,
		SpaceID:    spaceID,
		FolderID:   folderID,
		Title:      title,
		Content:    content,
		Visibility: visibility,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// DocumentTag 文档-标签关联(schema.sql: document_tags)
type DocumentTag struct {
	DocumentID string `gorm:"type:uuid;primaryKey" json:"document_id"`
	TagID      string `gorm:"type:uuid;primaryKey" json:"tag_id"`
}

// Comment 评论(schema.sql: comments;一级回复)
type Comment struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	DocumentID string    `gorm:"type:uuid;index" json:"document_id"`
	AuthorID   string    `gorm:"type:uuid;index" json:"author_id"`
	Content    string    `gorm:"type:text" json:"content"`
	ReplyToID  *string   `gorm:"type:uuid" json:"reply_to_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewComment 构造评论。
func NewComment(documentID, authorID, content string, replyToID *string) *Comment {
	return &Comment{
		ID:         uuid.NewString(),
		DocumentID: documentID,
		AuthorID:   authorID,
		Content:    content,
		ReplyToID:  replyToID,
		CreatedAt:  time.Now(),
	}
}

// Reaction 点赞/表情反应(schema.sql: reactions;唯一约束 一人一文档一表情一次)
type Reaction struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	DocumentID string    `gorm:"type:uuid;index" json:"document_id"`
	UserID     string    `gorm:"type:uuid;index" json:"user_id"`
	Emoji      string    `gorm:"size:16;default:👍" json:"emoji"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewReaction 构造点赞。
func NewReaction(documentID, userID, emoji string) *Reaction {
	if emoji == "" {
		emoji = "👍"
	}
	return &Reaction{
		ID:         uuid.NewString(),
		DocumentID: documentID,
		UserID:     userID,
		Emoji:      emoji,
		CreatedAt:  time.Now(),
	}
}

// Repository 文档数据访问接口
type Repository interface {
	Create(ctx context.Context, d *Document) error
	GetByID(ctx context.Context, id string) (*Document, error)
	Update(ctx context.Context, d *Document) error
	SoftDelete(ctx context.Context, id string) error

	ListBySpace(ctx context.Context, spaceID string, folderID *string, visibility string) ([]*Document, error)
	ListPublic(ctx context.Context, sort string, offset, limit int) ([]*Document, int64, error)
	ListByTag(ctx context.Context, tagID string) ([]*Document, error)
	CountByFolder(ctx context.Context, folderID string) (int64, error)

	// 批量统计,防 N+1(规范 §5)
	ReactionStats(ctx context.Context, docIDs []string) (map[string]int64, error)
	CommentStats(ctx context.Context, docIDs []string) (map[string]int64, error)

	SetTags(ctx context.Context, docID string, tagIDs []string) error
}

// GormRepository Document 的 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, d *Document) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(d).Error
}

func (r *GormRepository) GetByID(ctx context.Context, id string) (*Document, error) {
	var d Document
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&d, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *GormRepository) Update(ctx context.Context, d *Document) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(d).Error
}

func (r *GormRepository) SoftDelete(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Delete(&Document{}, "id = ?", id).Error
}

func (r *GormRepository) ListBySpace(ctx context.Context, spaceID string, folderID *string, visibility string) ([]*Document, error) {
	q := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("space_id = ?", spaceID)
	if folderID != nil {
		q = q.Where("folder_id = ?", *folderID)
	}
	if visibility != "" {
		q = q.Where("visibility = ?", visibility)
	}
	var docs []*Document
	err := q.Order("created_at DESC").Find(&docs).Error
	return docs, err
}

func (r *GormRepository) ListPublic(ctx context.Context, sort string, offset, limit int) ([]*Document, int64, error) {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	var total int64
	if err := db.Model(&Document{}).Where("visibility = ?", VisibilityPublic).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var docs []*Document
	var err error
	if sort == "hot" {
		err = db.Model(&Document{}).
			Select("documents.*, (SELECT COUNT(*) FROM reactions WHERE reactions.document_id = documents.id) AS reaction_count").
			Where("documents.visibility = ?", VisibilityPublic).
			Order("reaction_count DESC, documents.created_at DESC").
			Offset(offset).Limit(limit).
			Scan(&docs).Error
	} else { // latest 默认
		err = db.Where("visibility = ?", VisibilityPublic).
			Order("created_at DESC").Offset(offset).Limit(limit).Find(&docs).Error
	}
	if err != nil {
		return nil, 0, err
	}
	return docs, total, nil
}

func (r *GormRepository) ListByTag(ctx context.Context, tagID string) ([]*Document, error) {
	var docs []*Document
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Joins("JOIN document_tags ON document_tags.document_id = documents.id").
		Where("document_tags.tag_id = ?", tagID).
		Order("documents.created_at DESC").
		Find(&docs).Error
	return docs, err
}

func (r *GormRepository) CountByFolder(ctx context.Context, folderID string) (int64, error) {
	var n int64
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Document{}).Where("folder_id = ?", folderID).Count(&n).Error
	return n, err
}

func (r *GormRepository) ReactionStats(ctx context.Context, docIDs []string) (map[string]int64, error) {
	return countBy(ctx, r.db, &Reaction{}, "document_id", docIDs)
}

func (r *GormRepository) CommentStats(ctx context.Context, docIDs []string) (map[string]int64, error) {
	return countBy(ctx, r.db, &Comment{}, "document_id", docIDs)
}

func countBy(ctx context.Context, db *gorm.DB, model any, groupCol string, ids []string) (map[string]int64, error) {
	out := make(map[string]int64)
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID string
		N  int64
	}
	var rows []row
	err := database.TxFromContext(ctx, db).WithContext(ctx).
		Model(model).
		Select(groupCol+" AS id, COUNT(*) AS n").
		Where(groupCol+" IN ?", ids).
		Group(groupCol).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, rw := range rows {
		out[rw.ID] = rw.N
	}
	return out, nil
}

// SetTags 替换文档标签(事务内调用;先删后插)。
func (r *GormRepository) SetTags(ctx context.Context, docID string, tagIDs []string) error {
	db := database.TxFromContext(ctx, r.db).WithContext(ctx)
	if err := db.Where("document_id = ?", docID).Delete(&DocumentTag{}).Error; err != nil {
		return err
	}
	if len(tagIDs) == 0 {
		return nil
	}
	rows := make([]DocumentTag, 0, len(tagIDs))
	for _, tid := range tagIDs {
		rows = append(rows, DocumentTag{DocumentID: docID, TagID: tid})
	}
	return db.Create(&rows).Error
}

// CommentRepository 评论数据访问接口
type CommentRepository interface {
	Create(ctx context.Context, c *Comment) error
	GetByID(ctx context.Context, id string) (*Comment, error)
	Delete(ctx context.Context, id string) error
	ListByDocument(ctx context.Context, docID string) ([]*Comment, error)
}

// GormCommentRepository Comment 的 GORM 实现
type GormCommentRepository struct {
	db *gorm.DB
}

func NewGormCommentRepository(db *gorm.DB) *GormCommentRepository {
	return &GormCommentRepository{db: db}
}

func (r *GormCommentRepository) Create(ctx context.Context, c *Comment) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(c).Error
}

func (r *GormCommentRepository) GetByID(ctx context.Context, id string) (*Comment, error) {
	var c Comment
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *GormCommentRepository) Delete(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&Comment{}, "id = ?", id).Error
}

func (r *GormCommentRepository) ListByDocument(ctx context.Context, docID string) ([]*Comment, error) {
	var comments []*Comment
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("document_id = ?", docID).
		Order("created_at ASC").Find(&comments).Error
	return comments, err
}

// ReactionRepository 点赞数据访问接口
type ReactionRepository interface {
	Find(ctx context.Context, docID, userID, emoji string) (*Reaction, error)
	Create(ctx context.Context, r *Reaction) error
	Delete(ctx context.Context, id string) error
}

// GormReactionRepository Reaction 的 GORM 实现
type GormReactionRepository struct {
	db *gorm.DB
}

func NewGormReactionRepository(db *gorm.DB) *GormReactionRepository {
	return &GormReactionRepository{db: db}
}

func (r *GormReactionRepository) Find(ctx context.Context, docID, userID, emoji string) (*Reaction, error) {
	var re Reaction
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		First(&re, "document_id = ? AND user_id = ? AND emoji = ?", docID, userID, emoji).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &re, nil
}

func (r *GormReactionRepository) Create(ctx context.Context, re *Reaction) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(re).Error
}

func (r *GormReactionRepository) Delete(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&Reaction{}, "id = ?", id).Error
}
