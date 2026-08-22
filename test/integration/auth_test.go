//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/token"
)

// 注册 → me → 刷新轮换 → 登出 → 全部失效 的完整认证链路
func TestAuth_FullLifecycle(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	// me
	w := doJSON(t, r, http.MethodGet, "/api/me", "", tokenA)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)

	// 登录拿 refresh cookie
	wLogin := doJSON(t, r, http.MethodPost, "/api/auth/login", `{"username":"alice","password":"password123"}`, "")
	require.Equal(t, http.StatusOK, wLogin.Code)
	oldRefresh := refreshCookie(t, wLogin)

	// 用旧 refresh 刷新 → 轮换出新 token
	wRef := doCookie(t, r, http.MethodPost, "/api/auth/refresh", []*http.Cookie{oldRefresh})
	require.Equal(t, http.StatusOK, wRef.Code, "刷新失败: %s", wRef.Body.String())
	newRefresh := refreshCookie(t, wRef)
	assert.NotEqual(t, oldRefresh.Value, newRefresh.Value, "refresh 必须轮换")

	// 旧 refresh 已失效
	wOld := doCookie(t, r, http.MethodPost, "/api/auth/refresh", []*http.Cookie{oldRefresh})
	assert.Equal(t, http.StatusUnauthorized, wOld.Code)

	// 新 refresh 登出
	wLogout := doCookie(t, r, http.MethodPost, "/api/auth/logout", []*http.Cookie{newRefresh})
	assert.Equal(t, http.StatusNoContent, wLogout.Code)

	// 登出后 refresh 失效
	wAfter := doCookie(t, r, http.MethodPost, "/api/auth/refresh", []*http.Cookie{newRefresh})
	assert.Equal(t, http.StatusUnauthorized, wAfter.Code)
}

func TestAuth_InviteValidation(t *testing.T) {
	r := setupServer(t)

	// 不存在的邀请码
	seedInvite(t, "VALID-CODE")
	w := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"WRONG","username":"u1","display_name":"U","password":"password123"}`, "")
	assertError(t, w, http.StatusUnauthorized, "INVALID_INVITE")

	// 已使用的邀请码
	w1 := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"VALID-CODE","username":"u1","display_name":"U","password":"password123"}`, "")
	require.Equal(t, http.StatusCreated, w1.Code)
	w2 := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"VALID-CODE","username":"u2","display_name":"U2","password":"password123"}`, "")
	assertError(t, w2, http.StatusUnauthorized, "INVALID_INVITE")

	// 过期的邀请码
	db := connectDB(t)
	require.NoError(t, db.Exec(
		"INSERT INTO invite_codes (id, code, created_by, expires_at) VALUES (gen_random_uuid(), 'EXPIRED', '00000000-0000-0000-0000-000000000000', now() - interval '1 hour')",
	).Error)
	closeDB(db)
	w3 := doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"EXPIRED","username":"u3","display_name":"U3","password":"password123"}`, "")
	assertError(t, w3, http.StatusUnauthorized, "INVALID_INVITE")
}

func TestAuth_RegisterValidation(t *testing.T) {
	r := setupServer(t)
	seedInvite(t, "R-CODE")
	body := `{"invite_code":"R-CODE","username":"dup","display_name":"D","password":"password123"}`

	require.Equal(t, http.StatusCreated, doJSON(t, r, http.MethodPost, "/api/auth/register", body, "").Code)

	// 同一邀请码重复使用 → 401 INVALID_INVITE(邀请码优先校验)
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/register", body, ""),
		http.StatusUnauthorized, "INVALID_INVITE")

	// 新邀请码 + 重复用户名 → 409 CONFLICT
	seedInvite(t, "R-CODE2")
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"R-CODE2","username":"dup","display_name":"D","password":"password123"}`, ""),
		http.StatusConflict, "CONFLICT")
	// 弱密码 400
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/register",
		`{"invite_code":"R-CODE","username":"weak","display_name":"W","password":"short"}`, ""),
		http.StatusBadRequest, "VALIDATION")
	// 非法 JSON 400
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/register", `{bad`, ""),
		http.StatusBadRequest, "VALIDATION")
}

// 无 token / 坏 token / 过期 token 访问受保护端点
func TestAuth_Protection(t *testing.T) {
	r := setupServer(t)

	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/me/space"},
		{http.MethodPost, "/api/me/folders"},
		{http.MethodGet, "/api/feed"},
		{http.MethodPost, "/api/me/documents"},
		{http.MethodGet, "/api/search?q=x"},
		{http.MethodGet, "/api/tags"},
		{http.MethodPost, "/api/tags"},
		{http.MethodGet, "/api/documents/x"},
		{http.MethodPost, "/api/documents/x/reactions"},
	}
	for _, tc := range protected {
		assert.Equal(t, http.StatusUnauthorized, doJSON(t, r, tc.method, tc.path, "", "").Code,
			"无 token 应 401: %s %s", tc.method, tc.path)
		assert.Equal(t, http.StatusUnauthorized, doJSON(t, r, tc.method, tc.path, "", "Bearer garbage").Code,
			"坏 token 应 401: %s %s", tc.method, tc.path)
	}

	// 过期 token(签一个 -1h 的,与 middleware 同 secret)
	expired := newExpiredToken(t, -time.Hour)
	assert.Equal(t, http.StatusUnauthorized, doJSON(t, r, http.MethodGet, "/api/me", "", expired).Code)
}

func TestAuth_LoginFailures(t *testing.T) {
	r := setupServer(t)
	registerUser(t, r, "alice", "Alice")

	// 密码错误与用户不存在返回同一错误(防枚举)
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"wrong"}`, ""), http.StatusUnauthorized, "AUTH_FAILED")
	assertError(t, doJSON(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"nobody","password":"password123"}`, ""), http.StatusUnauthorized, "AUTH_FAILED")
}

func TestAuth_UpdateMe(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	// 改昵称
	w := doJSON(t, r, http.MethodPatch, "/api/me", `{"display_name":"Alice2"}`, tokenA)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"display_name":"Alice2"`)

	// 改密码:旧密码错误 → 400
	wBad := doJSON(t, r, http.MethodPatch, "/api/me",
		`{"password":"new-password-456","old_password":"wrong"}`, tokenA)
	assertError(t, wBad, http.StatusBadRequest, "VALIDATION")

	// 改密码成功
	wOk := doJSON(t, r, http.MethodPatch, "/api/me",
		`{"password":"new-password-456","old_password":"password123"}`, tokenA)
	require.Equal(t, http.StatusOK, wOk.Code)

	// 新密码登录成功
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"new-password-456"}`, "").Code)
}

// newExpiredToken 签发指定年龄的 access token(与 middleware 相同的默认 secret)。
func newExpiredToken(t *testing.T, age time.Duration) string {
	t.Helper()
	access, err := token.GenerateAccessToken("dev-secret-change-me", "any-user", "student", age)
	require.NoError(t, err)
	return access
}

var _ = json.Marshal // 保留 json import(避免误删)
