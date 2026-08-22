// Package auth HTTP 层:账号相关端点(契约 docs/api-contract.md §F1)。
package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/middleware"
)

// RefreshCookieName refresh token 的 httpOnly cookie 名(契约约定 ln_refresh)
const RefreshCookieName = "ln_refresh"

// Handler 账号 HTTP handler
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes 注册 F1 全部路由(契约 §F1)
func (h *Handler) RegisterRoutes(r *gin.Engine, secret string) {
	api := r.Group("/api")

	api.POST("/auth/register", h.Register)
	api.POST("/auth/login", h.Login)
	api.POST("/auth/refresh", h.Refresh)
	api.POST("/auth/logout", h.Logout)

	authed := api.Group("")
	authed.Use(middleware.AuthRequired(secret))
	authed.GET("/me", h.Me)
	authed.PATCH("/me", h.UpdateMe)
}

// Register 注册(注册即登录,返回 access token;refresh 写 cookie)
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	res, err := h.svc.Register(c.Request.Context(), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	h.setRefreshCookie(c, res.RefreshToken)
	c.JSON(http.StatusCreated, gin.H{"access_token": res.AccessToken, "user": res.User})
}

// Login 登录
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	res, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	h.setRefreshCookie(c, res.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": res.AccessToken, "user": res.User})
}

// Refresh 刷新 access token(读 cookie 中的 refresh,轮换)
func (h *Handler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshCookieName)
	if err != nil || refreshToken == "" {
		respondError(c, http.StatusUnauthorized, "AUTH_REQUIRED", "refresh token required")
		return
	}
	res, err := h.svc.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	h.setRefreshCookie(c, res.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": res.AccessToken})
}

// Logout 登出(撤销 refresh,清除 cookie)
func (h *Handler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie(RefreshCookieName)
	if err != nil || refreshToken == "" {
		respondError(c, http.StatusUnauthorized, "AUTH_REQUIRED", "refresh token required")
		return
	}
	if err := h.svc.Logout(c.Request.Context(), refreshToken); err != nil {
		respondServiceError(c, err)
		return
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

// Me 当前用户信息
func (h *Handler) Me(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	u, err := h.svc.Me(c.Request.Context(), userID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// UpdateMe 修改个人资料
func (h *Handler) UpdateMe(c *gin.Context) {
	var req UpdateMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	u, err := h.svc.UpdateMe(c.Request.Context(), userID, req)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// ---- cookie 与错误响应 ----

func (h *Handler) setRefreshCookie(c *gin.Context, refreshToken string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int(h.svc.cfg.RefreshTokenTTL.Seconds()),
		SameSite: http.SameSiteLaxMode,
		// Secure: 生产 HTTPS 时开启(见 standards §9)
	})
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		SameSite: http.SameSiteLaxMode,
	})
}

// respondServiceError 哨兵错误 → HTTP(契约 §通用约定)
func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidInvite):
		respondError(c, http.StatusUnauthorized, "INVALID_INVITE", err.Error())
	case errors.Is(err, ErrWeakPassword):
		respondError(c, http.StatusBadRequest, "VALIDATION", err.Error())
	case errors.Is(err, ErrUsernameTaken):
		respondError(c, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		respondError(c, http.StatusUnauthorized, "AUTH_FAILED", err.Error())
	case errors.Is(err, ErrInvalidRefreshToken):
		respondError(c, http.StatusUnauthorized, "AUTH_REQUIRED", err.Error())
	case errors.Is(err, ErrUserNotFound):
		respondError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, ErrOldPasswordMismatch):
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
