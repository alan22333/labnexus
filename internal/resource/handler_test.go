package resource_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/resource"
	"labnexus/internal/token"
	"labnexus/internal/user"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	h := resource.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, f
}

func authHeader(userID string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, "student", 15*time.Minute)
	return "Bearer " + access
}

func adminHeader() string {
	access, _ := token.GenerateAccessToken("test-secret", "admin-user", "admin", 15*time.Minute)
	return "Bearer " + access
}

func resDo(t *testing.T, r *gin.Engine, method, path, body, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body != "" {
		reqBody = bytes.NewReader([]byte(body))
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if tokenStr != "" {
		req.Header.Set("Authorization", tokenStr)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func multipartUpload(t *testing.T, r *gin.Engine, path, filename, content, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, _ = fw.Write([]byte(content))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestResource_RequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/resources"},
		{http.MethodPost, "/api/resources"},
		{http.MethodGet, "/api/resources/x"},
	} {
		w := resDo(t, r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestResource_CreateLink(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	tg := f.seedTag("文献")

	w := resDo(t, r, http.MethodPost, "/api/resources",
		fmt.Sprintf(`{"type":"link","title":"好文章","url":"https://e.com/a","tag_ids":["%s"]}`, tg.ID),
		authHeader(userA))
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"type":"link"`)
	assert.Contains(t, w.Body.String(), `"title":"好文章"`)
	assert.Contains(t, w.Body.String(), `"uploader"`)
}

func TestResource_CreateValidation(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)

	// link 缺 url
	w := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"x"}`, authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "VALIDATION")

	// paper 缺 doi/arxiv
	w2 := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"paper","title":"x"}`, authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w2.Code)

	// 非法 type
	w3 := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"video","title":"x","url":"https://e.com"}`, authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w3.Code)
}

func TestResource_UploadFile(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)

	w := multipartUpload(t, r, "/api/resources/upload", "paper.pdf", "%PDF-1.4", authHeader(userA))
	require.Equal(t, http.StatusCreated, w.Code, "%s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"type":"file"`)
	assert.NotContains(t, w.Body.String(), `"file_path"`, "file_path 不应暴露")

	// 非法扩展名
	w2 := multipartUpload(t, r, "/api/resources/upload", "evil.exe", "MZ", authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestResource_ListAndGet(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"深度学习入门","url":"https://e.com/a"}`, authHeader(userA))
	resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"paper","title":"Attention","doi":"10.1/x"}`, authHeader(userA))

	// 列表 + type 筛选
	w := resDo(t, r, http.MethodGet, "/api/resources?type=link", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "深度学习入门")
	assert.NotContains(t, w.Body.String(), "Attention")

	// keyword 筛选
	w2 := resDo(t, r, http.MethodGet, "/api/resources?keyword=attention", "", authHeader(userA))
	assert.Contains(t, w2.Body.String(), "Attention")

	// 详情
	var list struct {
		Resources []struct {
			ID string `json:"id"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.NotEmpty(t, list.Resources)
	w3 := resDo(t, r, http.MethodGet, "/api/resources/"+list.Resources[0].ID, "", authHeader(userA))
	require.Equal(t, http.StatusOK, w3.Code)
}

func TestResource_Permission(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser("user-b", user.RoleStudent)
	f.seedUser("admin-user", user.RoleAdmin)

	w := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"A的资源","url":"https://e.com/a"}`, authHeader(userA))
	var created struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	id := created.Resource.ID

	// 他人修改/删除 → 403
	wPatch := resDo(t, r, http.MethodPatch, "/api/resources/"+id, `{"title":"hack"}`, authHeader("user-b"))
	assert.Equal(t, http.StatusForbidden, wPatch.Code)
	wDel := resDo(t, r, http.MethodDelete, "/api/resources/"+id, "", authHeader("user-b"))
	assert.Equal(t, http.StatusForbidden, wDel.Code)

	// admin 可删
	wAdmin := resDo(t, r, http.MethodDelete, "/api/resources/"+id, "", adminHeader())
	assert.Equal(t, http.StatusNoContent, wAdmin.Code)
}

func TestResource_PaperMeta(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	// mock 外部服务
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"title":["Meta Paper"],"author":[{"given":"A","family":"B"}],"DOI":"10.1/meta"}}`))
	}))
	defer mock.Close()
	f.svc.WithEndpoints(mock.Client(), mock.URL, mock.URL)

	w := resDo(t, r, http.MethodGet, "/api/resources/paper/meta?doi=10.1/meta", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Meta Paper")

	// 无参数 → 400
	w2 := resDo(t, r, http.MethodGet, "/api/resources/paper/meta", "", authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w2.Code)
}
