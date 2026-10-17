package resource_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/resource"
	"labnexus/internal/user"
)

// ---- v2 内存替身(分类仓库;资源仓库 v2 方法也在此文件补齐) ----

type memCategoryRepo struct {
	byID      map[string]*resource.ResourceCategory
	resources *memResourceRepo
}

func newMemCategories(resources *memResourceRepo) *memCategoryRepo {
	return &memCategoryRepo{byID: map[string]*resource.ResourceCategory{}, resources: resources}
}

func (r *memCategoryRepo) seed(name string, active bool) *resource.ResourceCategory {
	c := resource.NewResourceCategory(name, len(r.byID)+1, false)
	c.Active = active
	r.byID[c.ID] = c
	return c
}

func (r *memCategoryRepo) ListCategories(_ context.Context, includeInactive bool) ([]*resource.ResourceCategory, error) {
	out := make([]*resource.ResourceCategory, 0, len(r.byID))
	for _, c := range r.byID {
		if !includeInactive && !c.Active {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *memCategoryRepo) GetCategory(_ context.Context, id string) (*resource.ResourceCategory, error) {
	c, ok := r.byID[id]
	if !ok {
		return nil, resource.ErrCategoryNotFound
	}
	return c, nil
}

func (r *memCategoryRepo) CreateCategory(_ context.Context, c *resource.ResourceCategory) error {
	r.byID[c.ID] = c
	return nil
}

func (r *memCategoryRepo) UpdateCategory(_ context.Context, c *resource.ResourceCategory) error {
	r.byID[c.ID] = c
	return nil
}

func (r *memCategoryRepo) DeleteCategory(_ context.Context, id string) error {
	if _, ok := r.byID[id]; !ok {
		return resource.ErrCategoryNotFound
	}
	delete(r.byID, id)
	return nil
}

func (r *memCategoryRepo) ClearResourcesCategory(_ context.Context, categoryID string) error {
	for _, res := range r.resources.byID {
		if res.CategoryID != nil && *res.CategoryID == categoryID {
			res.CategoryID = nil
		}
	}
	return nil
}

func (r *memResourceRepo) FindLinkByURL(_ context.Context, rawURL string) (*resource.Resource, error) {
	for _, res := range r.byID {
		if res.Type == resource.TypeLink && strings.TrimSpace(res.URL) == strings.TrimSpace(rawURL) {
			return res, nil
		}
	}
	return nil, resource.ErrNotFound
}

func (r *memResourceRepo) IncrementViewCount(_ context.Context, id string) error {
	res, ok := r.byID[id]
	if !ok {
		return resource.ErrNotFound
	}
	res.ViewCount++
	return nil
}

func (r *memResourceRepo) IncrementDownloadCount(_ context.Context, id string) error {
	res, ok := r.byID[id]
	if !ok {
		return resource.ErrNotFound
	}
	res.DownloadCount++
	return nil
}

func (r *memResourceRepo) AddFavorite(_ context.Context, userID, resourceID string) error {
	if r.favorites == nil {
		r.favorites = map[string]map[string]bool{}
	}
	if r.favorites[userID] == nil {
		r.favorites[userID] = map[string]bool{}
	}
	r.favorites[userID][resourceID] = true
	return nil
}

func (r *memResourceRepo) RemoveFavorite(_ context.Context, userID, resourceID string) error {
	if r.favorites != nil {
		delete(r.favorites[userID], resourceID)
	}
	return nil
}

func (r *memResourceRepo) FavoriteResourceIDs(_ context.Context, userID string, resourceIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range resourceIDs {
		if r.favorites != nil && r.favorites[userID][id] {
			out[id] = true
		}
	}
	return out, nil
}

const userB = "user-b"

func boolPtr(b bool) *bool { return &b }

func TestV2_CreateLink_DuplicateURLAndForce(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	first, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "第一篇", URL: " https://example.com/paper ",
	})
	require.NoError(t, err)

	_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "重复", URL: "https://example.com/paper",
	})
	var dup *resource.DuplicateLinkError
	require.ErrorAs(t, err, &dup)
	assert.Equal(t, first.ID, dup.ExistingID)

	forced, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "允许重复", URL: "https://example.com/paper", Force: true,
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, forced.ID)
}

func TestV2_CreateLink_CategoryValidation(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	cats := newMemCategories(f.res)
	active := cats.seed("文献", true)
	inactive := cats.seed("停用分类", false)
	f.svc = f.svc.WithCategoryRepository(cats)

	view, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "带分类", URL: "https://example.com/a", CategoryID: strPtr(active.ID),
	})
	require.NoError(t, err)
	require.NotNil(t, view.Category)
	assert.Equal(t, "文献", view.Category.Name)

	_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "停用分类", URL: "https://example.com/b", CategoryID: strPtr(inactive.ID),
	})
	assert.ErrorIs(t, err, resource.ErrInvalidCategory)

	_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "不存在分类", URL: "https://example.com/c", CategoryID: strPtr("no-such"),
	})
	assert.ErrorIs(t, err, resource.ErrInvalidCategory)
}

func TestV2_FavoriteAndCounters(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser(userB, user.RoleStudent)
	view, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "收藏我", URL: "https://example.com/fav",
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.Favorite(context.Background(), userA, view.ID))
	require.NoError(t, f.svc.Favorite(context.Background(), userA, view.ID), "重复收藏幂等")

	got, err := f.svc.GetForUser(context.Background(), userA, view.ID)
	require.NoError(t, err)
	assert.True(t, got.Favorited)
	other, err := f.svc.GetForUser(context.Background(), userB, view.ID)
	require.NoError(t, err)
	assert.False(t, other.Favorited)

	require.NoError(t, f.svc.RecordView(context.Background(), view.ID))
	require.NoError(t, f.svc.RecordDownload(context.Background(), view.ID))
	stored, err := f.res.GetByID(context.Background(), view.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored.ViewCount)
	assert.EqualValues(t, 1, stored.DownloadCount)

	require.NoError(t, f.svc.Unfavorite(context.Background(), userA, view.ID))
	require.NoError(t, f.svc.Unfavorite(context.Background(), userA, view.ID), "取消收藏幂等")
	got, err = f.svc.GetForUser(context.Background(), userA, view.ID)
	require.NoError(t, err)
	assert.False(t, got.Favorited)

	assert.ErrorIs(t, f.svc.Favorite(context.Background(), userA, "missing"), resource.ErrResourceNotFound)
	assert.ErrorIs(t, f.svc.Unfavorite(context.Background(), userA, "missing"), resource.ErrResourceNotFound)
	assert.ErrorIs(t, f.svc.RecordView(context.Background(), "missing"), resource.ErrResourceNotFound)
}

func TestV2_Update_PinnedAndCategory(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser(userB, user.RoleStudent)
	cats := newMemCategories(f.res)
	cat := cats.seed("工具", true)
	f.svc = f.svc.WithCategoryRepository(cats)

	view, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "置顶我", URL: "https://example.com/pin",
	})
	require.NoError(t, err)

	updated, err := f.svc.Update(context.Background(), userA, view.ID, resource.UpdateRequest{
		Pinned: boolPtr(true), CategoryID: strPtr(cat.ID),
	})
	require.NoError(t, err)
	assert.True(t, updated.Pinned)
	require.NotNil(t, updated.Category)
	assert.Equal(t, cat.ID, updated.Category.ID)

	_, err = f.svc.Update(context.Background(), userB, view.ID, resource.UpdateRequest{Pinned: boolPtr(false)})
	assert.ErrorIs(t, err, resource.ErrResourceForbidden)

	cleared, err := f.svc.Update(context.Background(), userA, view.ID, resource.UpdateRequest{CategoryID: strPtr("")})
	require.NoError(t, err)
	assert.Nil(t, cleared.Category)
}

func TestV2_CategoryCRUDAndDeleteClearsResources(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	cats := newMemCategories(f.res)
	f.svc = f.svc.WithCategoryRepository(cats)

	created, err := f.svc.CreateCategory(context.Background(), " 新分类 ", nil)
	require.NoError(t, err)
	assert.Equal(t, "新分类", created.Name)
	assert.True(t, created.Active)

	_, err = f.svc.CreateCategory(context.Background(), "新分类", nil)
	assert.ErrorIs(t, err, resource.ErrCategoryExists)
	_, err = f.svc.CreateCategory(context.Background(), "   ", nil)
	assert.ErrorIs(t, err, resource.ErrCategoryNameEmpty)

	renamed, err := f.svc.UpdateCategory(context.Background(), created.ID, resource.UpdateCategoryRequest{
		Name: strPtr("改名"), Sort: intPtr(9), Active: boolPtr(false),
	})
	require.NoError(t, err)
	assert.Equal(t, "改名", renamed.Name)
	assert.Equal(t, 9, renamed.Sort)
	assert.False(t, renamed.Active)

	_, err = f.svc.UpdateCategory(context.Background(), created.ID, resource.UpdateCategoryRequest{Name: strPtr("")})
	assert.ErrorIs(t, err, resource.ErrCategoryNameEmpty)
	_, err = f.svc.UpdateCategory(context.Background(), "missing", resource.UpdateCategoryRequest{})
	assert.ErrorIs(t, err, resource.ErrCategoryNotFound)

	activeList, err := f.svc.ListCategories(context.Background(), false)
	require.NoError(t, err)
	assert.Empty(t, activeList, "includeInactive=false 只返回启用分类")
	created.Active = true
	activeList, err = f.svc.ListCategories(context.Background(), false)
	require.NoError(t, err)
	assert.Len(t, activeList, 1)

	res, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "待清分类", URL: "https://example.com/clear", CategoryID: strPtr(created.ID),
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteCategory(context.Background(), created.ID))
	got, err := f.svc.Get(context.Background(), res.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Category)
	assert.ErrorIs(t, f.svc.DeleteCategory(context.Background(), created.ID), resource.ErrCategoryNotFound)
}

func TestV2_List_InvalidSort(t *testing.T) {
	f := newFixture(t)
	_, _, err := f.svc.List(context.Background(), resource.ListFilter{Sort: "bogus"})
	assert.ErrorIs(t, err, resource.ErrInvalidSort)

	_, _, err = f.svc.List(context.Background(), resource.ListFilter{Sort: "useful", PageSize: 20})
	assert.NoError(t, err)
}

func intPtr(i int) *int { return &i }
