package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/authz"
	"labnexus/internal/user"
)

type memUserRepo struct {
	byID map[string]*user.User
}

func (r *memUserRepo) seed(id, role string) {
	r.byID[id] = &user.User{ID: id, Username: id, DisplayName: id, Role: role}
}

func (r *memUserRepo) Create(context.Context, *user.User) error { return nil }
func (r *memUserRepo) GetByID(_ context.Context, id string) (*user.User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}
func (r *memUserRepo) GetByUsername(context.Context, string) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *memUserRepo) GetByIDs(context.Context, []string) ([]*user.User, error) {
	return nil, nil
}
func (r *memUserRepo) Update(context.Context, *user.User) error { return nil }
func (r *memUserRepo) ListAll(context.Context, string) ([]*user.User, error) {
	return nil, nil
}

func newAuthorizer(t *testing.T, bypass bool) (*authz.Service, *memUserRepo) {
	t.Helper()
	users := &memUserRepo{byID: map[string]*user.User{}}
	users.seed("admin", user.RoleAdmin)
	users.seed("supervisor", user.RoleSupervisor)
	users.seed("student", user.RoleStudent)
	users.seed("other", user.RoleStudent)
	return authz.New(users, authz.Options{TestBypass: bypass}), users
}

func TestCan_GlobalRoles(t *testing.T) {
	a, _ := newAuthorizer(t, false)
	ctx := context.Background()

	tests := []struct {
		name    string
		actorID string
		action  string
		target  authz.Target
		want    bool
	}{
		{"admin can manage admin console", "admin", authz.ActionManage, authz.Target{Resource: authz.ResourceAdminConsole}, true},
		{"supervisor cannot manage admin console", "supervisor", authz.ActionManage, authz.Target{Resource: authz.ResourceAdminConsole}, false},
		{"student cannot manage admin console", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceAdminConsole}, false},
		{"admin can manage finance", "admin", authz.ActionManage, authz.Target{Resource: authz.ResourceFinance}, true},
		{"supervisor can manage finance", "supervisor", authz.ActionManage, authz.Target{Resource: authz.ResourceFinance}, true},
		{"student cannot manage finance", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceFinance}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Can(ctx, tt.actorID, tt.action, tt.target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCan_ResourceOwnership(t *testing.T) {
	a, _ := newAuthorizer(t, false)
	ctx := context.Background()
	target := authz.Target{Resource: authz.ResourceResource, OwnerID: "student"}

	got, err := a.Can(ctx, "student", authz.ActionManage, target)
	require.NoError(t, err)
	assert.True(t, got, "uploader can manage own resource")

	got, err = a.Can(ctx, "other", authz.ActionManage, target)
	require.NoError(t, err)
	assert.False(t, got, "other student cannot manage resource")

	got, err = a.Can(ctx, "admin", authz.ActionManage, target)
	require.NoError(t, err)
	assert.True(t, got, "admin can manage resource")
}

func TestCan_DocumentVisibilityAndAuthorship(t *testing.T) {
	a, _ := newAuthorizer(t, false)
	ctx := context.Background()

	publicDoc := authz.Target{Resource: authz.ResourceDocument, OwnerID: "student", Visibility: "public"}
	privateDoc := authz.Target{Resource: authz.ResourceDocument, OwnerID: "student", Visibility: "private"}

	tests := []struct {
		name    string
		actorID string
		action  string
		target  authz.Target
		want    bool
	}{
		{"any authenticated user reads public document", "other", authz.ActionRead, publicDoc, true},
		{"author reads private document", "student", authz.ActionRead, privateDoc, true},
		{"other user cannot read private document", "other", authz.ActionRead, privateDoc, false},
		{"author manages own document", "student", authz.ActionManage, privateDoc, true},
		{"other user cannot manage document", "other", authz.ActionManage, privateDoc, false},
		{"admin does not bypass document authorship", "admin", authz.ActionManage, privateDoc, false},
		{"author manages own comment", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceComment, OwnerID: "student"}, true},
		{"other user cannot manage comment", "other", authz.ActionManage, authz.Target{Resource: authz.ResourceComment, OwnerID: "student"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Can(ctx, tt.actorID, tt.action, tt.target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCan_ProjectAndTaskRoles(t *testing.T) {
	a, _ := newAuthorizer(t, false)
	ctx := context.Background()

	tests := []struct {
		name    string
		actorID string
		action  string
		target  authz.Target
		want    bool
	}{
		{"owner reads project", "student", authz.ActionRead, authz.Target{Resource: authz.ResourceProject, ProjectRole: "owner"}, true},
		{"member reads project", "student", authz.ActionRead, authz.Target{Resource: authz.ResourceProject, ProjectRole: "member"}, true},
		{"non-member cannot read project", "student", authz.ActionRead, authz.Target{Resource: authz.ResourceProject}, false},
		{"owner manages project", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceProject, ProjectRole: "owner"}, true},
		{"member cannot manage project", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceProject, ProjectRole: "member"}, false},
		{"owner manages task", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceTask, ProjectRole: "owner"}, true},
		{"assignee manages task", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceTask, AssigneeID: "student"}, true},
		{"member cannot manage unassigned task", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceTask, ProjectRole: "member"}, false},
		{"non-member cannot manage task", "student", authz.ActionManage, authz.Target{Resource: authz.ResourceTask}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Can(ctx, tt.actorID, tt.action, tt.target)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCan_DefaultsDeny(t *testing.T) {
	a, _ := newAuthorizer(t, false)
	got, err := a.Can(context.Background(), "student", "unknown", authz.Target{Resource: "unknown"})
	require.NoError(t, err)
	assert.False(t, got)
}

func TestCan_TestBypass(t *testing.T) {
	a, _ := newAuthorizer(t, true)
	got, err := a.Can(context.Background(), "not-a-user", "anything", authz.Target{Resource: "anything"})
	require.NoError(t, err)
	assert.True(t, got)
}
