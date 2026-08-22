//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 分页:page/page_size 归一化、上限截断、total 正确
func TestEdge_Pagination(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	for i := 0; i < 5; i++ {
		createDoc(t, r, tokenA, "公开帖"+string(rune('A'+i)), "public")
	}

	// page_size 上限截断为 50
	w := doJSON(t, r, http.MethodGet, "/api/feed?page=1&page_size=999", "", tokenA)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Documents  []any `json:"documents"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	parseJSON(t, w, &body)
	assert.Equal(t, 50, body.Pagination.PageSize)
	assert.Equal(t, 5, body.Pagination.Total)

	// page=0 归一化为 1;total 始终为总数(5)
	w2 := doJSON(t, r, http.MethodGet, "/api/feed?page=0&page_size=2", "", tokenA)
	parseJSON(t, w2, &body)
	assert.Equal(t, 1, body.Pagination.Page, "page=0 应归一化为 1")
	assert.Equal(t, 5, body.Pagination.Total)
	require.Len(t, body.Documents, 2, "第一页(page_size=2)应 2 条")

	// 超出范围页:空结果,但 total 正确
	w3 := doJSON(t, r, http.MethodGet, "/api/feed?page=4&page_size=2", "", tokenA)
	parseJSON(t, w3, &body)
	assert.Empty(t, body.Documents, "第 4 页应无数据(3 页共 5 条)")
	assert.Equal(t, 5, body.Pagination.Total)
}

// 输入校验矩阵:所有 400 场景
func TestEdge_Validation(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tagID := createTag(t, r, tokenA, "已有标签")
	docID := createDoc(t, r, tokenA, "公开帖", "public")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"空标题", http.MethodPost, "/api/me/documents", `{"title":"","visibility":"private"}`},
		{"非法可见性", http.MethodPost, "/api/me/documents", `{"title":"x","visibility":"secret"}`},
		{"空评论", http.MethodPost, "/api/documents/" + docID + "/comments", `{"content":""}`},
		{"空搜索q", http.MethodGet, "/api/search?q=", ""},
		{"非法搜索type", http.MethodGet, "/api/search?q=x&type=video", ""},
		{"空标签名", http.MethodPost, "/api/tags", `{"name":""}`},
		{"空目录名", http.MethodPost, "/api/me/folders", `{"name":"  "}`},
	}
	for _, tc := range cases {
		w := doJSON(t, r, tc.method, tc.path, tc.body, tokenA)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s 应 400: %s", tc.name, w.Body.String())
		assert.Equal(t, "VALIDATION", errorCode(t, w), "%s 错误码", tc.name)
	}

	// 标签重名 → 409
	assertError(t, doJSON(t, r, http.MethodPost, "/api/tags", `{"name":"已有标签"}`, tokenA),
		http.StatusConflict, "CONFLICT")
	_ = tagID
}

// 点赞幂等:同人两次 = 取消;三人点赞计数 3
func TestEdge_ReactionToggle(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	docID := createDoc(t, r, tokenA, "热门帖", "public")

	// A 自己点赞(作者可赞自己的公开帖)
	assert.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/reactions", `{"emoji":"👍"}`, tokenA).Code)
	// 再点 = 取消
	assert.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/reactions", `{"emoji":"👍"}`, tokenA).Code)
	w1 := doJSON(t, r, http.MethodGet, "/api/documents/"+docID, "", tokenA)
	assert.Contains(t, w1.Body.String(), `"reactions_count":0`)

	// 三人各赞一次 → 3
	u2 := registerUser(t, r, "charlie", "Charlie")
	u3 := registerUser(t, r, "dave", "Dave")
	for _, tk := range []string{tokenA, u2, u3} {
		assert.Equal(t, http.StatusNoContent,
			doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/reactions", `{"emoji":"👍"}`, tk).Code)
	}
	w3 := doJSON(t, r, http.MethodGet, "/api/documents/"+docID, "", tokenA)
	assert.Contains(t, w3.Body.String(), `"reactions_count":3`)

	// hot 排序:该帖应在 feed 首位
	wFeed := doJSON(t, r, http.MethodGet, "/api/feed?sort=hot", "", tokenA)
	var feed struct {
		Documents []struct {
			Title string `json:"title"`
		} `json:"documents"`
	}
	parseJSON(t, wFeed, &feed)
	require.NotEmpty(t, feed.Documents)
	assert.Equal(t, "热门帖", feed.Documents[0].Title, "hot 排序应把点赞最多的放最前")
}

// 软删除:删后所有入口消失,但 DB 行仍在(deleted_at 非空)
func TestEdge_SoftDelete(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	docID := createDoc(t, r, tokenA, "待删除的公开帖", "public")

	assert.Equal(t, http.StatusNoContent, doJSON(t, r, http.MethodDelete, "/api/documents/"+docID, "", tokenA).Code)

	// 各入口消失
	assertError(t, doJSON(t, r, http.MethodGet, "/api/documents/"+docID, "", tokenB),
		http.StatusNotFound, "NOT_FOUND")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/feed", "", tokenB).Body.String(), "待删除的公开帖")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/me/documents", "", tokenA).Body.String(), "待删除的公开帖")
	assert.NotContains(t, doJSON(t, r, http.MethodGet, "/api/search?q=待删除", "", tokenA).Body.String(), "待删除的公开帖")

	// DB 行仍在(软删标记)
	db := connectDB(t)
	defer closeDB(db)
	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM documents WHERE id = ? AND deleted_at IS NOT NULL", docID).Scan(&count).Error)
	assert.Equal(t, int64(1), count, "软删除应保留数据行(deleted_at 非空)")
}

// 契约一致性:错误格式 / search 三组结构 / feed 分页结构 / 评论作者结构
func TestEdge_ContractShape(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	docID := createDoc(t, r, tokenA, "契约测试帖", "public")

	// 错误格式:始终 {error:{code,message}}
	wErr := doJSON(t, r, http.MethodGet, "/api/documents/no-such-id", "", tokenA)
	var errBody map[string]any
	parseJSON(t, wErr, &errBody)
	errObj, ok := errBody["error"].(map[string]any)
	require.True(t, ok, "错误响应必须有 error 对象")
	assert.NotEmpty(t, errObj["code"])
	assert.NotEmpty(t, errObj["message"])

	// search:三组结构固定
	wSearch := doJSON(t, r, http.MethodGet, "/api/search?q=契约", "", tokenA)
	var search struct {
		Documents []any `json:"documents"`
		Resources []any `json:"resources"`
		Tasks     []any `json:"tasks"`
	}
	parseJSON(t, wSearch, &search)
	require.NotNil(t, search.Documents)
	require.NotNil(t, search.Resources, "resources 必须为数组(可为空)")
	require.NotNil(t, search.Tasks, "tasks 必须为数组(可为空)")

	// feed:分页结构
	wFeed := doJSON(t, r, http.MethodGet, "/api/feed", "", tokenA)
	var feed struct {
		Documents  []any `json:"documents"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	parseJSON(t, wFeed, &feed)
	assert.Equal(t, 1, feed.Pagination.Page)
	assert.Equal(t, 20, feed.Pagination.PageSize)

	// 评论:author 结构完整
	wC := doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/comments",
		`{"content":"评论作者结构"}`, tokenA)
	var comment struct {
		Comment struct {
			Author struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"author"`
		} `json:"comment"`
	}
	parseJSON(t, wC, &comment)
	assert.NotEmpty(t, comment.Comment.Author.ID)
	assert.Equal(t, "Alice", comment.Comment.Author.DisplayName)
}

var _ = json.Marshal
