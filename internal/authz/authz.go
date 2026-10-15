// Package authz 统一 RBAC + ABAC 权限决策。
// 策略在进程内由 Casbin 加载;领域服务只提交 actor、动作与目标属性。
package authz

import (
	"context"
	"fmt"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"

	"labnexus/internal/user"
)

// 资源类型与动作常量。各领域服务使用这些常量,避免权限字符串散落。
const (
	ResourceAdminConsole = "admin_console"
	ResourceFinance      = "finance"
	ResourceProject      = "project"
	ResourceTask         = "task"
	ResourceResource     = "resource"
	ResourceDocument     = "document"
	ResourceComment      = "comment"

	ActionRead   = "read"
	ActionManage = "manage"
)

// Target 描述权限对象属性。空值默认不匹配任何限制性策略。
type Target struct {
	Resource    string
	OwnerID     string // 资源所有者/上传者/文档或评论作者
	ProjectRole string // 当前用户在该项目中的角色:owner/member
	AssigneeID  string // 任务负责人
	Visibility  string // 文档可见性:private/public
}

// Authorizer 统一权限接口。
type Authorizer interface {
	Can(ctx context.Context, actorID, action string, target Target) (bool, error)
}

// Options 授权器配置。
type Options struct {
	// TestBypass 仅用于自动化测试;必须由显式配置开启,生产环境由 Config.Validate 拒绝。
	TestBypass bool
}

// Service 基于 Casbin 的授权器。
type Service struct {
	users      user.Repository
	testBypass bool
	once       sync.Once
	enforcer   *casbin.Enforcer
	initErr    error
}

// New 创建授权器。Casbin 初始化延迟到第一次 Can,避免构造函数产生隐式启动副作用。
func New(users user.Repository, opts Options) *Service {
	return &Service{users: users, testBypass: opts.TestBypass}
}

// Can 判断 actor 是否可对 target 执行 action。
func (s *Service) Can(ctx context.Context, actorID, action string, target Target) (bool, error) {
	if s.testBypass {
		return true, nil
	}
	if actorID == "" {
		return false, nil
	}
	u, err := s.users.GetByID(ctx, actorID)
	if err != nil {
		return false, err
	}
	if err := s.ensureEnforcer(); err != nil {
		return false, err
	}

	assignee := "false"
	if target.AssigneeID != "" && target.AssigneeID == actorID {
		assignee = "true"
	}
	ok, err := s.enforcer.Enforce(
		actorID,
		u.Role,
		target.Resource,
		action,
		target.OwnerID,
		target.ProjectRole,
		assignee,
		target.Visibility,
	)
	if err != nil {
		return false, fmt.Errorf("casbin enforce: %w", err)
	}
	return ok, nil
}

func (s *Service) ensureEnforcer() error {
	s.once.Do(func() {
		m, err := model.NewModelFromString(modelText)
		if err != nil {
			s.initErr = fmt.Errorf("casbin model: %w", err)
			return
		}
		e, err := casbin.NewEnforcer(m)
		if err != nil {
			s.initErr = fmt.Errorf("casbin enforcer: %w", err)
			return
		}
		if _, err := e.AddPolicies(policyRules); err != nil {
			s.initErr = fmt.Errorf("casbin policies: %w", err)
			return
		}
		if _, err := e.AddGroupingPolicies(groupingRules); err != nil {
			s.initErr = fmt.Errorf("casbin grouping policies: %w", err)
			return
		}
		s.enforcer = e
	})
	return s.initErr
}

const modelText = `
[request_definition]
r = sub, role, resource, action, owner, project_role, assignee, visibility

[policy_definition]
p = role, resource, action, own_required, project_role, assignee_required, visibility

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.role, p.role) && (p.resource == "*" || p.resource == r.resource) && (p.action == "*" || p.action == r.action) && (p.own_required == "false" || r.sub == r.owner) && (p.project_role == "*" || p.project_role == r.project_role) && (p.assignee_required == "false" || r.assignee == "true") && (p.visibility == "*" || p.visibility == r.visibility)
`

var groupingRules = [][]string{
	// 角色继承
	{"admin", "authenticated"},
	{"supervisor", "authenticated"},
	{"student", "authenticated"},
}

var policyRules = [][]string{
	// 管理后台与经费
	{"admin", ResourceAdminConsole, ActionManage, "false", "*", "false", "*"},
	{"admin", ResourceFinance, ActionManage, "false", "*", "false", "*"},
	{"supervisor", ResourceFinance, ActionManage, "false", "*", "false", "*"},

	// 资源:上传者或 admin
	{"authenticated", ResourceResource, ActionManage, "true", "*", "false", "*"},
	{"admin", ResourceResource, ActionManage, "false", "*", "false", "*"},

	// 项目:owner 管理,owner/member 读取
	{"authenticated", ResourceProject, ActionRead, "false", "owner", "false", "*"},
	{"authenticated", ResourceProject, ActionRead, "false", "member", "false", "*"},
	{"authenticated", ResourceProject, ActionManage, "false", "owner", "false", "*"},

	// 任务:项目 owner 或任务 assignee
	{"authenticated", ResourceTask, ActionManage, "false", "owner", "false", "*"},
	{"authenticated", ResourceTask, ActionManage, "false", "*", "true", "*"},

	// 文档/评论:作者维护;公开文档所有登录成员可读
	{"authenticated", ResourceDocument, ActionManage, "true", "*", "false", "*"},
	{"authenticated", ResourceDocument, ActionRead, "true", "*", "false", "*"},
	{"authenticated", ResourceDocument, ActionRead, "false", "*", "false", "public"},
	{"authenticated", ResourceComment, ActionManage, "true", "*", "false", "*"},
}
