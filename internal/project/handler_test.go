package project_test

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

	"labnexus/internal/project"
	"labnexus/internal/token"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	h := project.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, f
}

func authHeader(userID string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, "student", 15*time.Minute)
	return "Bearer " + access
}

func pjDo(t *testing.T, r *gin.Engine, method, path, body, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if tokenStr != "" {
		req.Header.Set("Authorization", tokenStr)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func createProjectViaAPI(t *testing.T, r *gin.Engine, userID, name string) string {
	t.Helper()
	w := pjDo(t, r, http.MethodPost, "/api/projects",
		fmt.Sprintf(`{"name":%q}`, name), authHeader(userID))
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var body struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Project.ID
}

func TestProject_RequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/projects"},
		{http.MethodPost, "/api/projects"},
		{http.MethodGet, "/api/projects/x"},
		{http.MethodPost, "/api/projects/x/tasks"},
	} {
		w := pjDo(t, r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestProject_FullLifecycle(t *testing.T) {
	r, f := newTestRouter(t)
	f.users.seed(owner)
	f.users.seed(member)
	f.users.seed(other)

	// 创建项目
	pid := createProjectViaAPI(t, r, owner, "论文项目")

	// 添加成员
	w := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		fmt.Sprintf(`{"user_id":%q}`, member), authHeader(owner))
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())

	// 建里程碑
	wM := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"初稿","due_date":"2026-09-01"}`, authHeader(owner))
	require.Equal(t, http.StatusCreated, wM.Code)

	// 建任务(指派给 member)
	wT := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		fmt.Sprintf(`{"title":"写综述","assignee_id":%q,"priority":"high","due_date":"2026-08-30"}`, member),
		authHeader(owner))
	require.Equal(t, http.StatusCreated, wT.Code, "%s", wT.Body.String())
	assert.Contains(t, wT.Body.String(), `"status":"todo"`)

	// 详情(成员可见)
	wGet := pjDo(t, r, http.MethodGet, "/api/projects/"+pid, "", authHeader(member))
	require.Equal(t, http.StatusOK, wGet.Code)
	assert.Contains(t, wGet.Body.String(), "写综述")
	assert.Contains(t, wGet.Body.String(), `"milestones"`)

	// 非成员访问 → 403
	assert.Equal(t, http.StatusForbidden, pjDo(t, r, http.MethodGet, "/api/projects/"+pid, "", authHeader(other)).Code)
}

func TestProject_TaskStateMachineViaAPI(t *testing.T) {
	r, f := newTestRouter(t)
	f.users.seed(owner)
	f.users.seed(member)
	pid := createProjectViaAPI(t, r, owner, "P")
	pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		fmt.Sprintf(`{"user_id":%q}`, member), authHeader(owner))

	wT := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		fmt.Sprintf(`{"title":"任务","assignee_id":%q}`, member), authHeader(owner))
	var body struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	require.NoError(t, json.Unmarshal(wT.Body.Bytes(), &body))
	tid := body.Task.ID

	// 合法迁移:todo → in_progress → done
	w1 := pjDo(t, r, http.MethodPost, "/api/tasks/"+tid+"/transition",
		`{"status":"in_progress"}`, authHeader(owner))
	require.Equal(t, http.StatusOK, w1.Code)
	w2 := pjDo(t, r, http.MethodPost, "/api/tasks/"+tid+"/transition",
		`{"status":"done"}`, authHeader(owner))
	require.Equal(t, http.StatusOK, w2.Code)

	// done 后再迁 → 400
	w3 := pjDo(t, r, http.MethodPost, "/api/tasks/"+tid+"/transition",
		`{"status":"todo"}`, authHeader(owner))
	assert.Equal(t, http.StatusBadRequest, w3.Code)

	// 非成员不能操作任务
	pid2 := createProjectViaAPI(t, r, owner, "P2")
	wT2 := pjDo(t, r, http.MethodPost, "/api/projects/"+pid2+"/tasks",
		`{"title":"t"}`, authHeader(owner))
	var b2 struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	require.NoError(t, json.Unmarshal(wT2.Body.Bytes(), &b2))
	wDel := pjDo(t, r, http.MethodDelete, "/api/tasks/"+b2.Task.ID, "", authHeader(member))
	assert.Equal(t, http.StatusForbidden, wDel.Code, "非 owner 不能删任务")
}

func TestProject_Validation(t *testing.T) {
	r, f := newTestRouter(t)
	f.users.seed(owner)

	// 空项目名
	w := pjDo(t, r, http.MethodPost, "/api/projects", `{"name":""}`, authHeader(owner))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 非法日期
	pid := createProjectViaAPI(t, r, owner, "P")
	w2 := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"m","due_date":"bad-date"}`, authHeader(owner))
	assert.Equal(t, http.StatusBadRequest, w2.Code)

	// 非法优先级
	w3 := pjDo(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		`{"title":"t","priority":"urgent"}`, authHeader(owner))
	assert.Equal(t, http.StatusBadRequest, w3.Code)

	// 移除 owner 自己 → 400
	w4 := pjDo(t, r, http.MethodDelete, "/api/projects/"+pid+"/members/"+owner, "", authHeader(owner))
	assert.Equal(t, http.StatusBadRequest, w4.Code)
}
