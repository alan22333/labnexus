//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F9 v2:任务类型字典 / 加成员候选 / 进度统计(specs/f9-project.md §8)
func TestTaskTypes_ListSeeded(t *testing.T) {
	r := setupServer(t)
	token := registerUser(t, r, "ttuser", "类型用户")

	// 未登录 → 401
	assert.Equal(t, http.StatusUnauthorized, doJSON(t, r, http.MethodGet, "/api/task-types", "", "").Code)

	w := doJSON(t, r, http.MethodGet, "/api/task-types", "", token)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		TaskTypes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"task_types"`
	}
	parseJSON(t, w, &body)
	require.NotEmpty(t, body.TaskTypes, "内置类型应已种子化")
	names := map[string]bool{}
	for _, tt := range body.TaskTypes {
		names[tt.Name] = true
	}
	for _, want := range []string{"实验", "文献", "写作", "代码", "其他"} {
		assert.True(t, names[want], "缺内置类型: %s", want)
	}
}

func TestTask_CreateWithType(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	pid := createProjectViaAPI(t, r, tokenOwner, "类型项目")

	// 取一个内置类型 id
	var types struct {
		TaskTypes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"task_types"`
	}
	parseJSON(t, doJSON(t, r, http.MethodGet, "/api/task-types", "", tokenOwner), &types)
	require.NotEmpty(t, types.TaskTypes)
	typeID := types.TaskTypes[0].ID

	// 合法 type_id → 201,响应含 type 对象
	w := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		fmt.Sprintf(`{"title":"带类型的任务","type_id":%q}`, typeID), tokenOwner)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		Task struct {
			ID     string `json:"id"`
			TypeID string `json:"type_id"`
			Type   struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"type"`
		} `json:"task"`
	}
	parseJSON(t, w, &created)
	assert.Equal(t, typeID, created.Task.TypeID)
	assert.Equal(t, typeID, created.Task.Type.ID)
	assert.Equal(t, types.TaskTypes[0].Name, created.Task.Type.Name)

	// 非法 type_id → 400 VALIDATION
	wBad := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks",
		`{"title":"坏类型","type_id":"11111111-1111-1111-1111-111111111111"}`, tokenOwner)
	assert.Equal(t, http.StatusBadRequest, wBad.Code, "%s", wBad.Body.String())
	assert.Equal(t, "VALIDATION", errorCode(t, wBad))

	// 详情任务列表含 type 对象
	wGet := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenOwner)
	assert.Contains(t, wGet.Body.String(), `"type"`)

	// PATCH 清空类型(传空串)
	wPatch := doJSON(t, r, http.MethodPatch, "/api/tasks/"+created.Task.ID,
		`{"type_id":""}`, tokenOwner)
	require.Equal(t, http.StatusOK, wPatch.Code, "%s", wPatch.Body.String())
	assert.NotContains(t, wPatch.Body.String(), `"type_id"`)
}

func TestProjectCandidates_OwnerOnly(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	tokenMember := registerUser(t, r, "pmember", "成员")
	tokenOther := registerUser(t, r, "pother", "未入组")
	pid := createProjectViaAPI(t, r, tokenOwner, "候选项目")
	addMemberViaAPI(t, r, tokenOwner, pid, tokenMember)

	// owner → 200:含未入组 pother,不含已有成员 owner/member
	w := doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/candidates", "", tokenOwner)
	require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "未入组")
	assert.NotContains(t, w.Body.String(), userIDOf(t, r, tokenMember))
	// owner 自己在 members 里,也不应出现
	assert.NotContains(t, w.Body.String(), userIDOf(t, r, tokenOwner))

	// member / 局外人 → 403
	assert.Equal(t, http.StatusForbidden,
		doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/candidates", "", tokenMember).Code)
	assert.Equal(t, http.StatusForbidden,
		doJSON(t, r, http.MethodGet, "/api/projects/"+pid+"/candidates", "", tokenOther).Code)
	_ = tokenOther
}

func TestProjectStats_ListAndDetail(t *testing.T) {
	r := setupServer(t)
	tokenOwner := registerUser(t, r, "powner", "负责人")
	pid := createProjectViaAPI(t, r, tokenOwner, "统计项目")

	// 3 个任务:todo(过期) / in_progress(未过期) / done(过期也不算 overdue)
	todoID := createTaskViaAPI(t, r, tokenOwner, pid, "过期待办", "")
	tid := createTaskViaAPI(t, r, tokenOwner, pid, "进行中", "")
	doneID := createTaskViaAPI(t, r, tokenOwner, pid, "已完成", "")
	db := connectDB(t)
	require.NoError(t, db.Exec("UPDATE tasks SET due_date = '2000-01-01' WHERE id = ?", todoID).Error)
	require.NoError(t, db.Exec("UPDATE tasks SET status='in_progress' WHERE id = ?", tid).Error)
	require.NoError(t, db.Exec("UPDATE tasks SET status='done', due_date='2000-01-01' WHERE id = ?", doneID).Error)
	closeDB(db)

	// 详情 stats
	w := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenOwner)
	require.Equal(t, http.StatusOK, w.Code)
	var detail struct {
		Project struct {
			Stats struct {
				Total      int `json:"total"`
				Todo       int `json:"todo"`
				InProgress int `json:"in_progress"`
				Done       int `json:"done"`
				Overdue    int `json:"overdue"`
			} `json:"stats"`
		} `json:"project"`
	}
	parseJSON(t, w, &detail)
	assert.Equal(t, 3, detail.Project.Stats.Total)
	assert.Equal(t, 1, detail.Project.Stats.Todo)
	assert.Equal(t, 1, detail.Project.Stats.InProgress)
	assert.Equal(t, 1, detail.Project.Stats.Done)
	assert.Equal(t, 1, detail.Project.Stats.Overdue, "仅过期未完成的 todo 计入 overdue")

	// 列表 stats
	wList := doJSON(t, r, http.MethodGet, "/api/projects", "", tokenOwner)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), `"stats"`)
	var list struct {
		Projects []struct {
			Stats struct {
				Total int `json:"total"`
			} `json:"stats"`
		} `json:"projects"`
	}
	parseJSON(t, wList, &list)
	require.Len(t, list.Projects, 1)
	assert.Equal(t, 3, list.Projects[0].Stats.Total)
}

var _ = json.Marshal
