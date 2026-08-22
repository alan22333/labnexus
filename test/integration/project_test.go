//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 项目全流程:创建(owner 自动成员)→ 加成员 → 里程碑 → 任务(指派+关联)
// → 状态机 → 详情 → 筛选 → 越权矩阵
func TestProject_FullFlow(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "项目负责人")
	tokenMember := registerUser(t, r, "pmember", "项目成员")
	tokenOutsider := registerUser(t, r, "pout", "局外人")

	// 创建项目
	w := doJSON(t, r, http.MethodPost, "/api/projects",
		`{"name":"论文-大模型方向","description":"毕业论文"}`, tokenOwner)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		Project struct {
			ID    string `json:"id"`
			Owner struct {
				DisplayName string `json:"display_name"`
			} `json:"owner"`
		} `json:"project"`
	}
	parseJSON(t, w, &created)
	pid := created.Project.ID
	assert.Equal(t, "项目负责人", created.Project.Owner.DisplayName)

	// 添加成员
	wM := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/members",
		`{"user_id":"`+userIDOf(t, r, tokenMember)+`"}`, tokenOwner)
	require.Equal(t, http.StatusCreated, wM.Code, "%s", wM.Body.String())

	// 成员创建里程碑?不,仅 owner → 403
	wMS := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"初稿"}`, tokenMember)
	assert.Equal(t, http.StatusForbidden, wMS.Code)
	// owner 创建里程碑
	wMO := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/milestones",
		`{"name":"初稿","due_date":"2026-09-01"}`, tokenOwner)
	require.Equal(t, http.StatusCreated, wMO.Code)

	// 创建任务(指派给 member,关联一个文档)
	docID := createDoc(t, r, tokenOwner, "任务相关文档", "private")
	wT := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		fmt.Sprintf(`{"title":"写综述","assignee_id":%q,"priority":"high","due_date":"2026-08-30","link_document_id":%q}`,
			userIDOf(t, r, tokenMember), docID), tokenOwner)
	require.Equal(t, http.StatusCreated, wT.Code, "%s", wT.Body.String())
	var task struct {
		Task struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Links  []any  `json:"links"`
		} `json:"task"`
	}
	parseJSON(t, wT, &task)
	assert.Equal(t, "todo", task.Task.Status)
	require.Len(t, task.Task.Links, 1, "任务应关联文档")

	// 状态机:todo→in_progress→done;done 后 → 400
	w1 := doJSON(t, r, http.MethodPost, "/api/tasks/"+task.Task.ID+"/transition",
		`{"status":"in_progress"}`, tokenMember) // assignee 可流转
	require.Equal(t, http.StatusOK, w1.Code)
	w2 := doJSON(t, r, http.MethodPost, "/api/tasks/"+task.Task.ID+"/transition",
		`{"status":"done"}`, tokenOwner)
	require.Equal(t, http.StatusOK, w2.Code)
	w3 := doJSON(t, r, http.MethodPost, "/api/tasks/"+task.Task.ID+"/transition",
		`{"status":"todo"}`, tokenOwner)
	assert.Equal(t, http.StatusBadRequest, w3.Code)

	// 任务列表筛选(status=done)
	wList := doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/tasks?status=done", "", tokenMember)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "写综述")

	// 详情:成员可见,含全部子资源
	wGet := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenMember)
	require.Equal(t, http.StatusOK, wGet.Code)
	assert.Contains(t, wGet.Body.String(), `"milestones"`)
	assert.Contains(t, wGet.Body.String(), `"tasks"`)

	// 越权矩阵
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenOutsider).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodPatch, "/api/projects/"+pid,
		`{"name":"hack"}`, tokenMember).Code)
	assert.Equal(t, http.StatusForbidden, doJSON(t, r, http.MethodDelete, "/api/tasks/"+task.Task.ID, "", tokenMember).Code)

	// 列表仅本人参与
	wListP := doJSON(t, r, http.MethodGet, "/api/projects", "", tokenOutsider)
	assert.NotContains(t, wListP.Body.String(), "论文-大模型方向")
	wListM := doJSON(t, r, http.MethodGet, "/api/projects", "", tokenMember)
	assert.Contains(t, wListM.Body.String(), "论文-大模型方向")
}

func userIDOf(t *testing.T, r *gin.Engine, token string) string {
	t.Helper()
	w := doJSON(t, r, http.MethodGet, "/api/me", "", token)
	var body struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal([]byte(w.Body.String()), &body))
	return body.User.ID
}

var _ = json.Marshal
