// Package space HTTP 层:F2 空间/目录端点(契约 docs/api-contract.md §F2)。
package space

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
)

// Handler 空间/目录 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F2 路由(契约 §F2)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	authed.GET("/me/space", h.GetSpace)
	authed.POST("/me/folders", h.CreateFolder)
	authed.PATCH("/me/folders/:id", h.UpdateFolder)
	authed.DELETE("/me/folders/:id", h.DeleteFolder)
}

// GetSpace 获取当前用户空间与目录树
func (h *Handler) GetSpace(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	sp, tree, err := h.svc.GetSpace(c.Request.Context(), userID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"space": sp, "folders": tree})
}

type createFolderReq struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

// CreateFolder 创建目录
func (h *Handler) CreateFolder(c *gin.Context) {
	var req createFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	f, err := h.svc.CreateFolder(c.Request.Context(), userID, req.Name, req.ParentID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"folder": f})
}

type updateFolderReq struct {
	Name      *string `json:"name"`
	SortOrder *int    `json:"sort_order"`
}

// UpdateFolder 修改目录名称/排序
func (h *Handler) UpdateFolder(c *gin.Context) {
	var req updateFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	f, err := h.svc.UpdateFolder(c.Request.Context(), userID, c.Param("id"), req.Name, req.SortOrder)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"folder": f})
}

// DeleteFolder 删除目录(仅空目录)
func (h *Handler) DeleteFolder(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.DeleteFolder(c.Request.Context(), userID, c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// respondServiceError 哨兵错误 → HTTP(契约 §通用约定)
func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSpaceNotFound), errors.Is(err, ErrFolderNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrFolderNotOwned):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrFolderNotEmpty):
		respondError(c, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, ErrFolderNameEmpty):
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
