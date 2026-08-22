package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/middleware"
	"labnexus/internal/token"
)

const secret = "test-secret"

func newAuthedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.AuthRequired(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"userID": c.GetString(middleware.ContextUserID),
			"role":   c.GetString(middleware.ContextRole),
		})
	})
	return r
}

func TestMiddleware_NoToken(t *testing.T) {
	r := newAuthedRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/protected", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "AUTH_REQUIRED")
}

func TestMiddleware_InvalidToken(t *testing.T) {
	r := newAuthedRouter()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_ValidToken(t *testing.T) {
	r := newAuthedRouter()
	access, err := token.GenerateAccessToken(secret, "user-1", "student", 15*time.Minute)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"userID":"user-1"`)
	assert.Contains(t, w.Body.String(), `"role":"student"`)
}

func TestMiddleware_ExpiredToken(t *testing.T) {
	r := newAuthedRouter()
	access, err := token.GenerateAccessToken(secret, "user-1", "student", -time.Minute)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
