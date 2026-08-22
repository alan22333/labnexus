//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 资源全流程:A 建标签 → 建 link/paper → 上传文件 → 筛选列表 → B 可见
// → 权限:B 改/删 403 → A 删 204
func TestResource_FullFlow(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	// 标签
	tagID := createTag(t, r, tokenA, "文献-2025")

	// 建 link(带标签)
	wLink := doJSON(t, r, http.MethodPost, "/api/resources",
		fmt.Sprintf(`{"type":"link","title":"深度学习综述","url":"https://example.com/dl","tag_ids":["%s"]}`, tagID),
		tokenA)
	require.Equal(t, http.StatusCreated, wLink.Code, "%s", wLink.Body.String())
	assert.Contains(t, wLink.Body.String(), `"type":"link"`)
	var link struct {
		Resource struct {
			ID   string `json:"id"`
			Tags []any  `json:"tags"`
		} `json:"resource"`
	}
	parseJSON(t, wLink, &link)
	require.Len(t, link.Resource.Tags, 1, "link 应带标签")

	// 建 paper
	wPaper := doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"paper","title":"Attention Paper","doi":"10.1000/attention"}`, tokenA)
	require.Equal(t, http.StatusCreated, wPaper.Code, "%s", wPaper.Body.String())

	// 上传文件
	wUp := doMultipart(t, r, "/api/resources/upload", "论文.pdf", "%PDF-1.4 smoke", tokenA)
	require.Equal(t, http.StatusCreated, wUp.Code, "%s", wUp.Body.String())
	assert.Contains(t, wUp.Body.String(), `"type":"file"`)
	assert.NotContains(t, wUp.Body.String(), `"file_path"`, "内部路径不暴露")

	// B 列表(共享可见)+ 筛选
	wList := doJSON(t, r, http.MethodGet, "/api/resources?type=link", "", tokenB)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "深度学习综述")

	wKw := doJSON(t, r, http.MethodGet, "/api/resources?keyword=attention", "", tokenB)
	assert.Contains(t, wKw.Body.String(), "Attention Paper")

	wTag := doJSON(t, r, http.MethodGet, "/api/resources?tag_id="+tagID, "", tokenB)
	assert.Contains(t, wTag.Body.String(), "深度学习综述")

	// 权限:B 改/删 A 的资源 → 403
	wPatchB := doJSON(t, r, http.MethodPatch, "/api/resources/"+link.Resource.ID,
		`{"title":"hack"}`, tokenB)
	assert.Equal(t, http.StatusForbidden, wPatchB.Code)
	wDelB := doJSON(t, r, http.MethodDelete, "/api/resources/"+link.Resource.ID, "", tokenB)
	assert.Equal(t, http.StatusForbidden, wDelB.Code)

	// A 删除 → 204
	wDelA := doJSON(t, r, http.MethodDelete, "/api/resources/"+link.Resource.ID, "", tokenA)
	assert.Equal(t, http.StatusNoContent, wDelA.Code)
}

// 元数据抓取依赖外部网络,端到端不覆盖(单元测试已用 mock 覆盖 Crossref/arXiv)
func TestResource_PaperMetaRequiresParam(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	w := doJSON(t, r, http.MethodGet, "/api/resources/paper/meta", "", tokenA)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "VALIDATION", errorCode(t, w))
}

var _ = json.Marshal
