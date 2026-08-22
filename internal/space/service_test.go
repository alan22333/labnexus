package space_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/space"
)

// ---- 内存替身 ----

type memSpaceRepo struct {
	byUser map[string]*space.Space
}

func newMemSpaceRepo() *memSpaceRepo {
	return &memSpaceRepo{byUser: map[string]*space.Space{}}
}

func (r *memSpaceRepo) Create(_ context.Context, s *space.Space) error {
	r.byUser[s.UserID] = s
	return nil
}

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

func newMemFolderRepo() *memFolderRepo {
	return &memFolderRepo{byID: map[string]*space.Folder{}}
}

func (r *memFolderRepo) Create(_ context.Context, f *space.Folder) error {
	r.byID[f.ID] = f
	return nil
}

func (r *memFolderRepo) GetByID(_ context.Context, id string) (*space.Folder, error) {
	f, ok := r.byID[id]
	if !ok {
		return nil, space.ErrNotFound
	}
	return f, nil
}

func (r *memFolderRepo) ListBySpace(_ context.Context, spaceID string) ([]*space.Folder, error) {
	var out []*space.Folder
	for _, f := range r.byID {
		if f.SpaceID == spaceID {
			out = append(out, f)
		}
	}
	// 与 GORM 实现一致:按 sort_order 排序(次键用 ID 保证稳定)
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *memFolderRepo) Update(_ context.Context, f *space.Folder) error {
	if _, ok := r.byID[f.ID]; !ok {
		return space.ErrNotFound
	}
	r.byID[f.ID] = f
	return nil
}

func (r *memFolderRepo) Delete(_ context.Context, id string) error {
	delete(r.byID, id)
	return nil
}

func (r *memFolderRepo) CountChildren(_ context.Context, parentID string) (int64, error) {
	var n int64
	for _, f := range r.byID {
		if f.ParentID != nil && *f.ParentID == parentID {
			n++
		}
	}
	return n, nil
}

// ---- 夹具 ----

func newTestService(t *testing.T) (*space.Service, *memSpaceRepo, *memFolderRepo) {
	t.Helper()
	spaces := newMemSpaceRepo()
	folders := newMemFolderRepo()
	svc := space.NewService(spaces, folders)
	return svc, spaces, folders
}

const (
	userA = "user-a"
	userB = "user-b"
)

func seedSpace(t *testing.T, spaces *memSpaceRepo, userID string) *space.Space {
	t.Helper()
	s := space.NewSpace(userID)
	require.NoError(t, spaces.Create(context.Background(), s))
	return s
}

func seedFolder(t *testing.T, folders *memFolderRepo, spaceID string, parentID *string, name string) *space.Folder {
	t.Helper()
	f := space.NewFolder(spaceID, parentID, name, 0)
	require.NoError(t, folders.Create(context.Background(), f))
	return f
}

// ---- 获取空间 ----

func TestGetSpace_WithTree(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	sp := seedSpace(t, spaces, userA)
	root := seedFolder(t, folders, sp.ID, nil, "会议记录")
	seedFolder(t, folders, sp.ID, &root.ID, "组会")
	seedFolder(t, folders, sp.ID, nil, "日常记录")

	gotSpace, tree, err := svc.GetSpace(context.Background(), userA)
	require.NoError(t, err)
	assert.Equal(t, sp.ID, gotSpace.ID)
	require.Len(t, tree, 2)

	// 找"会议记录"根节点(不依赖插入顺序),验证树形:组会是其子节点
	var meetingRoot *space.FolderNode
	for _, n := range tree {
		if n.Name == "会议记录" {
			meetingRoot = n
		}
	}
	require.NotNil(t, meetingRoot, "应存在会议记录根节点")
	require.Len(t, meetingRoot.Children, 1)
	assert.Equal(t, "组会", meetingRoot.Children[0].Name)
}

func TestGetSpace_NoSpace(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, _, err := svc.GetSpace(context.Background(), userA)
	assert.ErrorIs(t, err, space.ErrSpaceNotFound)
}

// ---- 创建目录 ----

func TestCreateFolder_Root(t *testing.T) {
	svc, spaces, _ := newTestService(t)
	seedSpace(t, spaces, userA)

	f, err := svc.CreateFolder(context.Background(), userA, "会议记录", nil)
	require.NoError(t, err)
	assert.Equal(t, "会议记录", f.Name)
	assert.Nil(t, f.ParentID)
}

func TestCreateFolder_WithParent(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	sp := seedSpace(t, spaces, userA)
	root := seedFolder(t, folders, sp.ID, nil, "会议记录")

	f, err := svc.CreateFolder(context.Background(), userA, "组会", &root.ID)
	require.NoError(t, err)
	assert.Equal(t, root.ID, *f.ParentID)
}

func TestCreateFolder_ParentNotOwned(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	spA := seedSpace(t, spaces, userA)
	spB := seedSpace(t, spaces, userB)
	otherFolder := seedFolder(t, folders, spB.ID, nil, "别人的目录")

	_, err := svc.CreateFolder(context.Background(), userA, "子目录", &otherFolder.ID)
	assert.ErrorIs(t, err, space.ErrFolderNotOwned)
	_ = spA
}

func TestCreateFolder_ParentNotFound(t *testing.T) {
	svc, spaces, _ := newTestService(t)
	seedSpace(t, spaces, userA)

	missing := "no-such-folder"
	_, err := svc.CreateFolder(context.Background(), userA, "子目录", &missing)
	assert.ErrorIs(t, err, space.ErrFolderNotFound)
}

func TestCreateFolder_EmptyName(t *testing.T) {
	svc, spaces, _ := newTestService(t)
	seedSpace(t, spaces, userA)

	_, err := svc.CreateFolder(context.Background(), userA, "   ", nil)
	assert.ErrorIs(t, err, space.ErrFolderNameEmpty)
}

func TestCreateFolder_NoSpace(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.CreateFolder(context.Background(), userA, "目录", nil)
	assert.ErrorIs(t, err, space.ErrSpaceNotFound)
}

// ---- 修改目录 ----

func TestUpdateFolder_RenameAndSort(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	sp := seedSpace(t, spaces, userA)
	f := seedFolder(t, folders, sp.ID, nil, "旧名")

	newName := "新名"
	newSort := 5
	got, err := svc.UpdateFolder(context.Background(), userA, f.ID, &newName, &newSort)
	require.NoError(t, err)
	assert.Equal(t, "新名", got.Name)
	assert.Equal(t, 5, got.SortOrder)
}

func TestUpdateFolder_NotOwned(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	seedSpace(t, spaces, userA)
	spB := seedSpace(t, spaces, userB)
	other := seedFolder(t, folders, spB.ID, nil, "别人的")

	_, err := svc.UpdateFolder(context.Background(), userA, other.ID, nil, nil)
	assert.ErrorIs(t, err, space.ErrFolderNotOwned)
}

func TestUpdateFolder_NotFound(t *testing.T) {
	svc, spaces, _ := newTestService(t)
	seedSpace(t, spaces, userA)

	_, err := svc.UpdateFolder(context.Background(), userA, "no-such", nil, nil)
	assert.ErrorIs(t, err, space.ErrFolderNotFound)
}

// ---- 删除目录 ----

func TestDeleteFolder_Empty(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	sp := seedSpace(t, spaces, userA)
	f := seedFolder(t, folders, sp.ID, nil, "空目录")

	err := svc.DeleteFolder(context.Background(), userA, f.ID)
	require.NoError(t, err)
	_, err = folders.GetByID(context.Background(), f.ID)
	assert.ErrorIs(t, err, space.ErrNotFound)
}

func TestDeleteFolder_NotEmpty(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	sp := seedSpace(t, spaces, userA)
	root := seedFolder(t, folders, sp.ID, nil, "父目录")
	seedFolder(t, folders, sp.ID, &root.ID, "子目录")

	err := svc.DeleteFolder(context.Background(), userA, root.ID)
	assert.ErrorIs(t, err, space.ErrFolderNotEmpty)
}

func TestDeleteFolder_NotOwned(t *testing.T) {
	svc, spaces, folders := newTestService(t)
	seedSpace(t, spaces, userA)
	spB := seedSpace(t, spaces, userB)
	other := seedFolder(t, folders, spB.ID, nil, "别人的")

	err := svc.DeleteFolder(context.Background(), userA, other.ID)
	assert.ErrorIs(t, err, space.ErrFolderNotOwned)
}

// 断言辅助:确保 errors 包被引用(避免误删)
var _ = errors.Is
