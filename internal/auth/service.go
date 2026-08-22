// Package auth 账号域:F1 注册/登录/刷新/登出/个人资料。
// 分层:handler(HTTP) → service(业务)→ repository(数据)+ cache(Redis)。
// 依据规格:docs/specs/f1-auth.md;契约:docs/api-contract.md §F1。
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"labnexus/internal/cache"
	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/space"
	"labnexus/internal/token"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP 状态码,规范 §4)
var (
	ErrInvalidInvite       = errors.New("invalid invite code")
	ErrWeakPassword        = errors.New("password too short")
	ErrUsernameTaken       = errors.New("username already taken")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrUserNotFound        = errors.New("user not found")
	ErrOldPasswordMismatch = errors.New("old password mismatch")
)

// MinPasswordLen 密码最小长度(f1-auth.md §4)
const MinPasswordLen = 8

// Service 账号业务逻辑
type Service struct {
	users    user.Repository
	invites  user.InviteRepository
	spaces   space.Repository
	store    cache.Store
	cfg      *config.Config
	txRunner database.TxRunner
}

// NewService 构造函数(依赖注入,规范 §3);默认无事务执行(测试场景),
// main 中通过 WithTxRunner 注入 GORM 事务。
func NewService(
	users user.Repository,
	invites user.InviteRepository,
	spaces space.Repository,
	store cache.Store,
	cfg *config.Config,
) *Service {
	return &Service{
		users:    users,
		invites:  invites,
		spaces:   spaces,
		store:    store,
		cfg:      cfg,
		txRunner: database.NoopTxRunner(),
	}
}

// WithTxRunner 注入事务运行器(写操作事务化,规范 §5)。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	InviteCode  string `json:"invite_code"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// UpdateMeRequest 修改个人资料请求(指针字段 = 可选)
type UpdateMeRequest struct {
	DisplayName *string `json:"display_name"`
	Password    *string `json:"password"`
	OldPassword *string `json:"old_password"`
}

// LoginResult 登录/注册/刷新成功响应
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	User         *user.User
}

// Register 注册:校验邀请码 → 创建用户(bcrypt)→ 标记邀请码已用 → 创建个人空间(事务内)→ 签发 token。
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*LoginResult, error) {
	// 基础校验
	if len(req.Password) < MinPasswordLen {
		return nil, ErrWeakPassword
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.DisplayName) == "" {
		return nil, errors.New("username and display_name are required")
	}

	// 邀请码校验:存在、未过期、未使用
	inv, err := s.invites.GetByCode(ctx, req.InviteCode)
	if err != nil || inv.IsExpired() || inv.IsUsed() {
		return nil, ErrInvalidInvite
	}

	// 用户名唯一
	if _, err := s.users.GetByUsername(ctx, req.Username); err == nil {
		return nil, ErrUsernameTaken
	} else if !errors.Is(err, user.ErrNotFound) {
		return nil, err
	}

	// 密码 bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := user.NewUser(req.Username, req.DisplayName, string(hash))

	// 事务:建用户 + 标记邀请码已用 + 建个人空间(任一步失败整体回滚)
	err = s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.users.Create(tctx, u); err != nil {
			return err
		}
		if err := s.invites.MarkUsed(tctx, inv.ID, u.ID); err != nil {
			return err
		}
		return s.spaces.Create(tctx, space.NewSpace(u.ID))
	})
	if err != nil {
		return nil, err
	}

	return s.issueTokens(ctx, u)
}

// Login 登录:校验凭据 → 签发 access + refresh。
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResult, error) {
	u, err := s.users.GetByUsername(ctx, req.Username)
	if err != nil {
		// 不区分"用户不存在"与"密码错误",防枚举(规格 §5)
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return s.issueTokens(ctx, u)
}

// Refresh 刷新:验证 refresh(Redis 存在)→ 轮换(删旧发新)→ 签发新 access。
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*LoginResult, error) {
	if refreshToken == "" {
		return nil, ErrInvalidRefreshToken
	}
	userID, err := s.store.Get(ctx, refreshKey(refreshToken))
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	// 轮换:旧 refresh 立即失效
	if err := s.store.Del(ctx, refreshKey(refreshToken)); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, u)
}

// Logout 登出:撤销 refresh(Redis 删除)。
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return ErrInvalidRefreshToken
	}
	if _, err := s.store.Get(ctx, refreshKey(refreshToken)); err != nil {
		return ErrInvalidRefreshToken
	}
	return s.store.Del(ctx, refreshKey(refreshToken))
}

// Me 获取当前用户信息。
func (s *Service) Me(ctx context.Context, userID string) (*user.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// UpdateMe 修改个人资料(display_name / 密码)。
func (s *Service) UpdateMe(ctx context.Context, userID string, req UpdateMeRequest) (*user.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}

	if req.DisplayName != nil && strings.TrimSpace(*req.DisplayName) != "" {
		u.DisplayName = *req.DisplayName
	}

	if req.Password != nil {
		if req.OldPassword == nil ||
			bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(*req.OldPassword)) != nil {
			return nil, ErrOldPasswordMismatch
		}
		if len(*req.Password) < MinPasswordLen {
			return nil, ErrWeakPassword
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = string(hash)
	}

	u.UpdatedAt = time.Now()
	if err := s.users.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// issueTokens 签发 access token(JWT)+ refresh token(不透明串存 Redis)。
func (s *Service) issueTokens(ctx context.Context, u *user.User) (*LoginResult, error) {
	access, err := token.GenerateAccessToken(s.cfg.JWTSecret, u.ID, u.Role, s.cfg.AccessTokenTTL)
	if err != nil {
		return nil, err
	}
	refresh := newRefreshToken()
	if err := s.store.Set(ctx, refreshKey(refresh), u.ID, s.cfg.RefreshTokenTTL); err != nil {
		return nil, err
	}
	return &LoginResult{AccessToken: access, RefreshToken: refresh, User: u}, nil
}

func refreshKey(refreshToken string) string {
	return "refresh:" + refreshToken
}

// newRefreshToken 生成不透明 refresh token(两段 UUID v4,128+ 位随机性)。
func newRefreshToken() string {
	return uuid.NewString() + uuid.NewString()
}
