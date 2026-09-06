// Package admin 管理后台域(Admin Console):成员/邀请码/任务类型管理。
// 权限:全部能力仅 admin 可用(specs/admin-console.md §2)。
// 分层:handler(HTTP)→ service(业务/角色校验)→ 注入的域仓库与域 service。
package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"labnexus/internal/project"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrForbidden        = errors.New("admin permission required")
	ErrInvalidRole      = errors.New("invalid role, expected admin/supervisor/student")
	ErrUserNotFound     = errors.New("user not found")
	ErrCannotSelfDemote = errors.New("cannot demote yourself")
	ErrLastAdmin        = errors.New("cannot demote the last admin")
	ErrInviteNotFound   = errors.New("invite not found")
	ErrInviteExists     = errors.New("invite code already exists")
	ErrInviteEmpty      = errors.New("invite code is empty")
)

// validRoles admin 可设置的合法角色
var validRoles = map[string]bool{
	user.RoleAdmin:      true,
	user.RoleSupervisor: true,
	user.RoleStudent:    true,
}

// TaskTypeManager 任务类型管理能力(由 project.Service 实现;接口便于测试替身)。
type TaskTypeManager interface {
	ListAllTaskTypes(ctx context.Context) ([]*project.TaskType, error)
	CreateTaskType(ctx context.Context, name, color string) (*project.TaskType, error)
	UpdateTaskType(ctx context.Context, id string, req project.UpdateTaskTypeRequest) (*project.TaskType, error)
	DeleteTaskType(ctx context.Context, id string) error
}

// Service 管理后台业务逻辑(依赖注入)。
type Service struct {
	users     user.Repository
	invites   user.InviteRepository
	taskTypes TaskTypeManager
}

// NewService 构造函数。
func NewService(users user.Repository, invites user.InviteRepository, taskTypes TaskTypeManager) *Service {
	return &Service{users: users, invites: invites, taskTypes: taskTypes}
}

// ---- 角色校验 ----

// requireAdmin 校验 actor 为 admin。
func (s *Service) requireAdmin(ctx context.Context, actorID string) error {
	u, err := s.users.GetByID(ctx, actorID)
	if err != nil {
		return ErrForbidden
	}
	if u.Role != user.RoleAdmin {
		return ErrForbidden
	}
	return nil
}

// adminCount 统计 admin 数量(组小,ListAll 全量即可)。
func (s *Service) adminCount(ctx context.Context) (int, error) {
	list, err := s.users.ListAll(ctx, "")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range list {
		if u.Role == user.RoleAdmin {
			n++
		}
	}
	return n, nil
}

// ---- 成员管理 ----

// ListUsers 成员列表(可空 keyword 过滤)。
func (s *Service) ListUsers(ctx context.Context, actorID, keyword string) ([]*user.User, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.users.ListAll(ctx, keyword)
}

// ChangeRole 调整成员角色(防锁死:不能降自己、不能降最后一个 admin)。
func (s *Service) ChangeRole(ctx context.Context, actorID, targetID, role string) (*user.User, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if !validRoles[role] {
		return nil, ErrInvalidRole
	}
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	if target.Role == role {
		return target, nil // 幂等:同角色直接返回
	}
	// 防锁死 1:不能降级自己
	if targetID == actorID && role != user.RoleAdmin {
		return nil, ErrCannotSelfDemote
	}
	// 防锁死 2:不能把最后一个 admin 降级
	if target.Role == user.RoleAdmin {
		n, err := s.adminCount(ctx)
		if err != nil {
			return nil, err
		}
		if n <= 1 {
			return nil, ErrLastAdmin
		}
	}
	target.Role = role
	if err := s.users.Update(ctx, target); err != nil {
		return nil, err
	}
	return target, nil
}

// ---- 邀请码管理 ----

// ListInvites 邀请码列表。
func (s *Service) ListInvites(ctx context.Context, actorID string) ([]*user.InviteCode, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.invites.List(ctx)
}

// CreateInvite 生成邀请码;code 为空 → 自动生成随机码。
func (s *Service) CreateInvite(ctx context.Context, actorID, code string) (*user.InviteCode, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		code = newInviteCode()
	}
	if _, err := s.invites.GetByCode(ctx, code); err == nil {
		return nil, ErrInviteExists
	}
	inv := user.NewInviteCode(actorID, code, nil)
	if err := s.invites.Create(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// DeleteInvite 撤销邀请码(无论是否已用;防再次注册)。
func (s *Service) DeleteInvite(ctx context.Context, actorID, id string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return s.invites.Delete(ctx, id)
}

// ---- 任务类型管理(委托 project 域) ----

// ListAllTaskTypes 全部任务类型(含停用)。
func (s *Service) ListAllTaskTypes(ctx context.Context, actorID string) ([]*project.TaskType, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.taskTypes.ListAllTaskTypes(ctx)
}

// CreateTaskType 新建任务类型。
func (s *Service) CreateTaskType(ctx context.Context, actorID, name, color string) (*project.TaskType, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, project.ErrTaskTypeNameEmpty
	}
	return s.taskTypes.CreateTaskType(ctx, name, color)
}

// UpdateTaskType 编辑任务类型。
func (s *Service) UpdateTaskType(ctx context.Context, actorID, id string, req project.UpdateTaskTypeRequest) (*project.TaskType, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.taskTypes.UpdateTaskType(ctx, id, req)
}

// DeleteTaskType 删除任务类型(引用任务回退未分类)。
func (s *Service) DeleteTaskType(ctx context.Context, actorID, id string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return s.taskTypes.DeleteTaskType(ctx, id)
}

// newInviteCode 生成随机邀请码:前缀 LN- + 8 位随机大写字母数字(便于口述/复制)。
func newInviteCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去易混淆字符 O/0/I/1
	const n = 8
	b := make([]byte, n)
	// crypto/rand 一次性取足随机字节
	randBytes := make([]byte, n)
	if _, err := rand.Read(randBytes); err != nil {
		// 极端情况回退时间戳(仅测试/环境异常兜底)
		return "LN-" + fmt.Sprintf("%d", time.Now().UnixNano())[:8]
	}
	for i := range b {
		b[i] = chars[int(randBytes[i])%len(chars)]
	}
	return "LN-" + string(b)
}
