package resource_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/resource"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// ---- 内存替身 ----

type memUserRepo struct {
	byID map[string]*user.User
}

func newMemUsers() *memUserRepo { return &memUserRepo{byID: map[string]*user.User{}} }

func (r *memUserRepo) seed(id, role string) *user.User {
	u := &user.User{ID: id, Username: id, DisplayName: id, Role: role}
	r.byID[id] = u
	return u
}

func (r *memUserRepo) GetByID(_ context.Context, id string) (*user.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return u, nil
}

func (r *memUserRepo) GetByIDs(_ context.Context, ids []string) ([]*user.User, error) {
	var out []*user.User
	for _, id := range ids {
		if u, ok := r.byID[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

func (r *memUserRepo) Create(_ context.Context, u *user.User) error     { r.byID[u.ID] = u; return nil }
func (r *memUserRepo) GetByUsername(_ context.Context, _ string) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *memUserRepo) Update(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }

type memTagRepo struct {
	byID    map[string]*tag.Tag
	resTags map[string][]string // 与 memResourceRepo 共享
}

func newMemTags() *memTagRepo {
	return &memTagRepo{byID: map[string]*tag.Tag{}, resTags: map[string][]string{}}
}

func (r *memTagRepo) Create(_ context.Context, t *tag.Tag) error { r.byID[t.ID] = t; return nil }
func (r *memTagRepo) GetByID(_ context.Context, id string) (*tag.Tag, error) {
	t, ok := r.byID[id]
	if !ok {
		return nil, tag.ErrNotFound
	}
	return t, nil
}
func (r *memTagRepo) GetByName(_ context.Context, _ string) (*tag.Tag, error) {
	return nil, tag.ErrNotFound
}
func (r *memTagRepo) List(_ context.Context) ([]*tag.Tag, error) { return nil, nil }
func (r *memTagRepo) ListByDocumentIDs(_ context.Context, _ []string) (map[string][]*tag.Tag, error) {
	return nil, nil
}
func (r *memTagRepo) ListDocumentIDsByTag(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (r *memTagRepo) ListByResourceIDs(_ context.Context, resourceIDs []string) (map[string][]*tag.Tag, error) {
	out := map[string][]*tag.Tag{}
	for _, resID := range resourceIDs {
		for _, tid := range r.resTags[resID] {
			if t, ok := r.byID[tid]; ok {
				out[resID] = append(out[resID], t)
			}
		}
	}
	return out, nil
}

type memResourceRepo struct {
	byID     map[string]*resource.Resource
	resTags  map[string][]string
}

func newMemResources() *memResourceRepo {
	return &memResourceRepo{byID: map[string]*resource.Resource{}, resTags: map[string][]string{}}
}

func (r *memResourceRepo) Create(_ context.Context, res *resource.Resource) error {
	r.byID[res.ID] = res
	return nil
}

func (r *memResourceRepo) GetByID(_ context.Context, id string) (*resource.Resource, error) {
	res, ok := r.byID[id]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return res, nil
}

func (r *memResourceRepo) Update(_ context.Context, res *resource.Resource) error {
	r.byID[res.ID] = res
	return nil
}

func (r *memResourceRepo) Delete(_ context.Context, id string) error {
	delete(r.byID, id)
	return nil
}

func (r *memResourceRepo) List(_ context.Context, f resource.ListFilter) ([]*resource.Resource, int64, error) {
	var out []*resource.Resource
	for _, res := range r.byID {
		if f.Type != "" && res.Type != f.Type {
			continue
		}
		if f.Keyword != "" && !strings.Contains(strings.ToLower(res.Title), strings.ToLower(f.Keyword)) {
			continue
		}
		if f.TagID != "" {
			found := false
			for _, tid := range r.resTags[res.ID] {
				if tid == f.TagID {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, res)
	}
	return out, int64(len(out)), nil
}

func (r *memResourceRepo) SetTags(_ context.Context, resourceID string, tagIDs []string) error {
	r.resTags[resourceID] = tagIDs
	return nil
}

type memFileStore struct {
	files map[string]string
}

func newMemFileStore() *memFileStore { return &memFileStore{files: map[string]string{}} }

func (m *memFileStore) Save(reader io.Reader, filename string) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	path := "uploads/" + filename
	m.files[path] = string(data)
	return path, nil
}

func (m *memFileStore) Delete(path string) error {
	delete(m.files, path)
	return nil
}

// ---- 夹具 ----

type fixture struct {
	svc    *resource.Service
	users  *memUserRepo
	tags   *memTagRepo
	res    *memResourceRepo
	files  *memFileStore
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		users: newMemUsers(), tags: newMemTags(),
		res: newMemResources(), files: newMemFileStore(),
	}
	sharedResTags := map[string][]string{}
	f.tags.resTags = sharedResTags
	f.res.resTags = sharedResTags
	f.svc = resource.NewService(f.res, f.tags, f.files, f.users)
	return f
}

func (f *fixture) seedUser(id, role string) *user.User { return f.users.seed(id, role) }
func (f *fixture) seedTag(name string) *tag.Tag {
	t := tag.NewTag(name, "")
	_ = f.tags.Create(context.Background(), t)
	return t
}

const (
	userA   = "user-a"
	adminID = "user-admin"
)

// ---- F7:创建 ----

func TestCreateLink_Success(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	tg := f.seedTag("文献")

	view, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "好文章", URL: "https://example.com/a", TagIDs: []string{tg.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, resource.TypeLink, view.Type)
	assert.Equal(t, "https://example.com/a", view.URL)
	require.Len(t, view.Tags, 1)
	assert.NotNil(t, view.Uploader)
}

func TestCreateLink_Validation(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "", URL: "x"})
	assert.ErrorIs(t, err, resource.ErrTitleEmpty)
	_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: ""})
	assert.ErrorIs(t, err, resource.ErrURLRequired)
}

func TestCreatePaper_Success(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	view, err := f.svc.CreatePaper(context.Background(), userA, resource.CreatePaperRequest{
		Title: "Paper X", DOI: "10.1000/xyz",
	})
	require.NoError(t, err)
	assert.Equal(t, resource.TypePaper, view.Type)
	assert.Equal(t, "10.1000/xyz", view.DOI)
}

func TestCreatePaper_NeedDOIOrArxiv(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.CreatePaper(context.Background(), userA, resource.CreatePaperRequest{Title: "x"})
	assert.ErrorIs(t, err, resource.ErrDOIOrArxivRequired)
}

func TestUploadFile_Success(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	view, err := f.svc.UploadFile(context.Background(), userA, "paper.pdf",
		strings.NewReader("%PDF-1.4 test"), "", nil)
	require.NoError(t, err)
	assert.Equal(t, resource.TypeFile, view.Type)
	assert.NotEmpty(t, view.FilePath)
	assert.Contains(t, f.files.files, view.FilePath)
}

func TestUploadFile_BadExt(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.UploadFile(context.Background(), userA, "evil.exe",
		strings.NewReader("MZ"), "", nil)
	assert.ErrorIs(t, err, resource.ErrFileTypeNotAllowed)
}

func TestCreate_TagNotFound(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "x", URL: "https://e.com", TagIDs: []string{"no-such"},
	})
	assert.ErrorIs(t, err, tag.ErrTagNotFound)
}

// ---- F7:列表/详情 ----

func TestList_FilterByTypeAndKeyword(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	_, _ = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "深度学习入门", URL: "https://a.com"})
	_, _ = f.svc.CreatePaper(context.Background(), userA, resource.CreatePaperRequest{Title: "Attention Is All", DOI: "10.1/x"})

	list, total, err := f.svc.List(context.Background(), resource.ListFilter{Type: resource.TypeLink})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "深度学习入门", list[0].Title)

	list2, total2, _ := f.svc.List(context.Background(), resource.ListFilter{Keyword: "attention"})
	assert.Equal(t, int64(1), total2)
	assert.Equal(t, "Attention Is All", list2[0].Title)
}

func TestGet_AnyUser(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser("user-b", user.RoleStudent)
	view, _ := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: "https://a.com"})

	got, err := f.svc.Get(context.Background(), view.ID)
	require.NoError(t, err)
	assert.Equal(t, view.ID, got.ID)
}

// ---- F7:修改/删除权限 ----

func TestUpdate_OnlyUploaderOrAdmin(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser("user-b", user.RoleStudent)
	f.seedUser(adminID, user.RoleAdmin)
	view, _ := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "原题", URL: "https://a.com"})

	// 他人 → 403
	_, err := f.svc.Update(context.Background(), "user-b", view.ID, resource.UpdateRequest{Title: strPtr("hack")})
	assert.ErrorIs(t, err, resource.ErrResourceForbidden)

	// admin → OK
	newTitle := "admin改"
	updated, err := f.svc.Update(context.Background(), adminID, view.ID, resource.UpdateRequest{Title: &newTitle})
	require.NoError(t, err)
	assert.Equal(t, "admin改", updated.Title)

	// 上传者 → OK
	title2 := "作者改"
	updated2, err := f.svc.Update(context.Background(), userA, view.ID, resource.UpdateRequest{Title: &title2})
	require.NoError(t, err)
	assert.Equal(t, "作者改", updated2.Title)
}

func TestDelete_OnlyUploaderOrAdmin(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser("user-b", user.RoleStudent)
	f.seedUser(adminID, user.RoleAdmin)
	view, _ := f.svc.UploadFile(context.Background(), userA, "a.pdf", strings.NewReader("x"), "", nil)

	// 他人 → 403
	assert.ErrorIs(t, f.svc.Delete(context.Background(), "user-b", view.ID), resource.ErrResourceForbidden)

	// 上传者 → 204 且文件删除
	require.NoError(t, f.svc.Delete(context.Background(), userA, view.ID))
	assert.NotContains(t, f.files.files, view.FilePath)

	// admin 删另一个
	view2, _ := f.svc.UploadFile(context.Background(), userA, "b.pdf", strings.NewReader("y"), "", nil)
	require.NoError(t, f.svc.Delete(context.Background(), adminID, view2.ID))
}

// ---- F8:元数据抓取(mock 外部服务) ----

func crossrefHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/works/10.1000%2Fxyz", "/works/10.1000/xyz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":{
				"title":["Crossref Paper"],
				"author":[{"given":"Alice","family":"Smith"},{"given":"Bob","family":"Lee"}],
				"container-title":["Journal of Testing"],
				"published-print":{"date-parts":[[2024]]},
				"DOI":"10.1000/xyz"
			}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestFetchPaperMeta_Crossref(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(crossrefHandler(t))
	defer srv.Close()
	f.svc.WithEndpoints(srv.Client(), srv.URL, srv.URL)

	meta, err := f.svc.FetchPaperMeta(context.Background(), "10.1000/xyz", "")
	require.NoError(t, err)
	assert.Equal(t, "Crossref Paper", meta.Title)
	require.Len(t, meta.Authors, 2)
	assert.Equal(t, "Alice Smith", meta.Authors[0])
	assert.Equal(t, "Journal of Testing", meta.Journal)
	assert.Equal(t, 2024, meta.Year)
	assert.Equal(t, "10.1000/xyz", meta.DOI)
}

func TestFetchPaperMeta_Arxiv(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/query")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>Arxiv Paper Title</title>
    <author><name>Carol White</name></author>
    <published>2023-06-01T00:00:00Z</published>
    <id>http://arxiv.org/abs/2306.00123</id>
  </entry>
</feed>`))
	}))
	defer srv.Close()
	f.svc.WithEndpoints(srv.Client(), srv.URL, srv.URL)

	meta, err := f.svc.FetchPaperMeta(context.Background(), "", "2306.00123")
	require.NoError(t, err)
	assert.Equal(t, "Arxiv Paper Title", meta.Title)
	require.Len(t, meta.Authors, 1)
	assert.Equal(t, "Carol White", meta.Authors[0])
	assert.Equal(t, 2023, meta.Year)
	assert.Equal(t, "2306.00123", meta.ArxivID)
}

func TestFetchPaperMeta_NotFound(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	f.svc.WithEndpoints(srv.Client(), srv.URL, srv.URL)

	_, err := f.svc.FetchPaperMeta(context.Background(), "10.9999/missing", "")
	assert.ErrorIs(t, err, resource.ErrPaperMetaNotFound)
}

func TestFetchPaperMeta_UpstreamError(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	f.svc.WithEndpoints(srv.Client(), srv.URL, srv.URL)

	_, err := f.svc.FetchPaperMeta(context.Background(), "10.1000/xyz", "")
	assert.ErrorIs(t, err, resource.ErrPaperMetaUpstream)
}

func TestFetchPaperMeta_RequireParam(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.FetchPaperMeta(context.Background(), "", "")
	assert.ErrorIs(t, err, resource.ErrDOIOrArxivRequired)
}

func strPtr(s string) *string { return &s }

var _ = errors.Is
