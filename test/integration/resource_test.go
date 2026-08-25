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

// 资源全流程:A 建标签 → 建 link → 上传文件 → 筛选列表 → B 可见
// → 权限:B 改/删 403 → A 删 204
func TestResource_FullFlow(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	// 标签
	tagID := createTag(t, r, tokenA, "文献-2025")

	// 建 link(带标签)
	wLink := doJSON(t, r, http.MethodPost, "/api/resources",
		fmt.Sprintf(`{"type":"link","title":"深度学习综述","url":"https://example.com/dl","description":"入门必读","tag_ids":["%s"]}`, tagID),
		tokenA)
	require.Equal(t, http.StatusCreated, wLink.Code, "%s", wLink.Body.String())
	assert.Contains(t, wLink.Body.String(), `"type":"link"`)
	assert.Contains(t, wLink.Body.String(), `"description":"入门必读"`)
	var link struct {
		Resource struct {
			ID   string `json:"id"`
			Tags []any  `json:"tags"`
		} `json:"resource"`
	}
	parseJSON(t, wLink, &link)
	require.Len(t, link.Resource.Tags, 1, "link 应带标签")

	// 上传文件(PDF 论文)
	wUp := doMultipart(t, r, "/api/resources/upload", "论文.pdf", "%PDF-1.4 smoke", tokenA)
	require.Equal(t, http.StatusCreated, wUp.Code, "%s", wUp.Body.String())
	assert.Contains(t, wUp.Body.String(), `"type":"file"`)
	assert.Contains(t, wUp.Body.String(), `"mime_type":"application/pdf"`)
	assert.Contains(t, wUp.Body.String(), `"preview":{"supported":true,"type":"pdf"`)
	assert.Contains(t, wUp.Body.String(), `"download_url"`)
	assert.NotContains(t, wUp.Body.String(), `"file_path"`, "内部路径不暴露")
	var up struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	parseJSON(t, wUp, &up)

	// B 列表(共享可见)+ 筛选
	wList := doJSON(t, r, http.MethodGet, "/api/resources?type=link", "", tokenB)
	require.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "深度学习综述")

	wKw := doJSON(t, r, http.MethodGet, "/api/resources?keyword=%E8%AE%BA%E6%96%87", "", tokenB)
	assert.Contains(t, wKw.Body.String(), "论文")

	wTag := doJSON(t, r, http.MethodGet, "/api/resources?tag_id="+tagID, "", tokenB)
	assert.Contains(t, wTag.Body.String(), "深度学习综述")

	// B 可下载/预览(共享)
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/resources/"+up.Resource.ID+"/download", "", tokenB).Code)
	assert.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/api/resources/"+up.Resource.ID+"/preview", "", tokenB).Code)

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

// 下载/预览契约:仅 file;link 400;非上传者可用
func TestResource_DownloadPreviewFlow(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	// file:下载 attachment + 预览 inline
	w := doMultipart(t, r, "/api/resources/upload", "报告.pdf", "%PDF-1.4 下载预览", tokenA)
	var created struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	parseJSON(t, w, &created)
	id := created.Resource.ID

	wd := doJSON(t, r, http.MethodGet, "/api/resources/"+id+"/download", "", tokenB)
	require.Equal(t, http.StatusOK, wd.Code)
	assert.Equal(t, "attachment", wd.Header().Get("Content-Disposition")[:10])
	assert.Contains(t, wd.Header().Get("Content-Type"), "application/pdf")
	assert.Contains(t, wd.Body.String(), "%PDF-1.4 下载预览")

	wp := doJSON(t, r, http.MethodGet, "/api/resources/"+id+"/preview", "", tokenB)
	require.Equal(t, http.StatusOK, wp.Code)
	assert.Equal(t, "inline", wp.Header().Get("Content-Disposition")[:6])

	// link:下载/预览 400
	wl := doJSON(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"x","url":"https://e.com"}`, tokenA)
	var link struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	parseJSON(t, wl, &link)
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/api/resources/"+link.Resource.ID+"/download", "", tokenA).Code)
	assert.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/api/resources/"+link.Resource.ID+"/preview", "", tokenA).Code)
}

var _ = json.Marshal
