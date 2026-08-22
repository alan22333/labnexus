package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"labnexus/internal/auth"
	"labnexus/internal/cache"
	"labnexus/internal/config"
	"labnexus/internal/space"
	"labnexus/internal/token"
	"labnexus/internal/user"
)

// ---- 内存替身(规范 §2.1:单元测试不依赖外部服务) ----

type memUserRepo struct {
	byID   map[string]*user.User
	byName map[string]*user.User
}

func newMemUserRepo() *memUserRepo {
	return &memUserRepo{byID: map[string]*user.User{}, byName: map[string]*user.User{}}
}

func (r *memUserRepo) Create(_ context.Context, u *user.User) error {
	if _, dup := r.byName[u.Username]; dup {
		return errors.New("unique violation: username")
	}
	r.byID[u.ID] = u
	r.byName[u.Username] = u
	return nil
}

func (r *memUserRepo) GetByID(_ context.Context, id string) (*user.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *memUserRepo) GetByUsername(_ context.Context, username string) (*user.User, error) {
	u, ok := r.byName[username]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *memUserRepo) Update(_ context.Context, u *user.User) error {
	if _, ok := r.byID[u.ID]; !ok {
		return user.ErrNotFound
	}
	r.byID[u.ID] = u
	r.byName[u.Username] = u
	return nil
}

type memInviteRepo struct {
	byCode map[string]*user.InviteCode
}

func newMemInviteRepo() *memInviteRepo {
	return &memInviteRepo{byCode: map[string]*user.InviteCode{}}
}

func (r *memInviteRepo) Create(_ context.Context, c *user.InviteCode) error {
	r.byCode[c.Code] = c
	return nil
}

func (r *memInviteRepo) GetByCode(_ context.Context, code string) (*user.InviteCode, error) {
	c, ok := r.byCode[code]
	if !ok {
		return nil, user.ErrNotFound
	}
	return c, nil
}

func (r *memInviteRepo) MarkUsed(_ context.Context, id, userID string) error {
	for _, c := range r.byCode {
		if c.ID == id {
			now := time.Now()
			c.UsedBy = &userID
			c.UsedAt = &now
			return nil
		}
	}
	return errors.New("not found")
}

type memSpaceRepo struct {
	byUser map[string]*space.Space
}

func newMemSpaceRepo() *memSpaceRepo {
	return &memSpaceRepo{byUser: map[string]*space.Space{}}
}

func (r *memSpaceRepo) Create(_ context.Context, s *space.Space) error {
	r.byUser[s.UserID] = s
	return nil
}

func (r *memSpaceRepo) GetByUserID(_ context.Context, userID string) (*space.Space, error) {
	s, ok := r.byUser[userID]
	if !ok {
		return nil, space.ErrNotFound
	}
	return s, nil
}

// memStore 内存键值存储,模拟 Redis(含过期)
type memStore struct {
	data map[string]memEntry
}

type memEntry struct {
	value string
	exp   time.Time
}

func newMemStore() *memStore {
	return &memStore{data: map[string]memEntry{}}
}

func (m *memStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.data[key] = memEntry{value: value, exp: time.Now().Add(ttl)}
	return nil
}

func (m *memStore) Get(_ context.Context, key string) (string, error) {
	e, ok := m.data[key]
	if !ok || time.Now().After(e.exp) {
		return "", cache.ErrNotFound
	}
	return e.value, nil
}

func (m *memStore) Del(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

// ---- 测试夹具 ----

func newTestService(t *testing.T) (*auth.Service, *memUserRepo, *memInviteRepo, *memSpaceRepo, *memStore) {
	t.Helper()
	cfg := &config.Config{
		JWTSecret:       "test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	users := newMemUserRepo()
	invites := newMemInviteRepo()
	spaces := newMemSpaceRepo()
	store := newMemStore()
	svc := auth.NewService(users, invites, spaces, store, cfg)
	return svc, users, invites, spaces, store
}

const (
	validInvite = "INVITE-123"
	password    = "password123"
)

func seedInvite(t *testing.T, invites *memInviteRepo, code string, expiresAt *time.Time) {
	t.Helper()
	now := time.Now()
	require.NoError(t, invites.Create(context.Background(), &user.InviteCode{
		ID: "inv-" + code, Code: code, CreatedBy: "admin", ExpiresAt: expiresAt, CreatedAt: now,
	}))
}

func seedUser(t *testing.T, users *memUserRepo, username, pwd string) *user.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.MinCost)
	require.NoError(t, err)
	u := user.NewUser(username, "测试用户", string(hash))
	require.NoError(t, users.Create(context.Background(), u))
	return u
}

func validRegisterReq() auth.RegisterRequest {
	return auth.RegisterRequest{InviteCode: validInvite, Username: "alice", DisplayName: "Alice", Password: password}
}

// ---- 注册 ----

func TestRegister_Success(t *testing.T) {
	svc, users, invites, spaces, _ := newTestService(t)
	seedInvite(t, invites, validInvite, nil)

	res, err := svc.Register(context.Background(), validRegisterReq())
	require.NoError(t, err)
	require.NotNil(t, res)

	// 用户创建:role=student,密码为 bcrypt
	created, err := users.GetByUsername(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, user.RoleStudent, created.Role)
	assert.NotEqual(t, password, created.PasswordHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte(password)))

	// 邀请码标记已用
	inv, _ := invites.GetByCode(context.Background(), validInvite)
	assert.True(t, inv.IsUsed())

	// 个人空间自动创建
	sp, err := spaces.GetByUserID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "我的空间", sp.Name)

	// access token 可解析且载荷正确
	claims, err := token.ParseAccessToken("test-secret", res.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, created.ID, claims.UserID)
	assert.Equal(t, user.RoleStudent, claims.Role)
	assert.NotEmpty(t, res.RefreshToken)
}

func TestRegister_InvalidInvite(t *testing.T) {
	svc, _, invites, _, _ := newTestService(t)
	seedInvite(t, invites, "OTHER", nil)

	_, err := svc.Register(context.Background(), validRegisterReq())
	assert.ErrorIs(t, err, auth.ErrInvalidInvite)
}

func TestRegister_ExpiredInvite(t *testing.T) {
	svc, _, invites, _, _ := newTestService(t)
	past := time.Now().Add(-time.Hour)
	seedInvite(t, invites, validInvite, &past)

	_, err := svc.Register(context.Background(), validRegisterReq())
	assert.ErrorIs(t, err, auth.ErrInvalidInvite)
}

func TestRegister_UsedInvite(t *testing.T) {
	svc, _, invites, _, _ := newTestService(t)
	seedInvite(t, invites, validInvite, nil)
	usedBy := "someone"
	inv, _ := invites.GetByCode(context.Background(), validInvite)
	inv.UsedBy = &usedBy
	inv.UsedAt = ptr(time.Now())

	_, err := svc.Register(context.Background(), validRegisterReq())
	assert.ErrorIs(t, err, auth.ErrInvalidInvite)
}

func TestRegister_UsernameTaken(t *testing.T) {
	svc, users, invites, _, _ := newTestService(t)
	seedInvite(t, invites, validInvite, nil)
	seedUser(t, users, "alice", password)

	_, err := svc.Register(context.Background(), validRegisterReq())
	assert.ErrorIs(t, err, auth.ErrUsernameTaken)
}

func TestRegister_WeakPassword(t *testing.T) {
	svc, _, invites, _, _ := newTestService(t)
	seedInvite(t, invites, validInvite, nil)

	req := validRegisterReq()
	req.Password = "short"
	_, err := svc.Register(context.Background(), req)
	assert.ErrorIs(t, err, auth.ErrWeakPassword)
}

func TestRegister_EmptyUsername(t *testing.T) {
	svc, _, invites, _, _ := newTestService(t)
	seedInvite(t, invites, validInvite, nil)

	req := validRegisterReq()
	req.Username = "  "
	_, err := svc.Register(context.Background(), req)
	assert.Error(t, err)
}

// ---- 登录 ----

func TestLogin_Success(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	res, err := svc.Login(context.Background(), auth.LoginRequest{Username: "alice", Password: password})
	require.NoError(t, err)
	assert.Equal(t, u.ID, res.User.ID)
	assert.NotEmpty(t, res.AccessToken)
	assert.NotEmpty(t, res.RefreshToken)
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	seedUser(t, users, "alice", password)

	_, err := svc.Login(context.Background(), auth.LoginRequest{Username: "alice", Password: "wrong-pass"})
	assert.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

func TestLogin_UnknownUser(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	_, err := svc.Login(context.Background(), auth.LoginRequest{Username: "nobody", Password: password})
	assert.ErrorIs(t, err, auth.ErrInvalidCredentials)
}

// ---- 刷新 / 登出 ----

func TestRefresh_SuccessAndRotation(t *testing.T) {
	svc, users, _, _, store := newTestService(t)
	seedUser(t, users, "alice", password)

	loginRes, err := svc.Login(context.Background(), auth.LoginRequest{Username: "alice", Password: password})
	require.NoError(t, err)
	oldRefresh := loginRes.RefreshToken

	res, err := svc.Refresh(context.Background(), oldRefresh)
	require.NoError(t, err)
	assert.NotEmpty(t, res.AccessToken)
	assert.NotEqual(t, oldRefresh, res.RefreshToken, "刷新必须轮换 refresh token")

	// 旧 refresh 已失效
	_, err = svc.Refresh(context.Background(), oldRefresh)
	assert.ErrorIs(t, err, auth.ErrInvalidRefreshToken)

	// 新 refresh 仍有效
	_, err = svc.Refresh(context.Background(), res.RefreshToken)
	require.NoError(t, err)

	// Redis 中旧 key 已删除
	_, err = store.Get(context.Background(), "refresh:"+oldRefresh)
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestRefresh_InvalidToken(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	_, err := svc.Refresh(context.Background(), "no-such-token")
	assert.ErrorIs(t, err, auth.ErrInvalidRefreshToken)
}

func TestLogout_RevokesRefresh(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	seedUser(t, users, "alice", password)

	loginRes, err := svc.Login(context.Background(), auth.LoginRequest{Username: "alice", Password: password})
	require.NoError(t, err)

	require.NoError(t, svc.Logout(context.Background(), loginRes.RefreshToken))

	_, err = svc.Refresh(context.Background(), loginRes.RefreshToken)
	assert.ErrorIs(t, err, auth.ErrInvalidRefreshToken)
}

func TestLogout_InvalidToken(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	err := svc.Logout(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrInvalidRefreshToken)
}

// ---- 个人资料 ----

func TestMe(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	got, err := svc.Me(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)
	assert.Equal(t, "alice", got.Username)
}

func TestMe_NotFound(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	_, err := svc.Me(context.Background(), "no-such-id")
	assert.ErrorIs(t, err, auth.ErrUserNotFound)
}

func TestUpdateMe_DisplayName(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	newName := "Alice2"
	res, err := svc.UpdateMe(context.Background(), u.ID, auth.UpdateMeRequest{DisplayName: &newName})
	require.NoError(t, err)
	assert.Equal(t, newName, res.DisplayName)
	assert.Equal(t, password, "password123", "不改密码时密码不受影响")
	// 数据库中的密码 hash 未变
	updated, _ := users.GetByID(context.Background(), u.ID)
	assert.Equal(t, u.PasswordHash, updated.PasswordHash)
}

func TestUpdateMe_Password(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	newPwd := "new-password-456"
	old := password
	res, err := svc.UpdateMe(context.Background(), u.ID, auth.UpdateMeRequest{
		Password: &newPwd, OldPassword: &old,
	})
	require.NoError(t, err)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(res.PasswordHash), []byte(newPwd)))

	// 新密码可登录
	_, err = svc.Login(context.Background(), auth.LoginRequest{Username: "alice", Password: newPwd})
	require.NoError(t, err)
}

func TestUpdateMe_Password_WrongOld(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	newPwd := "new-password-456"
	wrongOld := "wrong-old"
	_, err := svc.UpdateMe(context.Background(), u.ID, auth.UpdateMeRequest{
		Password: &newPwd, OldPassword: &wrongOld,
	})
	assert.ErrorIs(t, err, auth.ErrOldPasswordMismatch)
}

func TestUpdateMe_Password_Weak(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	weak := "short"
	old := password
	_, err := svc.UpdateMe(context.Background(), u.ID, auth.UpdateMeRequest{
		Password: &weak, OldPassword: &old,
	})
	assert.ErrorIs(t, err, auth.ErrWeakPassword)
}

func TestUpdateMe_Password_MissingOld(t *testing.T) {
	svc, users, _, _, _ := newTestService(t)
	u := seedUser(t, users, "alice", password)

	newPwd := "new-password-456"
	_, err := svc.UpdateMe(context.Background(), u.ID, auth.UpdateMeRequest{
		Password: &newPwd, // 未提供旧密码
	})
	assert.ErrorIs(t, err, auth.ErrOldPasswordMismatch)
}

func ptr[T any](v T) *T { return &v }
