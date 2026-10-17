package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/admin"
	"labnexus/internal/resource"
	"labnexus/internal/user"
)

type memResourceCategories struct {
	byID map[string]*resource.ResourceCategory
}

func newMemResourceCategories() *memResourceCategories {
	return &memResourceCategories{byID: map[string]*resource.ResourceCategory{}}
}

func (m *memResourceCategories) ListCategories(_ context.Context, includeInactive bool) ([]*resource.ResourceCategory, error) {
	out := make([]*resource.ResourceCategory, 0, len(m.byID))
	for _, c := range m.byID {
		if !includeInactive && !c.Active {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (m *memResourceCategories) CreateCategory(_ context.Context, name string, sort *int) (*resource.ResourceCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, resource.ErrCategoryNameEmpty
	}
	for _, c := range m.byID {
		if c.Name == name {
			return nil, resource.ErrCategoryExists
		}
	}
	order := len(m.byID) + 1
	if sort != nil {
		order = *sort
	}
	c := resource.NewResourceCategory(name, order, false)
	m.byID[c.ID] = c
	return c, nil
}

func (m *memResourceCategories) UpdateCategory(_ context.Context, id string, req resource.UpdateCategoryRequest) (*resource.ResourceCategory, error) {
	c, ok := m.byID[id]
	if !ok {
		return nil, resource.ErrCategoryNotFound
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, resource.ErrCategoryNameEmpty
		}
		for _, other := range m.byID {
			if other.ID != id && other.Name == name {
				return nil, resource.ErrCategoryExists
			}
		}
		c.Name = name
	}
	if req.Sort != nil {
		c.Sort = *req.Sort
	}
	if req.Active != nil {
		c.Active = *req.Active
	}
	return c, nil
}

func (m *memResourceCategories) DeleteCategory(_ context.Context, id string) error {
	if _, ok := m.byID[id]; !ok {
		return resource.ErrCategoryNotFound
	}
	delete(m.byID, id)
	return nil
}

func newCategoryRouter() (*gin.Engine, *memResourceCategories) {
	gin.SetMode(gin.TestMode)
	f := newFixture()
	cats := newMemResourceCategories()
	f.svc = f.svc.WithResourceCategories(cats)
	h := admin.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, cats
}

func TestV2_AdminResourceCategories_PermissionAndCRUD(t *testing.T) {
	r, _ := newCategoryRouter()
	auth := adminAuth(adminUser, user.RoleAdmin)

	assert.Equal(t, http.StatusUnauthorized,
		admDo(t, r, http.MethodGet, "/api/admin/resource-categories", "", "").Code)
	assert.Equal(t, http.StatusForbidden,
		admDo(t, r, http.MethodGet, "/api/admin/resource-categories", "", adminAuth(stu, user.RoleStudent)).Code)

	created := admDo(t, r, http.MethodPost, "/api/admin/resource-categories", `{"name":"文献"}`, auth)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var body struct {
		Category struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"category"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &body))
	require.NotEmpty(t, body.Category.ID)
	assert.Equal(t, "文献", body.Category.Name)

	assert.Equal(t, http.StatusBadRequest,
		admDo(t, r, http.MethodPost, "/api/admin/resource-categories", `{"name":"  "}`, auth).Code)
	assert.Equal(t, http.StatusConflict,
		admDo(t, r, http.MethodPost, "/api/admin/resource-categories", `{"name":"文献"}`, auth).Code)

	updated := admDo(t, r, http.MethodPatch, "/api/admin/resource-categories/"+body.Category.ID,
		`{"name":"参考文献","sort":9,"active":false}`, auth)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	assert.Contains(t, updated.Body.String(), `"name":"参考文献"`)
	assert.Contains(t, updated.Body.String(), `"active":false`)

	assert.Equal(t, http.StatusOK,
		admDo(t, r, http.MethodGet, "/api/admin/resource-categories", "", auth).Code)
	assert.Equal(t, http.StatusNotFound,
		admDo(t, r, http.MethodPatch, "/api/admin/resource-categories/missing", `{"name":"x"}`, auth).Code)

	assert.Equal(t, http.StatusNoContent,
		admDo(t, r, http.MethodDelete, "/api/admin/resource-categories/"+body.Category.ID, "", auth).Code)
	assert.Equal(t, http.StatusNotFound,
		admDo(t, r, http.MethodDelete, "/api/admin/resource-categories/"+body.Category.ID, "", auth).Code)
}
