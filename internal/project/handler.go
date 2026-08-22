// Package project HTTP 层:F9 项目/成员/里程碑/任务(契约 docs/api-contract.md §F9)。
package project

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
	"labnexus/internal/user"
)

// Handler 项目 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F9 路由
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	authed := r.Group("/api")
	authed.Use(middleware.AuthRequired(secret))

	authed.GET("/projects", h.List)
	authed.POST("/projects", h.Create)
	authed.GET("/projects/:id", h.Get)
	authed.PATCH("/projects/:id", h.Update)
	authed.POST("/projects/:id/members", h.AddMember)
	authed.DELETE("/projects/:id/members/:user_id", h.RemoveMember)
	authed.POST("/projects/:id/milestones", h.CreateMilestone)
	authed.PATCH("/milestones/:id", h.UpdateMilestone)
	authed.GET("/projects/:id/tasks", h.ListTasks)
	authed.POST("/projects/:id/tasks", h.CreateTask)
	authed.PATCH("/tasks/:id", h.UpdateTask)
	authed.POST("/tasks/:id/transition", h.TransitionTask)
	authed.DELETE("/tasks/:id", h.DeleteTask)
}

func (h *Handler) userID(c *gin.Context) string {
	return c.GetString(middleware.ContextUserID)
}

// ---- 项目 ----

func (h *Handler) Create(c *gin.Context) {
	var req CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateProject(c.Request.Context(), h.userID(c), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"project": view})
}

func (h *Handler) List(c *gin.Context) {
	views, err := h.svc.ListProjects(c.Request.Context(), h.userID(c))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"projects": views})
}

func (h *Handler) Get(c *gin.Context) {
	view, err := h.svc.GetProject(c.Request.Context(), h.userID(c), c.Param("id"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": view})
}

func (h *Handler) Update(c *gin.Context) {
	var req UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.UpdateProject(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": view})
}

// ---- 成员 ----

func (h *Handler) AddMember(c *gin.Context) {
	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.AddMember(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"member": view})
}

func (h *Handler) RemoveMember(c *gin.Context) {
	if err := h.svc.RemoveMember(c.Request.Context(), h.userID(c), c.Param("id"), c.Param("user_id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- 里程碑 ----

func (h *Handler) CreateMilestone(c *gin.Context) {
	var req CreateMilestoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	m, err := h.svc.CreateMilestone(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"milestone": m})
}

func (h *Handler) UpdateMilestone(c *gin.Context) {
	var req UpdateMilestoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	m, err := h.svc.UpdateMilestone(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"milestone": m})
}

// ---- 任务 ----

func (h *Handler) ListTasks(c *gin.Context) {
	views, err := h.svc.ListTasks(c.Request.Context(), h.userID(c), c.Param("id"), ListFilter{
		Status:      c.Query("status"),
		AssigneeID:  c.Query("assignee_id"),
		MilestoneID: c.Query("milestone_id"),
	})
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": views})
}

func (h *Handler) CreateTask(c *gin.Context) {
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.CreateTask(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": view})
}

func (h *Handler) UpdateTask(c *gin.Context) {
	var req UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.UpdateTask(c.Request.Context(), h.userID(c), c.Param("id"), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": view})
}

func (h *Handler) TransitionTask(c *gin.Context) {
	var req TransitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	view, err := h.svc.TransitionTask(c.Request.Context(), h.userID(c), c.Param("id"), req.Status)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": view})
}

func (h *Handler) DeleteTask(c *gin.Context) {
	if err := h.svc.DeleteTask(c.Request.Context(), h.userID(c), c.Param("id")); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- 错误映射 ----

func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrProjectNotFound), errors.Is(err, ErrTaskNotFound),
		errors.Is(err, ErrMilestoneNotFound), errors.Is(err, user.ErrNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrNotMember), errors.Is(err, ErrNotOwner):
		respondError(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, ErrProjectNameEmpty), errors.Is(err, ErrTaskTitleEmpty),
		errors.Is(err, ErrMilestoneNameEmpty), errors.Is(err, ErrInvalidPriority),
		errors.Is(err, ErrInvalidTaskStatus), errors.Is(err, ErrInvalidTransition),
		errors.Is(err, ErrInvalidDate), errors.Is(err, ErrAssigneeNotMember),
		errors.Is(err, ErrMemberExists), errors.Is(err, ErrCannotRemoveOwner),
		errors.Is(err, ErrInvalidLinkType):
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
