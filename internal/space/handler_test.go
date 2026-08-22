package space_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/space"
	"labnexus/internal/token"
)

func newTestRouter(t *testing.T) (*gin.Engine, *memSpaceRepo, *memFolderRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	spaces := newMemSpaceRepo()
	folders := newMemFolderRepo()
	svc := space.NewService(spaces, folders)
	h := space.NewHandler(svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, spaces, folders
}

func authHeader(userID string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, "student", 15*time.Minute)
	return "Bearer " + access
}

func do(t *testing.T, r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSpace_RequiresAuth(t *testing.T) {
	r, _, _ := newTestRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/me/space"},
		{http.MethodPost, "/api/me/folders"},
		{http.MethodPatch, "/api/me/folders/x"},
		{http.MethodDelete, "/api/me/folders/x"},
	} {
		w := do(t, r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestSpace_GetSpace(t *testing.T) {
	r, spaces, folders := newTestRouter(t)
	sp := seedSpace(t, spaces, userA)
	root := seedFolder(t, folders, sp.ID, nil, "会议记录")
	seedFolder(t, folders, sp.ID, &root.ID, "组会")

	w := do(t, r, http.MethodGet, "/api/me/space", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"会议记录"`)
	assert.Contains(t, w.Body.String(), `"children"`)
}

func TestSpace_CreateFolder(t *testing.T) {
	r, spaces, _ := newTestRouter(t)
	seedSpace(t, spaces, userA)

	w := do(t, r, http.MethodPost, "/api/me/folders", `{"name":"近期工作"}`, authHeader(userA))
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"近期工作"`)
}

func TestSpace_CreateFolder_EmptyName(t *testing.T) {
	r, spaces, _ := newTestRouter(t)
	seedSpace(t, spaces, userA)

	w := do(t, r, http.MethodPost, "/api/me/folders", `{"name":""}`, authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "VALIDATION")
}

func TestSpace_UpdateFolder(t *testing.T) {
	r, spaces, folders := newTestRouter(t)
	sp := seedSpace(t, spaces, userA)
	f := seedFolder(t, folders, sp.ID, nil, "旧名")

	w := do(t, r, http.MethodPatch, "/api/me/folders/"+f.ID, `{"name":"新名"}`, authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"新名"`)
}

func TestSpace_DeleteFolder_Empty(t *testing.T) {
	r, spaces, folders := newTestRouter(t)
	sp := seedSpace(t, spaces, userA)
	f := seedFolder(t, folders, sp.ID, nil, "空目录")

	w := do(t, r, http.MethodDelete, "/api/me/folders/"+f.ID, "", authHeader(userA))
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestSpace_DeleteFolder_NotEmpty(t *testing.T) {
	r, spaces, folders := newTestRouter(t)
	sp := seedSpace(t, spaces, userA)
	root := seedFolder(t, folders, sp.ID, nil, "父")
	seedFolder(t, folders, sp.ID, &root.ID, "子")

	w := do(t, r, http.MethodDelete, "/api/me/folders/"+root.ID, "", authHeader(userA))
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "CONFLICT")
}

func TestSpace_DeleteFolder_NotOwned(t *testing.T) {
	r, spaces, folders := newTestRouter(t)
	seedSpace(t, spaces, userA)
	spB := seedSpace(t, spaces, userB)
	other := seedFolder(t, folders, spB.ID, nil, "别人的")

	w := do(t, r, http.MethodDelete, "/api/me/folders/"+other.ID, "", authHeader(userA))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// 引用 context 避免未使用告警(seed 函数在 service_test 中)
var _ = context.Background
