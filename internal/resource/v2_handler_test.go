package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/resource"
	"labnexus/internal/user"
)

func TestV2_Handler_DuplicateLinkConflict(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	auth := authHeader(userA)

	first := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"第一次","url":"https://example.com/dup"}`, auth)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"重复","url":"https://example.com/dup"}`, auth)
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	var body struct {
		ExistingID string `json:"existing_id"`
	}
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	assert.NotEmpty(t, body.ExistingID)

	forced := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"强制","url":"https://example.com/dup","force":true}`, auth)
	assert.Equal(t, http.StatusCreated, forced.Code, forced.Body.String())

	badSort := resDo(t, r, http.MethodGet, "/api/resources?sort=bogus", "", auth)
	assert.Equal(t, http.StatusBadRequest, badSort.Code)
}

func TestV2_Handler_ViewFavoriteRoutes(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	auth := authHeader(userA)

	created := resDo(t, r, http.MethodPost, "/api/resources",
		`{"type":"link","title":"详情","url":"https://example.com/detail"}`, auth)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var payload struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &payload))
	id := payload.Resource.ID
	require.NotEmpty(t, id)

	assert.Equal(t, http.StatusNoContent,
		resDo(t, r, http.MethodPost, "/api/resources/"+id+"/view", "", auth).Code)
	assert.Equal(t, http.StatusNoContent,
		resDo(t, r, http.MethodPost, "/api/resources/"+id+"/favorite", "", auth).Code)

	got := resDo(t, r, http.MethodGet, "/api/resources/"+id, "", auth)
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	assert.Contains(t, got.Body.String(), `"favorited":true`)
	assert.Contains(t, got.Body.String(), `"view_count":1`)

	assert.Equal(t, http.StatusNoContent,
		resDo(t, r, http.MethodDelete, "/api/resources/"+id+"/favorite", "", auth).Code)
	got = resDo(t, r, http.MethodGet, "/api/resources/"+id, "", auth)
	assert.Contains(t, got.Body.String(), `"favorited":false`)

	assert.Equal(t, http.StatusNotFound,
		resDo(t, r, http.MethodPost, "/api/resources/missing/view", "", auth).Code)
	assert.Equal(t, http.StatusNotFound,
		resDo(t, r, http.MethodPost, "/api/resources/missing/favorite", "", auth).Code)
}

func TestV2_Handler_ResourceCategories(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	cats := newMemCategories(f.res)
	cats.seed("文献", true)
	cats.seed("已停用", false)
	f.svc = f.svc.WithCategoryRepository(cats)
	h := resource.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")

	w := resDo(t, r, http.MethodGet, "/api/resource-categories", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Categories []struct {
			Name   string `json:"name"`
			Active bool   `json:"active"`
		} `json:"categories"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Categories, 1)
	assert.Equal(t, "文献", body.Categories[0].Name)
	assert.True(t, body.Categories[0].Active)

	// 未登录不可读
	assert.Equal(t, http.StatusUnauthorized,
		resDo(t, r, http.MethodGet, "/api/resource-categories", "", "").Code)
}

func TestV2_Handler_ListQueryValidation(t *testing.T) {
	r, f := newTestRouter(t)
	f.seedUser(userA, user.RoleStudent)
	auth := authHeader(userA)

	for _, tc := range []string{
		"/api/resources?uncategorized=not-bool",
		"/api/resources?mine=maybe",
		"/api/resources?favorited=2",
		"/api/resources?pinned=nah",
	} {
		w := resDo(t, r, http.MethodGet, tc, "", auth)
		assert.Equal(t, http.StatusBadRequest, w.Code, tc)
	}

	// 组合参数应能正常走到列表(内存仓库忽略具体筛选,但 handler 不应报错)
	path := fmt.Sprintf("/api/resources?tag_ids=a,b&category_id=%s&mine=true&favorited=true&pinned=true&sort=useful", "cat-1")
	assert.Equal(t, http.StatusOK, resDo(t, r, http.MethodGet, path, "", auth).Code)
}
