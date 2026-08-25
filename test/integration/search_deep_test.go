//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 搜索聚合:F7/F9 上线后,search 应覆盖文档 + 资源 + 任务(规格 f6 的承诺)
func TestSearch_AcrossTypes(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	// 文档(公开)
	docID := createDoc(t, r, tokenA, "文献综述初稿", "public")
	_ = docID
	// 资源(link,标题含关键词)
	require.Equal(t, http.StatusCreated, doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"文献管理工具","url":"https://zotero.org"}`, tokenA).Code)
	// 任务(标题含关键词,在 A 的项目里)
	pid := createProjectViaAPI(t, r, tokenA, "综述项目")
	tid := createTaskViaAPI(t, r, tokenA, pid, "整理文献清单", "")
	_ = tid

	// 默认聚合搜索(他人 B 可见公开文档与共享资源;任务全局可搜)
	w := doJSON(t, r, http.MethodGet, "/api/search?q=%E6%96%87%E7%8C%AE", "", tokenB)
	require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
	var body struct {
		Documents []struct {
			Title string `json:"title"`
		} `json:"documents"`
		Resources []struct {
			Title string `json:"title"`
		} `json:"resources"`
		Tasks []struct {
			Title string `json:"title"`
		} `json:"tasks"`
	}
	parseJSON(t, w, &body)
	assert.Equal(t, "文献综述初稿", body.Documents[0].Title, "文档应命中")
	assert.Equal(t, "文献管理工具", body.Resources[0].Title, "资源应命中")
	assert.Equal(t, "整理文献清单", body.Tasks[0].Title, "任务应命中")

	// type 定向搜索
	wR := doJSON(t, r, http.MethodGet, "/api/search?q=%E6%96%87%E7%8C%AE&type=resource", "", tokenB)
	parseJSON(t, wR, &body)
	assert.Empty(t, body.Documents)
	assert.Equal(t, "文献管理工具", body.Resources[0].Title)
	assert.Empty(t, body.Tasks)

	wT := doJSON(t, r, http.MethodGet, "/api/search?q=%E6%96%87%E7%8C%AE&type=task", "", tokenB)
	parseJSON(t, wT, &body)
	assert.Empty(t, body.Documents)
	assert.Equal(t, "整理文献清单", body.Tasks[0].Title)

	// 无匹配 → 三组空数组(非 null)
	wNone := doJSON(t, r, http.MethodGet, "/api/search?q=zzzznomatch", "", tokenB)
	require.Equal(t, http.StatusOK, wNone.Code)
	assert.Contains(t, wNone.Body.String(), `"documents":[]`)
	assert.Contains(t, wNone.Body.String(), `"resources":[]`)
	assert.Contains(t, wNone.Body.String(), `"tasks":[]`)
}

// 搜索可见性:他人私有文档不泄露(资源共享、任务全局,与文档不同)
func TestSearch_VisibilityPerType(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	createDoc(t, r, tokenA, "A的私密文献笔记", "private")
	require.Equal(t, http.StatusCreated, doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"共享文献资源","url":"https://paper.example.com/x"}`, tokenA).Code)

	w := doJSON(t, r, http.MethodGet, "/api/search?q=%E6%96%87%E7%8C%AE", "", tokenB)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "A的私密文献笔记", "他人私有文档不应泄露")
	assert.Contains(t, w.Body.String(), "共享文献资源", "资源共享应可见")
}
