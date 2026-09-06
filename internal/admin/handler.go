// Package admin HTTP 层:管理后台端点(契约 docs/api-contract.md §管理端)。
package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
	"labnexus/internal/project"
	"labnexus/internal/user"
)

// Handler 管理后台 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册管理端路由(全部 /api/admin,仅 admin 角色可调;角色校验在 service 层)。
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api/admin")
	authed.Use(middleware.AuthRequired(secret))

	// 成员
	authed.GET("/users", h.ListUsers)
	authed.PATCH("/users/:id/role", h.ChangeRole)
	// 邀请码
	authed.GET("/invites", h.ListInvites)
	authed.POST("/invites", h.CreateInvite)
	authed.DELETE("/invites/:id", h.DeleteInvite)
	// 任务类型
	authed.GET("/task-types", h.ListTaskTypes)
	authed.POST("/task-types", h.CreateTaskType)
	authed.PATCH("/task-types/:id", h.UpdateTaskType)
	authed.DELETE("/task-types/:id", h.DeleteTaskType)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// ---- 成员 ----

func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.svc.ListUsers(c.Request.Context(), h.userID(c), c.Query("q"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (h *Handler) ChangeRole(c *gin.Context) {
	var req struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	u, err := h.svc.ChangeRole(c.Request.Context(), h.userID(c), c.Param("id"), req.Role)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// ---- 邀请码 ----

func (h *Handler) ListInvites(c *gin.Context) {
	invites, err := h.svc.ListInvites(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invites": invites})
}

func (h *Handler) CreateInvite(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	inv, err := h.svc.CreateInvite(c.Request.Context(), h.userID(c), req.Code)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invite": inv, "url": "/register?code=" + inv.Code})
}

func (h *Handler) DeleteInvite(c *gin.Context) {
	if err := h.svc.DeleteInvite(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- 任务类型 ----

func (h *Handler) ListTaskTypes(c *gin.Context) {
	list, err := h.svc.ListAllTaskTypes(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task_types": list})
}

func (h *Handler) CreateTaskType(c *gin.Context) {
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	t, err := h.svc.CreateTaskType(c.Request.Context(), h.userID(c), req.Name, req.Color)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task_type": t})
}

func (h *Handler) UpdateTaskType(c *gin.Context) {
	var req project.UpdateTaskTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	t, err := h.svc.UpdateTaskType(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task_type": t})
}

func (h *Handler) DeleteTaskType(c *gin.Context) {
	if err := h.svc.DeleteTaskType(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- 错误映射 ----

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrUserNotFound), errors.Is(err, user.ErrNotFound),
		errors.Is(err, ErrInviteNotFound), errors.Is(err, project.ErrTaskTypeNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrCannotSelfDemote), errors.Is(err, ErrLastAdmin),
		errors.Is(err, ErrInvalidRole), errors.Is(err, ErrInviteEmpty):
		respondError(c, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrInviteExists), errors.Is(err, project.ErrTaskTypeExists),
		errors.Is(err, project.ErrTaskTypeNameEmpty):
		// 重名/重码 → 409;名称空 → 400
		if errors.Is(err, project.ErrTaskTypeNameEmpty) {
			respondError(c, http.StatusBadRequest, "VALIDATION", err.Error())
			return
		}
		respondError(c, http.StatusConflict, "CONFLICT", err.Error())
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}

func respondError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"code": code, "message": message},
	})
}
