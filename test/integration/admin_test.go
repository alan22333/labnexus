//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 提权:注册的普通用户直接 SQL 升为 admin(无 bootstrap admin,与 seed-admin 等价)。
func makeAdmin(t *testing.T, username string) {
	t.Helper()
	db := connectDB(t)
	defer closeDB(db)
	require.NoError(t, db.Exec("UPDATE users SET role = 'admin' WHERE username = ?", username).Error)
}

// 管理后台:越权矩阵(未登录 401;student 403)
func TestAdmin_ForbiddenMatrix(t *testing.T) {
	r := setupServer(t)
	tokenStu := registerUser(t, r, "adstu", "普通学生")
	_ = registerUser(t, r, "adsup", "导师")

	// 未登录 → 401
	assert.Equal(t, http.StatusUnauthorized, doJSON(t, r, http.MethodGet, "/api/admin/users", "", "").Code)

	// student 访问全部管理端点 → 403
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodGet, "/api/admin/invites", ""},
		{http.MethodPost, "/api/admin/invites", `{"code":"x"}`},
		{http.MethodGet, "/api/admin/task-types", ""},
		{http.MethodPost, "/api/admin/task-types", `{"name":"新"}`},
	} {
		w := doJSON(t, r, tc.method, tc.path, tc.body, tokenStu)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s → %s", tc.method, tc.path, w.Body.String())
	}
}

// 管理后台:成员列表 + 改角色(含防锁死)
func TestAdmin_UsersAndRole(t *testing.T) {
	r := setupServer(t)
	tokenAdmin := registerUser(t, r, "adadmin", "管理员")
	tokenStu := registerUser(t, r, "adstu2", "学生乙")
	makeAdmin(t, "adadmin")

	// 列表含全部且无密码字段
	w := doJSON(t, r, http.MethodGet, "/api/admin/users", "", tokenAdmin)
	require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "学生乙")
	assert.NotContains(t, w.Body.String(), "password_hash")

	// q 过滤
	wQ := doJSON(t, r, http.MethodGet, "/api/admin/users?q=stu2", "", tokenAdmin)
	require.Equal(t, http.StatusOK, wQ.Code)
	assert.Contains(t, wQ.Body.String(), "adstu2")
	assert.NotContains(t, wQ.Body.String(), "adadmin")

	// 改角色 stu → supervisor
	stuID := userIDOf(t, r, tokenStu)
	wRole := doJSON(t, r, http.MethodPatch, "/api/admin/users/"+stuID+"/role", `{"role":"supervisor"}`, tokenAdmin)
	require.Equal(t, http.StatusOK, wRole.Code, "%s", wRole.Body.String())
	assert.Contains(t, wRole.Body.String(), `"supervisor"`)

	// 非法角色 → 400;自降 → 400;不存在 → 404
	adminID := userIDOf(t, r, tokenAdmin)
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPatch, "/api/admin/users/"+stuID+"/role", `{"role":"root"}`, tokenAdmin).Code)
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPatch, "/api/admin/users/"+adminID+"/role", `{"role":"student"}`, tokenAdmin).Code)
	assert.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodPatch, "/api/admin/users/no-such/role", `{"role":"student"}`, tokenAdmin).Code)
}

// 管理后台:邀请码生成 → 注册 → 已用;撤销 → 无法注册
func TestAdmin_Invites_Lifecycle(t *testing.T) {
	r := setupServer(t)
	tokenAdmin := registerUser(t, r, "adinva", "管理员甲")
	makeAdmin(t, "adinva")

	// 生成自定义码
	w := doJSON(t, r, http.MethodPost, "/api/admin/invites", `{"code":"happy"}`, tokenAdmin)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		Invite struct {
			ID string `json:"id"`
		} `json:"invite"`
	}
	parseJSON(t, w, &created)

	// 新用户用该码注册成功
	wReg := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"HAPPY","username":"newbie","display_name":"新生","password":"password123"}`, "")
	require.Equal(t, http.StatusCreated, wReg.Code, "%s", wReg.Body.String())

	// 列表显示已用
	wList := doJSON(t, r, http.MethodGet, "/api/admin/invites", "", tokenAdmin)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), `"used_by"`)

	// 撤销一个未用码,再用它注册 → 失败
	wDel := doJSON(t, r, http.MethodDelete, "/api/admin/invites/"+created.Invite.ID, "", tokenAdmin)
	require.Equal(t, http.StatusNoContent, wDel.Code, "%s", wDel.Body.String())
	wReg2 := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"HAPPY","username":"newbie2","display_name":"新生2","password":"password123"}`, "")
	assert.Equal(t, http.StatusUnauthorized, wReg2.Code, "已撤销邀请码不应能注册: %s", wReg2.Body.String())
}

// 管理后台:任务类型 CRUD + 停用语义 + 删除回退
func TestAdmin_TaskTypes_Lifecycle(t *testing.T) {
	r := setupServer(t)
	tokenAdmin := registerUser(t, r, "adtadm", "管理员乙")
	tokenMember := registerUser(t, r, "adtmem", "成员乙")
	makeAdmin(t, "adtadm")

	// 新建 → 重名 409 → 空名 400
	w := doJSON(t, r, http.MethodPost, "/api/admin/task-types", `{"name":"组会","color":"#123456"}`, tokenAdmin)
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	var created struct {
		TaskType struct {
			ID string `json:"id"`
		} `json:"task_type"`
	}
	parseJSON(t, w, &created)
	typeID := created.TaskType.ID
	assert.Equal(t, http.StatusConflict, doJSON(t, r, http.MethodPost, "/api/admin/task-types", `{"name":"组会"}`, tokenAdmin).Code)
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPost, "/api/admin/task-types", `{"name":" "}`, tokenAdmin).Code)

	// 建项目 + 带该类型的任务
	pid := createProjectViaAPI(t, r, tokenAdmin, "类型管理项目")
	addMemberViaAPI(t, r, tokenAdmin, pid, tokenMember)
	taskBody := fmt.Sprintf(`{"title":"组会分享","type_id":%q}`, typeID)
	wTask := doJSON(t, r, http.MethodPost, "/api/projects/"+pid+"/tasks", taskBody, tokenAdmin)
	require.Equal(t, http.StatusCreated, wTask.Code, "%s", wTask.Body.String())
	assert.Contains(t, wTask.Body.String(), `"name":"组会"`)

	// 停用该类型
	wDeact := doJSON(t, r, http.MethodPatch, "/api/admin/task-types/"+typeID, `{"active":false}`, tokenAdmin)
	require.Equal(t, http.StatusOK, wDeact.Code, "%s", wDeact.Body.String())

	// 公开 GET /task-types 不再返回;但任务详情仍显示类型名(全量字典)
	wPub := doJSON(t, r, http.MethodGet, "/api/task-types", "", tokenAdmin)
	assert.NotContains(t, wPub.Body.String(), "组会")
	wDetail := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenMember)
	assert.Contains(t, wDetail.Body.String(), `"name":"组会"`, "停用类型旧任务应仍显示类型名")

	// 删除类型 → 任务 type_id 回退(未分类)
	wDel := doJSON(t, r, http.MethodDelete, "/api/admin/task-types/"+typeID, "", tokenAdmin)
	require.Equal(t, http.StatusNoContent, wDel.Code, "%s", wDel.Body.String())
	wDetail2 := doJSON(t, r, http.MethodGet, "/api/projects/"+pid, "", tokenAdmin)
	assert.NotContains(t, wDetail2.Body.String(), `"name":"组会"`)
	assert.NotContains(t, wDetail2.Body.String(), `"type_id"`)

	// 内置类型同样可删除(取一个内置类型删除,公开列表数量减少)
	before := doJSON(t, r, http.MethodGet, "/api/task-types", "", tokenAdmin)
	var types struct {
		TaskTypes []struct {
			ID string `json:"id"`
		} `json:"task_types"`
	}
	parseJSON(t, before, &types)
	require.NotEmpty(t, types.TaskTypes)
	builtinID := types.TaskTypes[0].ID
	require.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodDelete, "/api/admin/task-types/"+builtinID, "", tokenAdmin).Code)
	after := doJSON(t, r, http.MethodGet, "/api/task-types", "", tokenAdmin)
	var afterTypes struct {
		TaskTypes []struct {
			ID string `json:"id"`
		} `json:"task_types"`
	}
	parseJSON(t, after, &afterTypes)
	assert.Equal(t, len(types.TaskTypes)-1, len(afterTypes.TaskTypes), "删除内置类型后公开列表应减少")
}
