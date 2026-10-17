// Package resource HTTP 层:F7 资源库(link + file;契约 docs/api-contract.md §F7)。
package resource

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
	"labnexus/internal/tag"
)

// Handler 资源 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F7 路由(注意:download/preview 为 :id 的子路径,无冲突)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	authed.GET("/resources", h.List)
	authed.POST("/resources", h.Create)
	authed.POST("/resources/upload", h.Upload)
	authed.GET("/resources/:id", h.Get)
	authed.POST("/resources/:id/view", h.View)
	authed.GET("/resources/:id/download", h.Download)
	authed.GET("/resources/:id/preview", h.Preview)
	authed.PATCH("/resources/:id", h.Update)
	authed.DELETE("/resources/:id", h.Delete)
	authed.POST("/resources/:id/favorite", h.Favorite)
	authed.DELETE("/resources/:id/favorite", h.Unfavorite)
	authed.GET("/resource-categories", h.ListCategories)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// List 资源列表(筛选 + 排序 + 分页)
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = NormalizePage(page, pageSize)

	uncategorized, err := queryBool(c, "uncategorized")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid uncategorized")
		return
	}
	mine, err := queryBool(c, "mine")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid mine")
		return
	}
	favorited, err := queryBool(c, "favorited")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid favorited")
		return
	}
	pinned, err := queryBool(c, "pinned")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid pinned")
		return
	}

	viewerID := h.userID(c)
	uploaderID := c.Query("uploader_id")
	if mine {
		uploaderID = viewerID
	}
	filter := ListFilter{
		Type:          c.Query("type"),
		TagID:         c.Query("tag_id"),
		TagIDs:        splitCSV(c.Query("tag_ids")),
		Keyword:       c.Query("keyword"),
		CategoryID:    c.Query("category_id"),
		Uncategorized: uncategorized,
		UploaderID:    uploaderID,
		PinnedOnly:    pinned,
		Sort:          c.DefaultQuery("sort", SortLatest),
		ViewerID:      viewerID,
		Page:          page,
		PageSize:      pageSize,
	}
	if favorited {
		filter.FavoritedBy = viewerID
	}

	views, total, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"resources": views,
		"pagination": gin.H{
			"page": page, "page_size": pageSize, "total": total,
		},
	})
}

// Create 创建 link 资源(paper 已废弃;支持分类、标签与 URL 去重)
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Type        string   `json:"type"`
		Title       string   `json:"title"`
		URL         string   `json:"url"`
		Description string   `json:"description"`
		CategoryID  *string  `json:"category_id"`
		TagIDs      []string `json:"tag_ids"`
		Force       bool     `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	if req.Type != TypeLink {
		respondError(c, http.StatusBadRequest, "VALIDATION", ErrInvalidType.Error())
		return
	}
	view, err := h.svc.CreateLink(c.Request.Context(), h.userID(c), CreateLinkRequest{
		Title: req.Title, URL: req.URL, Description: req.Description,
		CategoryID: req.CategoryID, TagIDs: req.TagIDs, Force: req.Force,
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"resource": view})
}

// Upload 上传文件资源(multipart: file 必填,title/description/category_id/tag_ids 可选)
func (h *Handler) Upload(c *gin.Context) {
	// 整体请求体上限,防伪造大小/超大上传
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadBody)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "file field is required")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		respondServiceError(c, err)
		return
	}
	defer f.Close()

	var tagIDs []string
	if raw := c.PostForm("tag_ids"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &tagIDs); err != nil {
			respondError(c, http.StatusBadRequest, "VALIDATION", "invalid tag_ids")
			return
		}
	}
	var categoryID *string
	if raw := strings.TrimSpace(c.PostForm("category_id")); raw != "" {
		categoryID = &raw
	}

	view, err := h.svc.UploadFileWithCategory(c.Request.Context(), h.userID(c),
		fileHeader.Filename, f, c.PostForm("title"), c.PostForm("description"), categoryID, tagIDs)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"resource": view})
}

// Get 资源详情(携带当前用户收藏视角)
func (h *Handler) Get(c *gin.Context) {
	view, err := h.svc.GetForUser(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resource": view})
}

// View 记录浏览(计数失败不阻断;资源不存在仍 404)
func (h *Handler) View(c *gin.Context) {
	err := h.svc.RecordView(c.Request.Context(), c.Param("id"))
	if errors.Is(err, ErrResourceNotFound) {
		respondServiceError(c, err)
		return
	}
	if err != nil {
		slog.Warn("record resource view failed", "resource_id", c.Param("id"), "error", err)
	}
	c.Status(http.StatusNoContent)
}

// Download 下载文件(仅 file;attachment + 原始文件名;成功返回后计数)
func (h *Handler) Download(c *gin.Context) {
	res, file, err := h.svc.OpenFile(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	defer file.Close()

	name := sanitizeFilename(res.OriginalName)
	c.Header("Content-Type", res.MimeType)
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(c.Writer, c.Request, name, res.UpdatedAt, file)

	if err := h.svc.RecordDownload(c.Request.Context(), res.ID); err != nil {
		slog.Warn("record resource download failed", "resource_id", res.ID, "error", err)
	}
}

// Preview 预览文件(仅 file 且支持预览类型;inline)
func (h *Handler) Preview(c *gin.Context) {
	res, file, err := h.svc.OpenFile(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	defer file.Close()

	name := sanitizeFilename(res.OriginalName)
	ptype, ok := previewByExt[strings.ToLower(path.Ext(res.OriginalName))]
	if !ok {
		respondError(c, http.StatusBadRequest, "PREVIEW_UNSUPPORTED", ErrPreviewUnsupported.Error())
		return
	}
	contentType := res.MimeType
	switch {
	case ptype == "sheet":
		contentType = "application/octet-stream" // 前端用 xlsx 解析渲染
	case strings.HasPrefix(res.MimeType, "text/"):
		contentType = "text/plain; charset=utf-8" // 防 HTML 注入
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `inline; filename="`+name+`"`)
	http.ServeContent(c.Writer, c.Request, name, res.UpdatedAt, file)
}

// Update 修改资源(上传者或 admin)
func (h *Handler) Update(c *gin.Context) {
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.Update(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resource": view})
}

// Delete 删除资源
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Favorite 收藏(幂等)
func (h *Handler) Favorite(c *gin.Context) {
	if err := h.svc.Favorite(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unfavorite 取消收藏(幂等)
func (h *Handler) Unfavorite(c *gin.Context) {
	if err := h.svc.Unfavorite(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListCategories 分类字典(登录可见;只返回 active)
func (h *Handler) ListCategories(c *gin.Context) {
	list, err := h.svc.ListCategories(c.Request.Context(), false)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"categories": list})
}

// NormalizePage 分页归一化(与列表语义一致)。
func NormalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

// sanitizeFilename 去除文件名中的路径分隔符与引号,防止 header 注入。
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, `'`)
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, `\`, "_")
	return name
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func queryBool(c *gin.Context, key string) (bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func respondServiceError(c *gin.Context, err error) {
	var dup *DuplicateLinkError
	if errors.As(err, &dup) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"error":       gin.H{"code": "CONFLICT", "message": err.Error()},
			"existing_id": dup.ExistingID,
		})
		return
	}
	switch {
	case errors.Is(err, ErrResourceNotFound), errors.Is(err, ErrCategoryNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrResourceForbidden):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrCategoryExists):
		respondError(c, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, ErrInvalidType), errors.Is(err, ErrTitleEmpty),
		errors.Is(err, ErrURLRequired), errors.Is(err, ErrInvalidURL),
		errors.Is(err, ErrNotFile), errors.Is(err, ErrFileTooLarge),
		errors.Is(err, ErrFileTypeNotAllowed), errors.Is(err, ErrFileContentMismatch),
		errors.Is(err, ErrPreviewUnsupported), errors.Is(err, ErrInvalidSort),
		errors.Is(err, ErrInvalidCategory), errors.Is(err, ErrCategoryNameEmpty),
		errors.Is(err, tag.ErrTagNotFound):
		code := "VALIDATION"
		if errors.Is(err, ErrPreviewUnsupported) {
			code = "PREVIEW_UNSUPPORTED"
		}
		respondError(c, http.StatusBadRequest, code, err.Error())
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"code": code, "message": message},
	})
}
