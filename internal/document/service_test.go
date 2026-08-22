package document_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/document"
	"labnexus/internal/space"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// ---- 内存替身 ----

type memUserRepo struct {
	byID map[string]*user.User
}

func newMemUsers() *memUserRepo { return &memUserRepo{byID: map[string]*user.User{}} }

func (r *memUserRepo) seed(u *user.User) { r.byID[u.ID] = u }

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

func (r *memUserRepo) Create(_ context.Context, u *user.User) error    { r.byID[u.ID] = u; return nil }
func (r *memUserRepo) GetByUsername(_ context.Context, _ string) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *memUserRepo) Update(_ context.Context, u *user.User) error { r.byID[u.ID] = u; return nil }

type memSpaceRepo struct {
	byUser map[string]*space.Space
}

func newMemSpaces() *memSpaceRepo { return &memSpaceRepo{byUser: map[string]*space.Space{}} }

func (r *memSpaceRepo) Create(_ context.Context, s *space.Space) error { r.byUser[s.UserID] = s; return nil }
func (r *memSpaceRepo) GetByUserID(_ context.Context, userID string) (*space.Space, error) {
	s, ok := r.byUser[userID]
	if !ok {
		return nil, space.ErrNotFound
	}
	return s, nil
}

type memFolderRepo struct {
	byID map[string]*space.Folder
}

func newMemFolders() *memFolderRepo { return &memFolderRepo{byID: map[string]*space.Folder{}} }

func (r *memFolderRepo) Create(_ context.Context, f *space.Folder) error { r.byID[f.ID] = f; return nil }
func (r *memFolderRepo) GetByID(_ context.Context, id string) (*space.Folder, error) {
	f, ok := r.byID[id]
	if !ok {
		return nil, space.ErrNotFound
	}
	return f, nil
}
func (r *memFolderRepo) ListBySpace(_ context.Context, _ string) ([]*space.Folder, error) { return nil, nil }
func (r *memFolderRepo) Update(_ context.Context, f *space.Folder) error {
	r.byID[f.ID] = f
	return nil
}
func (r *memFolderRepo) Delete(_ context.Context, _ string) error { return nil }
func (r *memFolderRepo) CountChildren(_ context.Context, _ string) (int64, error) { return 0, nil }

type memTagRepo struct {
	byID    map[string]*tag.Tag
	docTags map[string][]string // docID -> tagIDs
}

func newMemTags() *memTagRepo {
	return &memTagRepo{byID: map[string]*tag.Tag{}, docTags: map[string][]string{}}
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
func (r *memTagRepo) ListByDocumentIDs(_ context.Context, docIDs []string) (map[string][]*tag.Tag, error) {
	out := map[string][]*tag.Tag{}
	for _, docID := range docIDs {
		for _, tagID := range r.docTags[docID] {
			if t, ok := r.byID[tagID]; ok {
				out[docID] = append(out[docID], t)
			}
		}
	}
	return out, nil
}
func (r *memTagRepo) ListDocumentIDsByTag(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

type memReactionRepo struct {
	rows map[string]*document.Reaction // id -> reaction
}

func newMemReactions() *memReactionRepo { return &memReactionRepo{rows: map[string]*document.Reaction{}} }

func (r *memReactionRepo) countByDoc(docID string) int64 {
	var n int64
	for _, re := range r.rows {
		if re.DocumentID == docID {
			n++
		}
	}
	return n
}

func (r *memReactionRepo) Find(_ context.Context, docID, userID, emoji string) (*document.Reaction, error) {
	for _, re := range r.rows {
		if re.DocumentID == docID && re.UserID == userID && re.Emoji == emoji {
			return re, nil
		}
	}
	return nil, document.ErrNotFound
}

func (r *memReactionRepo) Create(_ context.Context, re *document.Reaction) error {
	r.rows[re.ID] = re
	return nil
}

func (r *memReactionRepo) Delete(_ context.Context, id string) error {
	delete(r.rows, id)
	return nil
}

type memCommentRepo struct {
	byID map[string]*document.Comment
}

func newMemComments() *memCommentRepo { return &memCommentRepo{byID: map[string]*document.Comment{}} }

func (r *memCommentRepo) countByDoc(docID string) int64 {
	var n int64
	for _, c := range r.byID {
		if c.DocumentID == docID {
			n++
		}
	}
	return n
}

func (r *memCommentRepo) Create(_ context.Context, c *document.Comment) error { r.byID[c.ID] = c; return nil }
func (r *memCommentRepo) GetByID(_ context.Context, id string) (*document.Comment, error) {
	c, ok := r.byID[id]
	if !ok {
		return nil, document.ErrNotFound
	}
	return c, nil
}
func (r *memCommentRepo) Delete(_ context.Context, id string) error { delete(r.byID, id); return nil }
func (r *memCommentRepo) ListByDocument(_ context.Context, docID string) ([]*document.Comment, error) {
	var out []*document.Comment
	for _, c := range r.byID {
		if c.DocumentID == docID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

type memDocRepo struct {
	byID      map[string]*document.Document
	docTags   map[string][]string
	reactions *memReactionRepo
	comments  *memCommentRepo
}

func newMemDocs(reactions *memReactionRepo, comments *memCommentRepo) *memDocRepo {
	return &memDocRepo{
		byID: map[string]*document.Document{}, docTags: map[string][]string{},
		reactions: reactions, comments: comments,
	}
}

func (r *memDocRepo) Create(_ context.Context, d *document.Document) error { r.byID[d.ID] = d; return nil }
func (r *memDocRepo) GetByID(_ context.Context, id string) (*document.Document, error) {
	d, ok := r.byID[id]
	if !ok {
		return nil, document.ErrNotFound
	}
	return d, nil
}
func (r *memDocRepo) Update(_ context.Context, d *document.Document) error {
	r.byID[d.ID] = d
	return nil
}
func (r *memDocRepo) SoftDelete(_ context.Context, id string) error { delete(r.byID, id); return nil }

func (r *memDocRepo) ListBySpace(_ context.Context, spaceID string, folderID *string, visibility string) ([]*document.Document, error) {
	var out []*document.Document
	for _, d := range r.byID {
		if d.SpaceID != spaceID {
			continue
		}
		if folderID != nil && (d.FolderID == nil || *d.FolderID != *folderID) {
			continue
		}
		if visibility != "" && d.Visibility != visibility {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *memDocRepo) ListPublic(_ context.Context, sortMode string, offset, limit int) ([]*document.Document, int64, error) {
	var out []*document.Document
	for _, d := range r.byID {
		if d.Visibility == document.VisibilityPublic {
			out = append(out, d)
		}
	}
	switch sortMode {
	case "hot":
		sort.Slice(out, func(i, j int) bool {
			ci, cj := r.reactions.countByDoc(out[i].ID), r.reactions.countByDoc(out[j].ID)
			if ci != cj {
				return ci > cj
			}
			return out[i].CreatedAt.After(out[j].CreatedAt)
		})
	default:
		sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	}
	total := int64(len(out))
	start := offset
	if start > len(out) {
		start = len(out)
	}
	end := start + limit
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], total, nil
}

func (r *memDocRepo) ListByTag(_ context.Context, tagID string) ([]*document.Document, error) {
	var out []*document.Document
	for docID, tagIDs := range r.docTags {
		for _, id := range tagIDs {
			if id == tagID {
				if d, ok := r.byID[docID]; ok {
					out = append(out, d)
				}
			}
		}
	}
	return out, nil
}

func (r *memDocRepo) CountByFolder(_ context.Context, folderID string) (int64, error) {
	var n int64
	for _, d := range r.byID {
		if d.FolderID != nil && *d.FolderID == folderID {
			n++
		}
	}
	return n, nil
}

func (r *memDocRepo) ReactionStats(_ context.Context, docIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, id := range docIDs {
		out[id] = r.reactions.countByDoc(id)
	}
	return out, nil
}

func (r *memDocRepo) CommentStats(_ context.Context, docIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, id := range docIDs {
		out[id] = r.comments.countByDoc(id)
	}
	return out, nil
}

func (r *memDocRepo) SetTags(_ context.Context, docID string, tagIDs []string) error {
	r.docTags[docID] = tagIDs
	return nil
}

// ---- 夹具 ----

type fixture struct {
	svc     *document.Service
	users   *memUserRepo
	spaces  *memSpaceRepo
	folders *memFolderRepo
	tags    *memTagRepo
	docs    *memDocRepo
	reacs   *memReactionRepo
	comms   *memCommentRepo
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		users: newMemUsers(), spaces: newMemSpaces(), folders: newMemFolders(),
		tags: newMemTags(), reacs: newMemReactions(), comms: newMemComments(),
	}
	// docTags 由 document 与 tag 替身共享,保证 SetTags 与 ListByDocumentIDs 一致
	sharedDocTags := map[string][]string{}
	f.tags.docTags = sharedDocTags
	f.docs = newMemDocs(f.reacs, f.comms)
	f.docs.docTags = sharedDocTags
	f.svc = document.NewService(f.docs, f.comms, f.reacs, f.tags, f.users, f.spaces, f.folders)
	return f
}

func (f *fixture) seedUser(id, name string) *user.User {
	u := &user.User{ID: id, Username: name, DisplayName: name, Role: "student", CreatedAt: time.Now()}
	f.users.seed(u)
	return u
}

func (f *fixture) seedSpace(userID string) *space.Space {
	s := space.NewSpace(userID)
	_ = f.spaces.Create(context.Background(), s)
	return s
}

func (f *fixture) seedFolder(spaceID string, name string) *space.Folder {
	folder := space.NewFolder(spaceID, nil, name, 0)
	_ = f.folders.Create(context.Background(), folder)
	return folder
}

func (f *fixture) seedTag(name string) *tag.Tag {
	t := tag.NewTag(name, "")
	_ = f.tags.Create(context.Background(), t)
	return t
}

func (f *fixture) seedDoc(authorID, spaceID string, folderID *string, title, vis string) *document.Document {
	d := document.NewDocument(authorID, spaceID, folderID, title, "content", vis)
	_ = f.docs.Create(context.Background(), d)
	return d
}

func (f *fixture) byTitle(title string) *document.Document {
	for _, d := range f.docs.byID {
		if d.Title == title {
			return d
		}
	}
	return nil
}

const (
	userA = "user-a"
	userB = "user-b"
)

// ---- F3:创建 ----

func TestCreateDocument_Success(t *testing.T) {
	f := newFixture(t)
	u := f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	folder := f.seedFolder(sp.ID, "会议记录")
	tg := f.seedTag("组会")

	view, err := f.svc.CreateDocument(context.Background(), userA, document.CreateDocumentRequest{
		FolderID: &folder.ID, Title: "组会纪要", Content: "## 议题", Visibility: "private",
		TagIDs: []string{tg.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, u.ID, view.AuthorID)
	assert.Equal(t, "组会纪要", view.Title)
	assert.Equal(t, "private", view.Visibility)
	assert.Len(t, view.Tags, 1)
}

func TestCreateDocument_EmptyTitle(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedSpace(userA)

	_, err := f.svc.CreateDocument(context.Background(), userA, document.CreateDocumentRequest{Title: "  "})
	assert.ErrorIs(t, err, document.ErrTitleEmpty)
}

func TestCreateDocument_InvalidVisibility(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedSpace(userA)

	_, err := f.svc.CreateDocument(context.Background(), userA, document.CreateDocumentRequest{
		Title: "x", Visibility: "secret",
	})
	assert.ErrorIs(t, err, document.ErrInvalidVisibility)
}

func TestCreateDocument_FolderNotOwned(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	f.seedSpace(userA)
	spB := f.seedSpace(userB)
	otherFolder := f.seedFolder(spB.ID, "别人的")

	_, err := f.svc.CreateDocument(context.Background(), userA, document.CreateDocumentRequest{
		Title: "x", FolderID: &otherFolder.ID, Visibility: document.VisibilityPrivate,
	})
	assert.ErrorIs(t, err, space.ErrFolderNotOwned)
}

func TestCreateDocument_TagNotFound(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedSpace(userA)

	_, err := f.svc.CreateDocument(context.Background(), userA, document.CreateDocumentRequest{
		Title: "x", Visibility: document.VisibilityPrivate, TagIDs: []string{"no-such-tag"},
	})
	assert.ErrorIs(t, err, tag.ErrTagNotFound)
}

// ---- F3:查看 ----

func TestGetDocument_AuthorSeesPrivate(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "私密笔记", document.VisibilityPrivate)

	view, err := f.svc.GetDocument(context.Background(), userA, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, doc.ID, view.ID)
	assert.NotNil(t, view.Author)
}

func TestGetDocument_OtherCannotSeePrivate(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "私密笔记", document.VisibilityPrivate)

	_, err := f.svc.GetDocument(context.Background(), userB, doc.ID)
	assert.ErrorIs(t, err, document.ErrDocumentNotFound)
}

func TestGetDocument_PublicVisibleToAll(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "公开帖", document.VisibilityPublic)

	view, err := f.svc.GetDocument(context.Background(), userB, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, doc.ID, view.ID)
}

// ---- F3:修改/删除 ----

func TestUpdateDocument_VisibilitySwitch(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "笔记", document.VisibilityPrivate)

	pub := document.VisibilityPublic
	view, err := f.svc.UpdateDocument(context.Background(), userA, doc.ID, document.UpdateDocumentRequest{
		Visibility: &pub,
	})
	require.NoError(t, err)
	assert.Equal(t, document.VisibilityPublic, view.Visibility)
}

func TestUpdateDocument_NotAuthor(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "笔记", document.VisibilityPublic)

	newTitle := "hack"
	_, err := f.svc.UpdateDocument(context.Background(), userB, doc.ID, document.UpdateDocumentRequest{Title: &newTitle})
	assert.ErrorIs(t, err, document.ErrDocumentForbidden)
}

func TestDeleteDocument_AuthorOnly(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "笔记", document.VisibilityPublic)

	require.ErrorIs(t, f.svc.DeleteDocument(context.Background(), userB, doc.ID), document.ErrDocumentForbidden)
	require.NoError(t, f.svc.DeleteDocument(context.Background(), userA, doc.ID))

	// 软删后列表/信息流不再出现
	views, _ := f.svc.ListMyDocuments(context.Background(), userA, nil, "")
	assert.Empty(t, views)
}

func TestListMyDocuments_FilterByFolder(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	folder := f.seedFolder(sp.ID, "会议记录")
	f.seedDoc(userA, sp.ID, &folder.ID, "在目录里", document.VisibilityPrivate)
	f.seedDoc(userA, sp.ID, nil, "不在目录", document.VisibilityPrivate)

	views, err := f.svc.ListMyDocuments(context.Background(), userA, &folder.ID, "")
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "在目录里", views[0].Title)
}

// ---- F5:标签内容页 ----

func TestListByTag_VisibilityFilter(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	spA := f.seedSpace(userA)
	spB := f.seedSpace(userB)
	tg := f.seedTag("投稿")

	// 本人公开、本人私有、他人公开、他人私有
	myPub := f.seedDoc(userA, spA.ID, nil, "A公开", document.VisibilityPublic)
	myPriv := f.seedDoc(userA, spA.ID, nil, "A私有", document.VisibilityPrivate)
	f.seedDoc(userB, spB.ID, nil, "B公开", document.VisibilityPublic)
	f.seedDoc(userB, spB.ID, nil, "B私有", document.VisibilityPrivate)
	for _, d := range []*document.Document{myPub, myPriv} {
		require.NoError(t, f.docs.SetTags(context.Background(), d.ID, []string{tg.ID}))
	}
	// 给 B 的文档打标(直接改替身数据)
	for _, d := range []*document.Document{f.byTitle("B公开"), f.byTitle("B私有")} {
		require.NoError(t, f.docs.SetTags(context.Background(), d.ID, []string{tg.ID}))
	}

	views, err := f.svc.ListByTag(context.Background(), userA, tg.ID)
	require.NoError(t, err)
	got := map[string]bool{}
	for _, v := range views {
		got[v.Title] = true
	}
	assert.True(t, got["A公开"], "本人公开应出现")
	assert.True(t, got["A私有"], "本人私有应出现")
	assert.True(t, got["B公开"], "他人公开应出现")
	assert.False(t, got["B私有"], "他人私有不应泄露")
}

// ---- F4:信息流 ----

func TestGetFeed_LatestOnlyPublic(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	f.seedDoc(userA, sp.ID, nil, "公开1", document.VisibilityPublic)
	f.seedDoc(userA, sp.ID, nil, "私有", document.VisibilityPrivate)
	time.Sleep(2 * time.Millisecond)
	f.seedDoc(userA, sp.ID, nil, "公开2", document.VisibilityPublic)

	views, total, err := f.svc.GetFeed(context.Background(), "latest", 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, views, 2)
	assert.Equal(t, "公开2", views[0].Title) // 倒序
	assert.Equal(t, "公开1", views[1].Title)
}

func TestGetFeed_HotSort(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	docA := f.seedDoc(userA, sp.ID, nil, "热门帖", document.VisibilityPublic)
	docB := f.seedDoc(userA, sp.ID, nil, "冷门帖", document.VisibilityPublic)
	// docA 点赞 2 次
	for _, uid := range []string{"u1", "u2"} {
		require.NoError(t, f.reacs.Create(context.Background(), document.NewReaction(docA.ID, uid, "👍")))
	}
	require.NoError(t, f.reacs.Create(context.Background(), document.NewReaction(docB.ID, "u1", "👍")))

	views, _, err := f.svc.GetFeed(context.Background(), "hot", 1, 20)
	require.NoError(t, err)
	require.Len(t, views, 2)
	assert.Equal(t, "热门帖", views[0].Title)
	assert.Equal(t, int64(2), views[0].ReactionsCount)
}

// ---- F4:点赞 ----

func TestToggleReaction_OnThenOff(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)

	require.NoError(t, f.svc.ToggleReaction(context.Background(), userA, doc.ID, "👍"))
	stats, _ := f.docs.ReactionStats(context.Background(), []string{doc.ID})
	assert.Equal(t, int64(1), stats[doc.ID])

	require.NoError(t, f.svc.ToggleReaction(context.Background(), userA, doc.ID, "👍"))
	stats, _ = f.docs.ReactionStats(context.Background(), []string{doc.ID})
	assert.Equal(t, int64(0), stats[doc.ID])
}

func TestToggleReaction_PrivateDoc(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "私有", document.VisibilityPrivate)

	err := f.svc.ToggleReaction(context.Background(), userB, doc.ID, "👍")
	assert.ErrorIs(t, err, document.ErrDocumentNotFound)
}

// ---- F4:评论 ----

func TestCreateComment_Success(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)

	view, err := f.svc.CreateComment(context.Background(), userB, doc.ID, "好帖", nil)
	require.NoError(t, err)
	assert.Equal(t, "好帖", view.Content)
	assert.Equal(t, userB, view.Author.ID)
}

func TestCreateComment_EmptyContent(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)

	_, err := f.svc.CreateComment(context.Background(), userA, doc.ID, "  ", nil)
	assert.ErrorIs(t, err, document.ErrContentEmpty)
}

func TestCreateComment_PrivateDocForbidden(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "私有", document.VisibilityPrivate)

	_, err := f.svc.CreateComment(context.Background(), userB, doc.ID, "x", nil)
	assert.ErrorIs(t, err, document.ErrDocumentNotFound)
}

func TestCreateComment_InvalidReply(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)

	bad := "no-such-comment"
	_, err := f.svc.CreateComment(context.Background(), userA, doc.ID, "回复", &bad)
	assert.ErrorIs(t, err, document.ErrInvalidReply)
}

func TestDeleteComment_AuthorOnly(t *testing.T) {
	f := newFixture(t)
	f.seedUser(userA, "alice")
	f.seedUser(userB, "bob")
	sp := f.seedSpace(userA)
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)
	comment := document.NewComment(doc.ID, userB, "评论", nil)
	require.NoError(t, f.comms.Create(context.Background(), comment))

	require.ErrorIs(t, f.svc.DeleteComment(context.Background(), userA, comment.ID), document.ErrCommentForbidden)
	require.NoError(t, f.svc.DeleteComment(context.Background(), userB, comment.ID))

	views, err := f.svc.ListComments(context.Background(), doc.ID)
	require.NoError(t, err)
	assert.Empty(t, views)
}
