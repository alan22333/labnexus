// Package resource HTTP 层:F7 资源库 + F8 文献元数据(契约 docs/api-contract.md §F7/§F8)。
package resource

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

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

// RegisterRoutes 注册 F7/F8 路由(注意:paper/meta 静态路由须在 :id 之前注册)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	authed.GET("/resources", h.List)
	authed.POST("/resources", h.Create)
	authed.POST("/resources/upload", h.Upload)
	authed.GET("/resources/paper/meta", h.PaperMeta) // 静态路由在前
	authed.GET("/resources/:id", h.Get)
	authed.PATCH("/resources/:id", h.Update)
	authed.DELETE("/resources/:id", h.Delete)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// List 资源列表(筛选 + 分页)
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = NormalizePage(page, pageSize)

	views, total, err := h.svc.List(c.Request.Context(), ListFilter{
		Type:     c.Query("type"),
		TagID:    c.Query("tag_id"),
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
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

// Create 创建 link/paper 资源
func (h *Handler) Create(c *gin.Context) {
	// 平铺请求结构(避免嵌入结构同名字段冲突导致 JSON 解析丢失)
	var req struct {
		Type     string         `json:"type"`
		Title    string         `json:"title"`
		URL      string         `json:"url"`
		DOI      string         `json:"doi"`
		ArxivID  string         `json:"arxiv_id"`
		Metadata map[string]any `json:"metadata"`
		TagIDs   []string       `json:"tag_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	var (
		view *ResourceView
		err  error
	)
	switch req.Type {
	case TypeLink:
		view, err = h.svc.CreateLink(c.Request.Context(), h.userID(c), CreateLinkRequest{
			Title: req.Title, URL: req.URL, TagIDs: req.TagIDs,
		})
	case TypePaper:
		view, err = h.svc.CreatePaper(c.Request.Context(), h.userID(c), CreatePaperRequest{
			Title: req.Title, DOI: req.DOI, ArxivID: req.ArxivID,
			Metadata: req.Metadata, TagIDs: req.TagIDs,
		})
	default:
		respondError(c, http.StatusBadRequest, "VALIDATION", ErrInvalidType.Error())
		return
	}
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"resource": view})
}

// Upload 上传文件资源(multipart: file 必填,title/tag_ids 可选)
func (h *Handler) Upload(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "file field is required")
		return
	}
	if fileHeader.Size > MaxFileSize {
		respondError(c, http.StatusBadRequest, "VALIDATION", ErrFileTooLarge.Error())
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

	view, err := h.svc.UploadFile(c.Request.Context(), h.userID(c),
		fileHeader.Filename, f, c.PostForm("title"), tagIDs)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"resource": view})
}

// PaperMeta 文献元数据抓取(F8)
func (h *Handler) PaperMeta(c *gin.Context) {
	meta, err := h.svc.FetchPaperMeta(c.Request.Context(), c.Query("doi"), c.Query("arxiv_id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"metadata": meta})
}

// Get 资源详情
func (h *Handler) Get(c *gin.Context) {
	view, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"resource": view})
}

// Update 修改资源
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

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrResourceNotFound), errors.Is(err, ErrPaperMetaNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrResourceForbidden):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrPaperMetaUpstream):
		respondError(c, http.StatusBadGateway, "BAD_GATEWAY", err.Error())
	case errors.Is(err, ErrInvalidType), errors.Is(err, ErrTitleEmpty),
		errors.Is(err, ErrURLRequired), errors.Is(err, ErrDOIOrArxivRequired),
		errors.Is(err, ErrFileTooLarge), errors.Is(err, ErrFileTypeNotAllowed),
		errors.Is(err, tag.ErrTagNotFound):
		respondError(c, http.StatusBadRequest, "VALIDATION", err.Error())
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"code": code, "message": message},
	})
}
