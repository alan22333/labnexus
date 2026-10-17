// Package resource 业务层:F7 资源库(link + file)。
// v2(2026-08-25):去掉 paper 类型与 DOI/arXiv 抓取(F8 废弃,见 specs/f8-paper-meta.md);
// 新增描述字段、URL 协议校验、MIME 双校验、大小限制、下载/预览。
// v3(2026-10-17):分类、置顶、计数、收藏、跨字段检索(见 specs/resource-library-v2.md)。
package resource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"labnexus/internal/authz"
	"labnexus/internal/database"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// 排序取值(契约 §F7)。
const (
	SortLatest = "latest"
	SortOldest = "oldest"
	SortTitle  = "title"
	SortSize   = "size"
	SortUseful = "useful"
)

var validSorts = map[string]bool{
	SortLatest: true, SortOldest: true, SortTitle: true, SortSize: true, SortUseful: true,
}

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrInvalidType         = errors.New("invalid resource type")
	ErrTitleEmpty          = errors.New("resource title is empty")
	ErrURLRequired         = errors.New("url is required for link resource")
	ErrInvalidURL          = errors.New("url must be a valid http/https link")
	ErrResourceNotFound    = errors.New("resource not found")
	ErrResourceForbidden   = errors.New("cannot modify others' resource")
	ErrNotFile             = errors.New("resource is not a file")
	ErrFileTooLarge        = errors.New("file too large")
	ErrFileTypeNotAllowed  = errors.New("file type not allowed")
	ErrFileContentMismatch = errors.New("file content does not match extension")
	ErrPreviewUnsupported  = errors.New("preview not supported for this file type")
	ErrInvalidSort         = errors.New("invalid resource sort")
	ErrInvalidCategory     = errors.New("category does not exist or is inactive")
	ErrCategoryNameEmpty   = errors.New("resource category name is empty")
	ErrCategoryExists      = errors.New("resource category already exists")
	ErrCategoryNotFound    = errors.New("resource category not found")
)

// DuplicateLinkError URL 完全相同时返回,携带已存在资源 id(供前端二次确认后 force)。
type DuplicateLinkError struct {
	ExistingID string
}

func (e *DuplicateLinkError) Error() string { return "link URL already exists" }

// 限制(规格 §5)
const (
	MaxFileSize   = 50 << 20             // 普通文件 50MB
	MaxVideoSize  = 100 << 20            // 视频 100MB
	MaxUploadBody = MaxVideoSize + 1<<20 // handler 整体请求体上限(100MB+余量)

	// sniffLen 读取文件头字节数用于 MIME 检测
	sniffLen = 512
)

// allowedExts 文件类型白名单(规格 §5)
var allowedExts = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".txt": true, ".md": true,
	".ppt": true, ".pptx": true, ".xls": true, ".xlsx": true,
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
	".zip": true, ".tar": true, ".gz": true,
	".mp4": true, ".webm": true,
}

// videoExts 视频扩展名(大小上限 100MB)
var videoExts = map[string]bool{".mp4": true, ".webm": true}

// mimeByExt 扩展名 → 标准 MIME(存储与下载/预览响应头)
var mimeByExt = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".zip":  "application/zip",
	".tar":  "application/x-tar",
	".gz":   "application/gzip",
	".mp4":  "video/mp4",
	".webm": "video/webm",
}

// previewByExt 支持预览的类型(pdf / image / text / video / sheet);其余仅下载。
var previewByExt = map[string]string{
	".pdf": "pdf",
	".png": "image", ".jpg": "image", ".jpeg": "image", ".webp": "image", ".gif": "image",
	".txt": "text", ".md": "text",
	".xls": "sheet", ".xlsx": "sheet",
	".mp4": "video", ".webm": "video",
}

// Service 资源业务逻辑
type Service struct {
	repo       Repository
	categories CategoryRepository
	tags       tag.Repository
	store      FileStore
	users      user.Repository
	txRunner   database.TxRunner
	authz      authz.Authorizer
}

// NewService 构造函数(依赖注入)。
func NewService(
	repo Repository,
	tags tag.Repository,
	store FileStore,
	users user.Repository,
) *Service {
	return &Service{
		repo: repo, tags: tags, store: store, users: users,
		txRunner: database.NoopTxRunner(),
		authz:    authz.New(users, authz.Options{}),
	}
}

// WithAuthorizer 注入统一授权器。
func (s *Service) WithAuthorizer(a authz.Authorizer) *Service {
	s.authz = a
	return s
}

// WithTxRunner 注入事务运行器。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// WithCategoryRepository 注入分类仓库(应用装配与测试使用)。
func (s *Service) WithCategoryRepository(repo CategoryRepository) *Service {
	s.categories = repo
	return s
}

// PreviewInfo 预览能力信息(资源视图返回)
type PreviewInfo struct {
	Supported bool   `json:"supported"`
	Type      string `json:"type,omitempty"`
	URL       string `json:"url,omitempty"`
}

// ResourceView 资源视图(含上传者/分类/标签/预览/下载/收藏)。
type ResourceView struct {
	*Resource
	Uploader    *user.User        `json:"uploader"`
	Category    *ResourceCategory `json:"category"`
	Tags        []*tag.Tag        `json:"tags"`
	Favorited   bool              `json:"favorited"`
	Preview     PreviewInfo       `json:"preview"`
	DownloadURL string            `json:"download_url,omitempty"`
}

// CreateLinkRequest 链接资源创建请求
type CreateLinkRequest struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	CategoryID  *string  `json:"category_id"`
	TagIDs      []string `json:"tag_ids"`
	Force       bool     `json:"force"`
}

// UpdateRequest 修改资源请求
type UpdateRequest struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	CategoryID  *string   `json:"category_id"`
	Pinned      *bool     `json:"pinned"`
	TagIDs      *[]string `json:"tag_ids"`
}

// UpdateCategoryRequest 分类编辑请求(admin console)。
type UpdateCategoryRequest struct {
	Name   *string `json:"name"`
	Sort   *int    `json:"sort"`
	Active *bool   `json:"active"`
}

// ---- F7:创建 ----

// CreateLink 创建链接资源(url 仅 http/https;v2 支持去重与分类)。
func (s *Service) CreateLink(ctx context.Context, userID string, req CreateLinkRequest) (*ResourceView, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, ErrTitleEmpty
	}
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, ErrURLRequired
	}
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	if !req.Force {
		existing, err := s.repo.FindLinkByURL(ctx, rawURL)
		if err == nil {
			return nil, &DuplicateLinkError{ExistingID: existing.ID}
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	categoryID, err := s.resolveCategoryID(ctx, req.CategoryID)
	if err != nil {
		return nil, err
	}
	if err := s.validateTags(ctx, req.TagIDs); err != nil {
		return nil, err
	}
	res := NewResource(TypeLink, req.Title, userID)
	res.URL = rawURL
	res.Description = req.Description
	res.CategoryID = categoryID
	if err := s.createWithTags(ctx, res, req.TagIDs); err != nil {
		return nil, err
	}
	return s.buildView(ctx, userID, res)
}

// UploadFile 上传文件资源(兼容旧签名;分类由 UploadFileWithCategory 提供)。
func (s *Service) UploadFile(ctx context.Context, userID, filename string, reader io.Reader, title, description string, tagIDs []string) (*ResourceView, error) {
	return s.UploadFileWithCategory(ctx, userID, filename, reader, title, description, nil, tagIDs)
}

// UploadFileWithCategory 上传文件资源(扩展名 + 内容 MIME 双校验 + 大小限制 + 可选分类)。
func (s *Service) UploadFileWithCategory(ctx context.Context, userID, filename string, reader io.Reader, title, description string, categoryID *string, tagIDs []string) (*ResourceView, error) {
	ext := strings.ToLower(path.Ext(filename))
	if !allowedExts[ext] {
		return nil, ErrFileTypeNotAllowed
	}
	// 读文件头做 MIME 检测(重放给存储)
	head, err := io.ReadAll(io.LimitReader(reader, sniffLen))
	if err != nil {
		return nil, err
	}
	if !sniffMatches(ext, head) {
		return nil, ErrFileContentMismatch
	}

	limit := int64(MaxFileSize)
	if videoExts[ext] {
		limit = MaxVideoSize
	}
	// 用 LimitReader 限制实际落盘大小,超过即报错(防止伪造 header)
	bounded := io.LimitReader(io.MultiReader(bytes.NewReader(head), reader), limit+1)

	filePath, size, err := s.store.Save(bounded, filename)
	if err != nil {
		return nil, err
	}
	if size > limit {
		_ = s.store.Delete(filePath)
		return nil, ErrFileTooLarge
	}

	if title == "" {
		title = filename
	}
	resolvedCategoryID, err := s.resolveCategoryID(ctx, categoryID)
	if err != nil {
		_ = s.store.Delete(filePath)
		return nil, err
	}
	if err := s.validateTags(ctx, tagIDs); err != nil {
		_ = s.store.Delete(filePath)
		return nil, err
	}
	res := NewResource(TypeFile, title, userID)
	res.Description = description
	res.FilePath = filePath
	res.OriginalName = filename
	res.MimeType = mimeByExt[ext]
	res.FileSize = size
	res.CategoryID = resolvedCategoryID
	if err := s.createWithTags(ctx, res, tagIDs); err != nil {
		_ = s.store.Delete(filePath) // 回滚已存文件
		return nil, err
	}
	return s.buildView(ctx, userID, res)
}

// createWithTags 事务:写资源 + 标签。
func (s *Service) createWithTags(ctx context.Context, res *Resource, tagIDs []string) error {
	return s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.Create(tctx, res); err != nil {
			return err
		}
		return s.repo.SetTags(tctx, res.ID, tagIDs)
	})
}

// ---- F7:列表/详情 ----

// List 资源列表(全组可见;支持 type/标签/跨字段关键词/分类/收藏/上传者/置顶 + 排序分页)。
func (s *Service) List(ctx context.Context, f ListFilter) ([]*ResourceView, int64, error) {
	if f.Sort == "" {
		f.Sort = SortLatest
	}
	if !validSorts[f.Sort] {
		return nil, 0, ErrInvalidSort
	}
	list, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.buildViews(ctx, f.ViewerID, list)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// ListByTag 按标签列出资源(供 /tags/:id/contents 聚合,全组可见)。
func (s *Service) ListByTag(ctx context.Context, tagID string) ([]*ResourceView, error) {
	list, _, err := s.repo.List(ctx, ListFilter{TagID: tagID, Sort: SortLatest})
	if err != nil {
		return nil, err
	}
	return s.buildViews(ctx, "", list)
}

// Get 资源详情(任意登录用户;兼容旧调用,不含个人收藏视角)。
func (s *Service) Get(ctx context.Context, id string) (*ResourceView, error) {
	return s.GetForUser(ctx, "", id)
}

// GetForUser 资源详情(携带当前用户 favorited 视角)。
func (s *Service) GetForUser(ctx context.Context, viewerID, id string) (*ResourceView, error) {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrResourceNotFound
	}
	return s.buildView(ctx, viewerID, res)
}

// OpenFile 打开文件资源(仅 file 类型;供下载/预览)。
func (s *Service) OpenFile(ctx context.Context, id string) (*Resource, io.ReadSeekCloser, error) {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, ErrResourceNotFound
	}
	if res.Type != TypeFile {
		return nil, nil, ErrNotFile
	}
	rc, err := s.store.Open(res.FilePath)
	if err != nil {
		return nil, nil, err
	}
	return res, rc, nil
}

// Update 修改资源(仅上传者或 admin;支持分类与置顶)。
func (s *Service) Update(ctx context.Context, userID, id string, req UpdateRequest) (*ResourceView, error) {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrResourceNotFound
	}
	ok, err := s.canManage(ctx, userID, res)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrResourceForbidden
	}

	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, ErrTitleEmpty
		}
		res.Title = *req.Title
	}
	if req.Description != nil {
		res.Description = *req.Description
	}
	if req.CategoryID != nil {
		categoryID, err := s.resolveCategoryID(ctx, req.CategoryID)
		if err != nil {
			return nil, err
		}
		res.CategoryID = categoryID
	}
	if req.Pinned != nil {
		res.Pinned = *req.Pinned
	}
	res.UpdatedAt = time.Now()

	err = s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.Update(tctx, res); err != nil {
			return err
		}
		if req.TagIDs != nil {
			if err := s.validateTags(tctx, *req.TagIDs); err != nil {
				return err
			}
			return s.repo.SetTags(tctx, res.ID, *req.TagIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, userID, res)
}

// Delete 删除资源(仅上传者或 admin;file 同步删磁盘文件,删除失败记日志)。
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return ErrResourceNotFound
	}
	ok, err := s.canManage(ctx, userID, res)
	if err != nil {
		return err
	}
	if !ok {
		return ErrResourceForbidden
	}
	if res.Type != TypeFile {
		return s.repo.Delete(ctx, res.ID)
	}

	quarantinePath, err := s.store.Quarantine(res.FilePath)
	if err != nil {
		return fmt.Errorf("quarantine resource file: %w", err)
	}
	if err := s.repo.Delete(ctx, res.ID); err != nil {
		if restoreErr := s.store.Restore(res.FilePath, quarantinePath); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore quarantined resource file: %w", restoreErr))
		}
		return err
	}
	if err := s.store.DeleteQuarantined(quarantinePath); err != nil {
		return fmt.Errorf("resource deleted but quarantined file cleanup failed: %w", err)
	}
	return nil
}

// ---- v2:计数 / 收藏 ----

// RecordView 浏览计数(资源不存在 → ErrResourceNotFound)。
func (s *Service) RecordView(ctx context.Context, id string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return ErrResourceNotFound
	}
	return s.repo.IncrementViewCount(ctx, id)
}

// RecordDownload 下载计数(调用方在文件流成功返回后尽力调用)。
func (s *Service) RecordDownload(ctx context.Context, id string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return ErrResourceNotFound
	}
	return s.repo.IncrementDownloadCount(ctx, id)
}

// Favorite 收藏资源(幂等;资源不存在 → ErrResourceNotFound)。
func (s *Service) Favorite(ctx context.Context, userID, resourceID string) error {
	if _, err := s.repo.GetByID(ctx, resourceID); err != nil {
		return ErrResourceNotFound
	}
	return s.repo.AddFavorite(ctx, userID, resourceID)
}

// Unfavorite 取消收藏(幂等;资源不存在 → ErrResourceNotFound)。
func (s *Service) Unfavorite(ctx context.Context, userID, resourceID string) error {
	if _, err := s.repo.GetByID(ctx, resourceID); err != nil {
		return ErrResourceNotFound
	}
	return s.repo.RemoveFavorite(ctx, userID, resourceID)
}

// ---- v2:分类字典 ----

// ListCategories 分类字典(includeInactive=true 含停用;公开接口只用 active)。
func (s *Service) ListCategories(ctx context.Context, includeInactive bool) ([]*ResourceCategory, error) {
	if s.categories == nil {
		return []*ResourceCategory{}, nil
	}
	return s.categories.ListCategories(ctx, includeInactive)
}

// CreateCategory 新建分类(name 必填、重名冲突;sort 为空时追加到末尾)。
func (s *Service) CreateCategory(ctx context.Context, name string, sort *int) (*ResourceCategory, error) {
	if s.categories == nil {
		return nil, ErrCategoryNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrCategoryNameEmpty
	}
	list, err := s.categories.ListCategories(ctx, true)
	if err != nil {
		return nil, err
	}
	maxSort := 0
	for _, c := range list {
		if c.Name == name {
			return nil, ErrCategoryExists
		}
		if c.Sort > maxSort {
			maxSort = c.Sort
		}
	}
	order := maxSort + 1
	if sort != nil {
		order = *sort
	}
	c := NewResourceCategory(name, order, false)
	if err := s.categories.CreateCategory(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// UpdateCategoryRequest 分类编辑由 UpdateCategory 处理。

// UpdateCategory 编辑分类(改名/排序/启停)。
func (s *Service) UpdateCategory(ctx context.Context, id string, req UpdateCategoryRequest) (*ResourceCategory, error) {
	if s.categories == nil {
		return nil, ErrCategoryNotFound
	}
	c, err := s.categories.GetCategory(ctx, id)
	if err != nil {
		return nil, ErrCategoryNotFound
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrCategoryNameEmpty
		}
		list, err := s.categories.ListCategories(ctx, true)
		if err != nil {
			return nil, err
		}
		for _, other := range list {
			if other.ID != id && other.Name == name {
				return nil, ErrCategoryExists
			}
		}
		c.Name = name
	}
	if req.Sort != nil {
		c.Sort = *req.Sort
	}
	if req.Active != nil {
		c.Active = *req.Active
	}
	if err := s.categories.UpdateCategory(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteCategory 删除分类并把引用资源回退未分类(事务)。
func (s *Service) DeleteCategory(ctx context.Context, id string) error {
	if s.categories == nil {
		return ErrCategoryNotFound
	}
	if _, err := s.categories.GetCategory(ctx, id); err != nil {
		return ErrCategoryNotFound
	}
	return s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.categories.ClearResourcesCategory(tctx, id); err != nil {
			return err
		}
		return s.categories.DeleteCategory(tctx, id)
	})
}

// resolveCategoryID 校验并归一化分类 id:空 → nil;非空须存在且 active。
func (s *Service) resolveCategoryID(ctx context.Context, categoryID *string) (*string, error) {
	if categoryID == nil || strings.TrimSpace(*categoryID) == "" {
		return nil, nil
	}
	if s.categories == nil {
		return nil, ErrInvalidCategory
	}
	cat, err := s.categories.GetCategory(ctx, *categoryID)
	if err != nil || !cat.Active {
		return nil, ErrInvalidCategory
	}
	return categoryID, nil
}

// ---- 内部 helper ----

// validateURL 校验链接仅 http/https 且可解析。
func validateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidURL
	}
	if u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}

// sniffMatches 校验文件头与扩展名一致(防改名绕过)。
// 返回 false 表示内容与扩展名不符。
func sniffMatches(ext string, head []byte) bool {
	switch ext {
	case ".pdf":
		return bytes.HasPrefix(head, []byte("%PDF-"))
	case ".png":
		return bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4E, 0x47})
	case ".jpg", ".jpeg":
		return len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
	case ".gif":
		return bytes.HasPrefix(head, []byte("GIF8"))
	case ".webp":
		return len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	case ".mp4":
		return len(head) >= 8 && string(head[4:8]) == "ftyp"
	case ".webm":
		return len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3})
	case ".zip", ".docx", ".pptx", ".xlsx":
		return bytes.HasPrefix(head, []byte{0x50, 0x4B, 0x03, 0x04}) // ZIP magic
	case ".doc", ".ppt", ".xls":
		// OLE2(旧 Office)或 ZIP(OOXML)
		return bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0}) ||
			bytes.HasPrefix(head, []byte{0x50, 0x4B, 0x03, 0x04})
	case ".gz":
		return len(head) >= 2 && head[0] == 0x1F && head[1] == 0x8B
	case ".tar":
		// tar 头第 257 字节起为 "ustar"
		return len(head) >= 262 && string(head[257:262]) == "ustar"
	case ".txt", ".md":
		return true // 文本不校验内容
	default:
		return false
	}
}

// validateTags 校验标签全部存在。
func (s *Service) validateTags(ctx context.Context, tagIDs []string) error {
	for _, id := range tagIDs {
		if _, err := s.tags.GetByID(ctx, id); err != nil {
			return tag.ErrTagNotFound
		}
	}
	return nil
}

// canManage 上传者本人或 admin 可管理。
func (s *Service) canManage(ctx context.Context, userID string, res *Resource) (bool, error) {
	return s.authz.Can(ctx, userID, authz.ActionManage, authz.Target{
		Resource: authz.ResourceResource, OwnerID: res.UploaderID,
	})
}

func (s *Service) buildView(ctx context.Context, viewerID string, res *Resource) (*ResourceView, error) {
	views, err := s.buildViews(ctx, viewerID, []*Resource{res})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// buildViews 批量组装视图(作者/标签/分类/收藏一次查询,防 N+1)。
func (s *Service) buildViews(ctx context.Context, viewerID string, list []*Resource) ([]*ResourceView, error) {
	if len(list) == 0 {
		return []*ResourceView{}, nil
	}
	ids := make([]string, 0, len(list))
	uploaderIDs := make([]string, 0, len(list))
	for _, res := range list {
		ids = append(ids, res.ID)
		uploaderIDs = append(uploaderIDs, res.UploaderID)
	}

	users, err := s.users.GetByIDs(ctx, uploaderIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	tagsByRes, err := s.tags.ListByResourceIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	categoryByID := map[string]*ResourceCategory{}
	if s.categories != nil {
		categories, err := s.categories.ListCategories(ctx, true)
		if err != nil {
			return nil, err
		}
		for _, c := range categories {
			categoryByID[c.ID] = c
		}
	}

	favoritedByID := map[string]bool{}
	if viewerID != "" {
		favoritedByID, err = s.repo.FavoriteResourceIDs(ctx, viewerID, ids)
		if err != nil {
			return nil, err
		}
	}

	views := make([]*ResourceView, 0, len(list))
	for _, res := range list {
		tags := tagsByRes[res.ID]
		if tags == nil {
			tags = []*tag.Tag{} // JSON 输出 [] 而非 null
		}
		view := &ResourceView{
			Resource:  res,
			Uploader:  userByID[res.UploaderID],
			Category:  categoryByID[ptrValue(res.CategoryID)],
			Tags:      tags,
			Favorited: favoritedByID[res.ID],
		}
		if res.Type == TypeFile {
			view.DownloadURL = "/api/resources/" + res.ID + "/download"
			if ptype, ok := previewByExt[strings.ToLower(path.Ext(res.OriginalName))]; ok {
				view.Preview = PreviewInfo{
					Supported: true,
					Type:      ptype,
					URL:       "/api/resources/" + res.ID + "/preview",
				}
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
