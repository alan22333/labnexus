package admin_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/admin"
	"labnexus/internal/project"
	"labnexus/internal/user"
)

// ---- 内存替身 ----

type memUsers struct {
	byID map[string]*user.User
}

func newMemUsers() *memUsers { return &memUsers{byID: map[string]*user.User{}} }

func (r *memUsers) seed(id, username, role string) *user.User {
	u := &user.User{ID: id, Username: username, DisplayName: username, Role: role}
	r.byID[id] = u
	return u
}

func (r *memUsers) Create(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }
func (r *memUsers) GetByID(_ context.Context, id string) (*user.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}
func (r *memUsers) GetByUsername(_ context.Context, _ string) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *memUsers) GetByIDs(_ context.Context, ids []string) ([]*user.User, error) {
	var out []*user.User
	for _, id := range ids {
		if u, ok := r.byID[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}
func (r *memUsers) Update(_ context.Context, u *user.User) error {
	r.byID[u.ID] = u
	return nil
}
func (r *memUsers) ListAll(_ context.Context, keyword string) ([]*user.User, error) {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	var out []*user.User
	for _, u := range r.byID {
		if kw == "" ||
			strings.Contains(strings.ToLower(u.Username), kw) ||
			strings.Contains(strings.ToLower(u.DisplayName), kw) {
			out = append(out, u)
		}
	}
	return out, nil
}

type memInvites struct {
	byID   map[string]*user.InviteCode
	byCode map[string]*user.InviteCode
}

func newMemInvites() *memInvites {
	return &memInvites{byID: map[string]*user.InviteCode{}, byCode: map[string]*user.InviteCode{}}
}

func (r *memInvites) Create(_ context.Context, c *user.InviteCode) error {
	r.byID[c.ID] = c
	r.byCode[c.Code] = c
	return nil
}
func (r *memInvites) GetByCode(_ context.Context, code string) (*user.InviteCode, error) {
	c, ok := r.byCode[code]
	if !ok {
		return nil, user.ErrNotFound
	}
	return c, nil
}
func (r *memInvites) MarkUsed(_ context.Context, id, userID string) error { return nil }
func (r *memInvites) List(_ context.Context) ([]*user.InviteCode, error) {
	var out []*user.InviteCode
	for _, c := range r.byID {
		out = append(out, c)
	}
	return out, nil
}
func (r *memInvites) Delete(_ context.Context, id string) error {
	c, ok := r.byID[id]
	if !ok {
		return user.ErrNotFound
	}
	delete(r.byID, id)
	delete(r.byCode, c.Code)
	return nil
}

// memTypes project.TaskTypeManager 替身
type memTypes struct {
	byID map[string]*project.TaskType
}

func newMemTypes() *memTypes { return &memTypes{byID: map[string]*project.TaskType{}} }

func (r *memTypes) ListAllTaskTypes(_ context.Context) ([]*project.TaskType, error) {
	var out []*project.TaskType
	for _, t := range r.byID {
		out = append(out, t)
	}
	return out, nil
}
func (r *memTypes) CreateTaskType(_ context.Context, name, color string) (*project.TaskType, error) {
	for _, t := range r.byID {
		if t.Name == name {
			return nil, project.ErrTaskTypeExists
		}
	}
	t := project.NewTaskType(name, color, 0, false)
	r.byID[t.ID] = t
	return t, nil
}
func (r *memTypes) UpdateTaskType(_ context.Context, id string, req project.UpdateTaskTypeRequest) (*project.TaskType, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, project.ErrTaskTypeNotFound
	}
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Active != nil {
		t.Active = *req.Active
	}
	return t, nil
}
func (r *memTypes) DeleteTaskType(_ context.Context, id string) error {
	if _, ok := r.byID[id]; !ok {
		return project.ErrTaskTypeNotFound
	}
	delete(r.byID, id)
	return nil
}

// ---- 夹具 ----

type fixture struct {
	svc     *admin.Service
	users   *memUsers
	invites *memInvites
	types   *memTypes
}

const (
	adminUser = "admin-1"
	boss      = "boss-1" // supervisor
	stu       = "stu-1"
	otherStu  = "stu-2"
)

func newFixture() *fixture {
	f := &fixture{users: newMemUsers(), invites: newMemInvites(), types: newMemTypes()}
	f.users.seed(adminUser, "admin", user.RoleAdmin)
	f.users.seed(boss, "boss", user.RoleSupervisor)
	f.users.seed(stu, "stu", user.RoleStudent)
	f.svc = admin.NewService(f.users, f.invites, f.types)
	return f
}

// ---- 权限 ----

func TestAdmin_NonAdminForbidden(t *testing.T) {
	f := newFixture()

	// supervisor / student 访问各管理方法 → ErrForbidden
	_, err := f.svc.ListUsers(context.Background(), boss, "")
	assert.ErrorIs(t, err, admin.ErrForbidden)
	_, err = f.svc.ListUsers(context.Background(), stu, "")
	assert.ErrorIs(t, err, admin.ErrForbidden)

	_, err = f.svc.ChangeRole(context.Background(), stu, otherStu, user.RoleSupervisor)
	assert.ErrorIs(t, err, admin.ErrForbidden)

	_, err = f.svc.ListInvites(context.Background(), stu)
	assert.ErrorIs(t, err, admin.ErrForbidden)
	_, err = f.svc.CreateInvite(context.Background(), stu, "")
	assert.ErrorIs(t, err, admin.ErrForbidden)
	assert.ErrorIs(t, f.svc.DeleteInvite(context.Background(), stu, "x"), admin.ErrForbidden)

	_, err = f.svc.ListAllTaskTypes(context.Background(), stu)
	assert.ErrorIs(t, err, admin.ErrForbidden)
	_, err = f.svc.CreateTaskType(context.Background(), stu, "新类型", "")
	assert.ErrorIs(t, err, admin.ErrForbidden)
	_, err = f.svc.UpdateTaskType(context.Background(), stu, "x", project.UpdateTaskTypeRequest{})
	assert.ErrorIs(t, err, admin.ErrForbidden)
	assert.ErrorIs(t, f.svc.DeleteTaskType(context.Background(), stu, "x"), admin.ErrForbidden)
}

// ---- 成员 ----

func TestAdmin_ListUsers(t *testing.T) {
	f := newFixture()
	list, err := f.svc.ListUsers(context.Background(), adminUser, "")
	require.NoError(t, err)
	require.Len(t, list, 3)

	// 关键词过滤
	got, err := f.svc.ListUsers(context.Background(), adminUser, "boss")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, boss, got[0].ID)
}

func TestAdmin_ChangeRole(t *testing.T) {
	f := newFixture()

	// 学生升导师
	u, err := f.svc.ChangeRole(context.Background(), adminUser, stu, user.RoleSupervisor)
	require.NoError(t, err)
	assert.Equal(t, user.RoleSupervisor, u.Role)

	// 非法角色 → ErrInvalidRole
	_, err = f.svc.ChangeRole(context.Background(), adminUser, stu, "root")
	assert.ErrorIs(t, err, admin.ErrInvalidRole)

	// 目标不存在 → ErrUserNotFound
	_, err = f.svc.ChangeRole(context.Background(), adminUser, "no-such", user.RoleStudent)
	assert.ErrorIs(t, err, admin.ErrUserNotFound)

	// 防锁死:降级自己 → ErrCannotSelfDemote
	_, err = f.svc.ChangeRole(context.Background(), adminUser, adminUser, user.RoleStudent)
	assert.ErrorIs(t, err, admin.ErrCannotSelfDemote)
}

func TestAdmin_ChangeRole_LastAdmin(t *testing.T) {
	f := newFixture()
	// 只有 adminUser 一个 admin(其他是 supervisor/student)

	// 把唯一 admin 降级 → 拒绝
	_, err := f.svc.ChangeRole(context.Background(), adminUser, adminUser, user.RoleStudent)
	assert.ErrorIs(t, err, admin.ErrCannotSelfDemote)

	// 升一个 second admin 后,adminUser 可降级自己? 仍不允许(自降),但可降另一 admin?
	second := f.users.seed("admin-2", "admin2", user.RoleStudent)
	_, err = f.svc.ChangeRole(context.Background(), adminUser, second.ID, user.RoleAdmin)
	require.NoError(t, err)

	// 现在两个 admin:可把 admin-2 降为 supervisor
	_, err = f.svc.ChangeRole(context.Background(), adminUser, second.ID, user.RoleSupervisor)
	require.NoError(t, err)

	// 只剩 adminUser 一个 admin:再升 admin-2 又降(此时 admin-2 是唯一可操作的第二个 admin)…直接验证唯一性保护
	_, err = f.svc.ChangeRole(context.Background(), adminUser, second.ID, user.RoleAdmin)
	require.NoError(t, err)
	// 降 admin-2(合法,adminUser 仍是 admin)→ 只剩 adminUser
	_, err = f.svc.ChangeRole(context.Background(), adminUser, second.ID, user.RoleStudent)
	require.NoError(t, err)

	// 此时试图把 adminUser 之外的唯一 admin…已经不存在第二个 admin;
	// adminUser 自己不能降自己(防锁死1)。
}

// ---- 邀请码 ----

func TestAdmin_CreateInvite_AutoAndCustom(t *testing.T) {
	f := newFixture()

	// 自动生成
	inv, err := f.svc.CreateInvite(context.Background(), adminUser, "")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(inv.Code, "LN-"), "自动码应以 LN- 开头: %s", inv.Code)
	assert.Equal(t, adminUser, inv.CreatedBy)

	// 自定义码
	custom, err := f.svc.CreateInvite(context.Background(), adminUser, "group-2026")
	require.NoError(t, err)
	assert.Equal(t, "GROUP-2026", custom.Code)

	// 重码 → ErrInviteExists
	_, err = f.svc.CreateInvite(context.Background(), adminUser, "group-2026")
	assert.ErrorIs(t, err, admin.ErrInviteExists)
}

func TestAdmin_InviteListAndDelete(t *testing.T) {
	f := newFixture()
	inv, _ := f.svc.CreateInvite(context.Background(), adminUser, "")

	list, err := f.svc.ListInvites(context.Background(), adminUser)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, f.svc.DeleteInvite(context.Background(), adminUser, inv.ID))
	assert.ErrorIs(t, f.svc.DeleteInvite(context.Background(), adminUser, inv.ID), user.ErrNotFound)
}

// ---- 任务类型 ----

func TestAdmin_TaskTypeLifecycle(t *testing.T) {
	f := newFixture()

	// 创建
	created, err := f.svc.CreateTaskType(context.Background(), adminUser, "调研", "#123456")
	require.NoError(t, err)
	assert.Equal(t, "调研", created.Name)

	// 编辑(停用)
	updated, err := f.svc.UpdateTaskType(context.Background(), adminUser, created.ID, project.UpdateTaskTypeRequest{
		Active: boolPtr(false),
	})
	require.NoError(t, err)
	assert.False(t, updated.Active)

	// 列表
	list, err := f.svc.ListAllTaskTypes(context.Background(), adminUser)
	require.NoError(t, err)
	require.Len(t, list, 1)

	// 删除
	require.NoError(t, f.svc.DeleteTaskType(context.Background(), adminUser, created.ID))
	err = f.svc.DeleteTaskType(context.Background(), adminUser, created.ID)
	assert.ErrorIs(t, err, project.ErrTaskTypeNotFound)
}

func boolPtr(b bool) *bool { return &b }
