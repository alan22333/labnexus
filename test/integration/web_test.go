//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 前端外壳:静态页面可访问且含关键 UI 标记(TDD:先红后绿)
func TestWeb_StaticShell(t *testing.T) {
	r := setupServer(t)

	w := doJSON(t, r, http.MethodGet, "/", "", "")
	t.Logf("GET / → %d body=%q", w.Code, w.Body.String()[:min(200, w.Body.Len())])
	body := w.Body.String()
	assert.Contains(t, body, `id="app-view"`, "应含主应用容器")
	assert.Contains(t, body, "login", "应含登录标记")
	assert.Contains(t, body, "LabNexus", "应含品牌名")

	wJS := doJSON(t, r, http.MethodGet, "/app.js", "", "")
	require.Equal(t, http.StatusOK, wJS.Code, "app.js 应可访问")

	wCSS := doJSON(t, r, http.MethodGet, "/style.css", "", "")
	require.Equal(t, http.StatusOK, wCSS.Code, "style.css 应可访问")
}
