package resource_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

func (r *memUserRepo) Create(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }
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
	byID    map[string]*resource.Resource
	resTags map[string][]string
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

func (m *memFileStore) Save(reader io.Reader, filename string) (string, int64, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", 0, err
	}
	path := "uploads/" + filename
	m.files[path] = string(data)
	return path, int64(len(data)), nil
}

func (m *memFileStore) Open(path string) (io.ReadSeekCloser, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &memReadSeekCloser{Reader: strings.NewReader(data)}, nil
}

func (m *memFileStore) Delete(path string) error {
	delete(m.files, path)
	return nil
}

type memReadSeekCloser struct {
	*strings.Reader
}

func (m *memReadSeekCloser) Close() error { return nil }

// ---- 夹具 ----

type fixture struct {
	svc   *resource.Service
	users *memUserRepo
	tags  *memTagRepo
	res   *memResourceRepo
	files *memFileStore
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

// ---- F7:创建 link ----

func TestCreateLink_Success(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	tg := f.seedTag("文献")

	view, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "好文章", URL: "https://example.com/a", Description: "值得读", TagIDs: []string{tg.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, resource.TypeLink, view.Type)
	assert.Equal(t, "https://example.com/a", view.URL)
	assert.Equal(t, "值得读", view.Description)
	assert.False(t, view.Preview.Supported, "link 不支持预览")
	assert.Empty(t, view.DownloadURL, "link 无下载地址")
	require.Len(t, view.Tags, 1)
	assert.NotNil(t, view.Uploader)
}

func TestCreateLink_Validation(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "", URL: "https://e.com"})
	assert.ErrorIs(t, err, resource.ErrTitleEmpty)
	_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: ""})
	assert.ErrorIs(t, err, resource.ErrURLRequired)

	// 非法协议/格式
	for _, bad := range []string{
		"javascript:alert(1)", "ftp://example.com/x", "file:///etc/passwd",
		"not-a-url", "//example.com", "https://", "http://", "data:text/html,x",
	} {
		_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: bad})
		assert.ErrorIs(t, err, resource.ErrInvalidURL, "URL=%q", bad)
	}

	// 合法 http/https
	for _, good := range []string{
		"http://example.com", "https://example.com/a?b=c#d", "https://sub.example.com:8443/x",
	} {
		_, err = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: good})
		assert.NoError(t, err, "URL=%q", good)
	}
}

func TestCreate_TagNotFound(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{
		Title: "x", URL: "https://e.com", TagIDs: []string{"no-such"},
	})
	assert.ErrorIs(t, err, tag.ErrTagNotFound)
}

// ---- F7:上传 file ----

func TestUploadFile_Success_PDF(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	view, err := f.svc.UploadFile(context.Background(), userA, "论文.pdf",
		strings.NewReader("%PDF-1.4 test content"), "论文", "第一篇文献", nil)
	require.NoError(t, err)
	assert.Equal(t, resource.TypeFile, view.Type)
	assert.Equal(t, "论文", view.Title)
	assert.Equal(t, "第一篇文献", view.Description)
	assert.Equal(t, "论文.pdf", view.OriginalName)
	assert.Equal(t, "application/pdf", view.MimeType)
	assert.Greater(t, view.FileSize, int64(0))
	assert.NotEmpty(t, view.FilePath)
	assert.Contains(t, f.files.files, view.FilePath)
	assert.True(t, view.Preview.Supported)
	assert.Equal(t, "pdf", view.Preview.Type)
	assert.Contains(t, view.Preview.URL, "/preview")
	assert.Contains(t, view.DownloadURL, "/download")
}

func TestUploadFile_Success_Image(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	// 1x1 PNG
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}

	view, err := f.svc.UploadFile(context.Background(), userA, "fig.png",
		bytes.NewReader(png), "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "image/png", view.MimeType)
	assert.True(t, view.Preview.Supported)
	assert.Equal(t, "image", view.Preview.Type)
}

func TestUploadFile_Success_Text(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	view, err := f.svc.UploadFile(context.Background(), userA, "notes.md",
		strings.NewReader("# 组会记录\n- 事项"), "", "", nil)
	require.NoError(t, err)
	assert.True(t, view.Preview.Supported)
	assert.Equal(t, "text", view.Preview.Type)
}

func TestUploadFile_Success_Video(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	// MP4:offset 4 为 "ftyp"
	mp4 := append([]byte{0x00, 0x00, 0x00, 0x18}, []byte("ftypisom")...)

	view, err := f.svc.UploadFile(context.Background(), userA, "demo.mp4",
		bytes.NewReader(mp4), "", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", view.MimeType)
	assert.True(t, view.Preview.Supported)
	assert.Equal(t, "video", view.Preview.Type)
}

func TestUploadFile_BadExt(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	_, err := f.svc.UploadFile(context.Background(), userA, "evil.exe",
		strings.NewReader("MZ"), "", "", nil)
	assert.ErrorIs(t, err, resource.ErrFileTypeNotAllowed)
}

func TestUploadFile_MimeMismatch(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	// 改名的可执行文件:扩展名 pdf,内容不是 PDF
	_, err := f.svc.UploadFile(context.Background(), userA, "fake.pdf",
		strings.NewReader("MZ\x90\x00exe payload"), "", "", nil)
	assert.ErrorIs(t, err, resource.ErrFileContentMismatch)
}

func TestUploadFile_TooLarge(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	// 普通文件 > 50MB
	big := &repeatReader{prefix: "%PDF-1.4\n", n: resource.MaxFileSize + 1}
	_, err := f.svc.UploadFile(context.Background(), userA, "big.pdf", big, "", "", nil)
	assert.ErrorIs(t, err, resource.ErrFileTooLarge)
	// 超限时不留孤儿文件
	assert.Empty(t, f.files.files)
}

func TestUploadFile_VideoTooLarge(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	// 视频 > 100MB
	big := &repeatReader{prefix: "\x00\x00\x00\x18ftypisom", n: resource.MaxVideoSize + 1}
	_, err := f.svc.UploadFile(context.Background(), userA, "big.mp4", big, "", "", nil)
	assert.ErrorIs(t, err, resource.ErrFileTooLarge)
	assert.Empty(t, f.files.files)
}

// repeatReader 先输出 prefix,再输出 'a' 直至共 n 字节(避免大内存分配)。
type repeatReader struct {
	prefix string
	n      int64
	wrote  int64
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.wrote >= r.n {
		return 0, io.EOF
	}
	remaining := r.n - r.wrote
	// 先给 prefix
	pre := []byte(r.prefix)
	if r.wrote < int64(len(pre)) {
		c := copy(p, pre[r.wrote:])
		if int64(c) > remaining {
			c = int(remaining)
		}
		r.wrote += int64(c)
		return c, nil
	}
	c := copy(p, bytes.Repeat([]byte("a"), len(p)))
	if int64(c) > remaining {
		c = int(remaining)
	}
	r.wrote += int64(c)
	return c, nil
}

// ---- F7:列表/详情 ----

func TestList_FilterByTypeAndKeyword(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	_, _ = f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "深度学习入门", URL: "https://a.com"})
	_, _ = f.svc.UploadFile(context.Background(), userA, "attention.pdf", strings.NewReader("%PDF-1.4 a"), "Attention Is All", "", nil)

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

// ---- F7:打开文件(下载/预览) ----

func TestOpenFile_OnlyFileType(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)

	link, _ := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: "https://a.com"})
	_, _, err := f.svc.OpenFile(context.Background(), link.ID)
	assert.ErrorIs(t, err, resource.ErrNotFile)

	file, _ := f.svc.UploadFile(context.Background(), userA, "a.pdf", strings.NewReader("%PDF-1.4 x"), "", "", nil)
	_, rc, err := f.svc.OpenFile(context.Background(), file.ID)
	require.NoError(t, err)
	defer rc.Close()
	head := make([]byte, 8)
	_, _ = io.ReadFull(rc, head)
	assert.Equal(t, "%PDF-1.4", string(head))
}

func TestOpenFile_NotFound(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	_, _, err := f.svc.OpenFile(context.Background(), "no-such")
	assert.ErrorIs(t, err, resource.ErrResourceNotFound)
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

	// 上传者 → OK(含 description)
	title2 := "作者改"
	desc := "新描述"
	updated2, err := f.svc.Update(context.Background(), userA, view.ID, resource.UpdateRequest{Title: &title2, Description: &desc})
	require.NoError(t, err)
	assert.Equal(t, "作者改", updated2.Title)
	assert.Equal(t, "新描述", updated2.Description)
}

func TestDelete_OnlyUploaderOrAdmin(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	f.seedUser("user-b", user.RoleStudent)
	f.seedUser(adminID, user.RoleAdmin)
	view, _ := f.svc.UploadFile(context.Background(), userA, "a.pdf", strings.NewReader("%PDF-1.4 x"), "", "", nil)

	// 他人 → 403
	assert.ErrorIs(t, f.svc.Delete(context.Background(), "user-b", view.ID), resource.ErrResourceForbidden)

	// 上传者 → 204 且文件删除
	require.NoError(t, f.svc.Delete(context.Background(), userA, view.ID))
	assert.NotContains(t, f.files.files, view.FilePath)

	// admin 删另一个
	view2, _ := f.svc.UploadFile(context.Background(), userA, "b.pdf", strings.NewReader("%PDF-1.4 y"), "", "", nil)
	require.NoError(t, f.svc.Delete(context.Background(), adminID, view2.ID))
}

func TestDelete_LinkNoFile(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, user.RoleStudent)
	view, _ := f.svc.CreateLink(context.Background(), userA, resource.CreateLinkRequest{Title: "x", URL: "https://a.com"})
	require.NoError(t, f.svc.Delete(context.Background(), userA, view.ID))
}

func strPtr(s string) *string { return &s }

var _ = http.MethodGet
