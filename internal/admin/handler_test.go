package admin_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/admin"
	"labnexus/internal/token"
	"labnexus/internal/user"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := newFixture()
	h := admin.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, f
}

func adminAuth(userID, role string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, role, 15*time.Minute)
	return "Bearer " + access
}

func admDo(t *testing.T, r *gin.Engine, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminRoutes_AuthRequired(t *testing.T) {
	r, _ := newTestRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/admin/invites"},
		{http.MethodPost, "/api/admin/invites"},
		{http.MethodGet, "/api/admin/task-types"},
	} {
		w := admDo(t, r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestAdminRoutes_NonAdminForbidden(t *testing.T) {
	r, f := newTestRouter(t)
	// supervisor & student 都有账号
	_ = f.users.seed("some-supervisor", "sup2", user.RoleSupervisor)
	for _, tc := range []struct {
		auth string
		role string
	}{
		{adminAuth("some-supervisor", "supervisor"), user.RoleSupervisor},
		{adminAuth(stu, "student"), user.RoleStudent},
	} {
		w := admDo(t, r, http.MethodGet, "/api/admin/users", "", tc.auth)
		assert.Equal(t, http.StatusForbidden, w.Code, "role=%s", tc.role)
	}
}

func TestAdminRoutes_Users(t *testing.T) {
	r, _ := newTestRouter(t)
	auth := adminAuth(adminUser, user.RoleAdmin)

	// 列表
	w := admDo(t, r, http.MethodGet, "/api/admin/users?q=boss", "", auth)
	require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
	var body struct {
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Users, 1)
	assert.Equal(t, boss, body.Users[0].ID)

	// 改角色:stu → supervisor
	w2 := admDo(t, r, http.MethodPatch, "/api/admin/users/"+stu+"/role", `{"role":"supervisor"}`, auth)
	require.Equal(t, http.StatusOK, w2.Code, "%s", w2.Body.String())

	// 非法角色 → 400
	w3 := admDo(t, r, http.MethodPatch, "/api/admin/users/"+stu+"/role", `{"role":"root"}`, auth)
	assert.Equal(t, http.StatusBadRequest, w3.Code)

	// 降自己 → 400
	w4 := admDo(t, r, http.MethodPatch, "/api/admin/users/"+adminUser+"/role", `{"role":"student"}`, auth)
	assert.Equal(t, http.StatusBadRequest, w4.Code)

	// 不存在 → 404
	w5 := admDo(t, r, http.MethodPatch, "/api/admin/users/no-such/role", `{"role":"student"}`, auth)
	assert.Equal(t, http.StatusNotFound, w5.Code)
}

func TestAdminRoutes_Invites(t *testing.T) {
	r, _ := newTestRouter(t)
	auth := adminAuth(adminUser, user.RoleAdmin)

	// 自动生成
	w := admDo(t, r, http.MethodPost, "/api/admin/invites", `{"code":""}`, auth)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		Invite struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"invite"`
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.True(t, strings.HasPrefix(created.Invite.Code, "LN-"))
	assert.Contains(t, created.URL, created.Invite.Code)

	// 自定义码
	w2 := admDo(t, r, http.MethodPost, "/api/admin/invites", `{"code":"abc"}`, auth)
	require.Equal(t, http.StatusCreated, w2.Code, "%s", w2.Body.String())

	// 重码 → 409
	w3 := admDo(t, r, http.MethodPost, "/api/admin/invites", `{"code":"abc"}`, auth)
	assert.Equal(t, http.StatusConflict, w3.Code)

	// 列表
	w4 := admDo(t, r, http.MethodGet, "/api/admin/invites", "", auth)
	require.Equal(t, http.StatusOK, w4.Code)
	assert.Contains(t, w4.Body.String(), "ABC")

	// 撤销
	w5 := admDo(t, r, http.MethodDelete, "/api/admin/invites/"+created.Invite.ID, "", auth)
	assert.Equal(t, http.StatusNoContent, w5.Code)
	// 再删 → 404
	w6 := admDo(t, r, http.MethodDelete, "/api/admin/invites/"+created.Invite.ID, "", auth)
	assert.Equal(t, http.StatusNotFound, w6.Code)
}

func TestAdminRoutes_TaskTypes(t *testing.T) {
	r, _ := newTestRouter(t)
	auth := adminAuth(adminUser, user.RoleAdmin)

	// 创建
	w := admDo(t, r, http.MethodPost, "/api/admin/task-types", `{"name":"组会汇报","color":"#ff0000"}`, auth)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		TaskType struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"task_type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "组会汇报", created.TaskType.Name)

	// 重名 → 409
	w2 := admDo(t, r, http.MethodPost, "/api/admin/task-types", `{"name":"组会汇报"}`, auth)
	assert.Equal(t, http.StatusConflict, w2.Code)

	// 空名 → 400
	w3 := admDo(t, r, http.MethodPost, "/api/admin/task-types", `{"name":"  "}`, auth)
	assert.Equal(t, http.StatusBadRequest, w3.Code)

	// 停用
	w4 := admDo(t, r, http.MethodPatch, "/api/admin/task-types/"+created.TaskType.ID, `{"active":false}`, auth)
	require.Equal(t, http.StatusOK, w4.Code, "%s", w4.Body.String())

	// 列表(含停用)
	w5 := admDo(t, r, http.MethodGet, "/api/admin/task-types", "", auth)
	require.Equal(t, http.StatusOK, w5.Code)
	assert.Contains(t, w5.Body.String(), "组会汇报")

	// 删除 → 204;再删 404
	w6 := admDo(t, r, http.MethodDelete, "/api/admin/task-types/"+created.TaskType.ID, "", auth)
	assert.Equal(t, http.StatusNoContent, w6.Code)
	w7 := admDo(t, r, http.MethodDelete, "/api/admin/task-types/"+created.TaskType.ID, "", auth)
	assert.Equal(t, http.StatusNotFound, w7.Code)
}

func TestAdminRoutes_BadBody(t *testing.T) {
	r, _ := newTestRouter(t)
	auth := adminAuth(adminUser, user.RoleAdmin)
	w := admDo(t, r, http.MethodPost, "/api/admin/invites", `not-json`, auth)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

var _ = fmt.Sprintf
