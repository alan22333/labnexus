// Package document HTTP 层:F3 文档 + F4 信息流/点赞/评论 + F5 标签内容页。
// 契约:docs/api-contract.md §F3/§F4/§F5。
package document

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
	"labnexus/internal/space"
	"labnexus/internal/tag"
)

// Handler 文档/信息流 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F3/F4/F5(标签内容页)路由
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	// F3 文档
	authed.GET("/me/documents", h.ListMy)
	authed.POST("/me/documents", h.Create)
	authed.GET("/documents/:id", h.Get)
	authed.PATCH("/documents/:id", h.Update)
	authed.DELETE("/documents/:id", h.Delete)

	// F4 信息流
	authed.GET("/feed", h.Feed)
	authed.POST("/documents/:id/reactions", h.ToggleReaction)
	authed.GET("/documents/:id/comments", h.ListComments)
	authed.POST("/documents/:id/comments", h.CreateComment)
	authed.DELETE("/comments/:id", h.DeleteComment)

	// F5 标签内容页(依赖倒置:由拥有内容聚合能力的 document 模块注册)
	authed.GET("/tags/:id/contents", h.TagContents)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// ---- F3 ----

func (h *Handler) Create(c *gin.Context) {
	var req CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateDocument(c.Request.Context(), h.userID(c), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"document": view})
}

func (h *Handler) ListMy(c *gin.Context) {
	var folderID *string
	if f := c.Query("folder_id"); f != "" {
		folderID = &f
	}
	views, err := h.svc.ListMyDocuments(c.Request.Context(), h.userID(c), folderID, c.Query("visibility"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": views})
}

func (h *Handler) Get(c *gin.Context) {
	view, err := h.svc.GetDocument(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": view})
}

func (h *Handler) Update(c *gin.Context) {
	var req UpdateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.UpdateDocument(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document": view})
}

func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.DeleteDocument(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- F4 ----

func (h *Handler) Feed(c *gin.Context) {
	sortMode := c.DefaultQuery("sort", "latest")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	views, total, err := h.svc.GetFeed(c.Request.Context(), sortMode, page, pageSize)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"documents": views,
		"pagination": gin.H{
			"page": page, "page_size": pageSize, "total": total,
		},
	})
}

type toggleReactionReq struct {
	Emoji string `json:"emoji"`
}

func (h *Handler) ToggleReaction(c *gin.Context) {
	var req toggleReactionReq
	_ = c.ShouldBindJSON(&req) // emoji 可选,默认 👍
	if err := h.svc.ToggleReaction(c.Request.Context(), h.userID(c), c.Param("id"), req.Emoji); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListComments(c *gin.Context) {
	views, err := h.svc.ListComments(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"comments": views})
}

type createCommentReq struct {
	Content    string  `json:"content"`
	ReplyToID  *string `json:"reply_to_id"`
}

func (h *Handler) CreateComment(c *gin.Context) {
	var req createCommentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateComment(c.Request.Context(), h.userID(c), c.Param("id"), req.Content, req.ReplyToID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"comment": view})
}

func (h *Handler) DeleteComment(c *gin.Context) {
	if err := h.svc.DeleteComment(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- F5 标签内容页 ----

func (h *Handler) TagContents(c *gin.Context) {
	views, err := h.svc.ListByTag(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": views})
}

// ---- 错误映射 ----

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrDocumentNotFound), errors.Is(err, ErrCommentNotFound),
		errors.Is(err, space.ErrSpaceNotFound), errors.Is(err, space.ErrFolderNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrDocumentForbidden), errors.Is(err, ErrCommentForbidden),
		errors.Is(err, space.ErrFolderNotOwned):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrTitleEmpty), errors.Is(err, ErrInvalidVisibility),
		errors.Is(err, ErrContentEmpty), errors.Is(err, ErrInvalidReply),
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
