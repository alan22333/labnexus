// Package middleware Gin 中间件:JWT 认证。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"labnexus/internal/token"
)

// context 键(handler 通过 c.Get 读取当前用户)
const (
	ContextUserID = "userID"
	ContextRole   = "role"
)

// AuthRequired 校验 Authorization: Bearer <access_token>,通过后注入 userID/role。
// 权限语义统一收敛在业务层 can()(PRD §3.3),此处只做身份认证。
func AuthRequired(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authz := c.GetHeader("Authorization")
		tokenStr, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || tokenStr == "" {
			abortUnauthorized(c)
			return
		}
		claims, err := token.ParseAccessToken(secret, tokenStr)
		if err != nil {
			abortUnauthorized(c)
			return
		}
		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextRole, claims.Role)
		c.Next()
	}
}

func abortUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{"code": "AUTH_REQUIRED", "message": "authentication required"},
	})
}
