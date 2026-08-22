package tag_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/tag"
	"labnexus/internal/token"
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := tag.NewHandler(tag.NewService(newMemTagRepo()))
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r
}

func tagAuthHeader() string {
	access, _ := token.GenerateAccessToken("test-secret", "user-a", "student", 15*time.Minute)
	return "Bearer " + access
}

func tagDo(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", tagAuthHeader())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTags_RequiresAuth(t *testing.T) {
	r := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTags_Create(t *testing.T) {
	r := newTestRouter(t)
	w := tagDo(t, r, http.MethodPost, "/api/tags", `{"name":"文献-2025"}`)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"文献-2025"`)
}

func TestTags_Create_Duplicate(t *testing.T) {
	r := newTestRouter(t)
	require.Equal(t, http.StatusCreated, tagDo(t, r, http.MethodPost, "/api/tags", `{"name":"文献"}`).Code)
	w := tagDo(t, r, http.MethodPost, "/api/tags", `{"name":"文献"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "CONFLICT")
}

func TestTags_List(t *testing.T) {
	r := newTestRouter(t)
	tagDo(t, r, http.MethodPost, "/api/tags", `{"name":"A"}`)
	w := tagDo(t, r, http.MethodGet, "/api/tags", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"A"`)
}
