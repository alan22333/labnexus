//go:build integration

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 越权矩阵:修改/删除他人资源一律 403;访问他人私有一律 404
func TestPermission_Document(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	privID := createDoc(t, r, tokenA, "A的私密", "private")
	pubID := createDoc(t, r, tokenA, "A的公开", "public")

	// B 修改/删除 A 的文档(无论公开私有)→ 403
	assertError(t, doJSON(t, r, http.MethodPatch, "/api/documents/"+pubID, `{"title":"hack"}`, tokenB),
		http.StatusForbidden, "FORBIDDEN")
	assertError(t, doJSON(t, r, http.MethodDelete, "/api/documents/"+pubID, "", tokenB),
		http.StatusForbidden, "FORBIDDEN")
	assertError(t, doJSON(t, r, http.MethodPatch, "/api/documents/"+privID, `{"title":"hack"}`, tokenB),
		http.StatusForbidden, "FORBIDDEN")

	// 不存在的文档 → 404
	assertError(t, doJSON(t, r, http.MethodPatch, "/api/documents/no-such-id", `{"title":"x"}`, tokenB),
		http.StatusNotFound, "NOT_FOUND")
}

func TestPermission_Comment(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	docID := createDoc(t, r, tokenA, "A的公开", "public")
	wC := doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/comments",
		`{"content":"B 的评论"}`, tokenB)
	require.Equal(t, http.StatusCreated, wC.Code)
	var body struct {
		Comment struct {
			ID string `json:"id"`
		} `json:"comment"`
	}
	parseJSON(t, wC, &body)

	// A 删 B 的评论 → 403
	assertError(t, doJSON(t, r, http.MethodDelete, "/api/comments/"+body.Comment.ID, "", tokenA),
		http.StatusForbidden, "FORBIDDEN")
	// B 删自己的评论 → 204
	assert.Equal(t, http.StatusNoContent,
		doJSON(t, r, http.MethodDelete, "/api/comments/"+body.Comment.ID, "", tokenB).Code)
}

func TestPermission_Folder(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	tokenB := registerUser(t, r, "bob", "Bob")

	rootID := createFolder(t, r, tokenA, "A的目录", nil)
	subID := createFolder(t, r, tokenA, "A的子目录", &rootID)

	// B 改/删 A 的目录 → 403
	assertError(t, doJSON(t, r, http.MethodPatch, "/api/me/folders/"+rootID, `{"name":"hack"}`, tokenB),
		http.StatusForbidden, "FORBIDDEN")
	assertError(t, doJSON(t, r, http.MethodDelete, "/api/me/folders/"+rootID, "", tokenB),
		http.StatusForbidden, "FORBIDDEN")

	// B 在 A 的目录下建目录 → 403
	assertError(t, doJSON(t, r, http.MethodPost, "/api/me/folders",
		`{"name":"x","parent_id":"`+rootID+`"}`, tokenB), http.StatusForbidden, "FORBIDDEN")

	// B 在 A 的目录下建文档 → 403
	assertError(t, doJSON(t, r, http.MethodPost, "/api/me/documents",
		`{"title":"x","visibility":"private","folder_id":"`+rootID+`"}`, tokenB),
		http.StatusForbidden, "FORBIDDEN")

	// A 删非空目录 → 409;删空子目录 → 204;根目录空后 → 204
	assertError(t, doJSON(t, r, http.MethodDelete, "/api/me/folders/"+rootID, "", tokenA),
		http.StatusConflict, "CONFLICT")
	assert.Equal(t, http.StatusNoContent, doJSON(t, r, http.MethodDelete, "/api/me/folders/"+subID, "", tokenA).Code)
	assert.Equal(t, http.StatusNoContent, doJSON(t, r, http.MethodDelete, "/api/me/folders/"+rootID, "", tokenA).Code)
}

// 目录删除约束:F3 集成后,含文档的目录不可删
func TestPermission_FolderWithDocuments(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")

	rootID := createFolder(t, r, tokenA, "含文档目录", nil)
	w := doJSON(t, r, http.MethodPost, "/api/me/documents",
		`{"title":"目录里的文档","visibility":"private","folder_id":"`+rootID+`"}`, tokenA)
	require.Equal(t, http.StatusCreated, w.Code)

	assertError(t, doJSON(t, r, http.MethodDelete, "/api/me/folders/"+rootID, "", tokenA),
		http.StatusConflict, "CONFLICT")
}

// 回复约束:reply_to 不存在的评论 → 400
func TestPermission_InvalidReply(t *testing.T) {
	r := setupServer(t)
	tokenA := registerUser(t, r, "alice", "Alice")
	docID := createDoc(t, r, tokenA, "A的公开", "public")

	assertError(t, doJSON(t, r, http.MethodPost, "/api/documents/"+docID+"/comments",
		`{"content":"回复","reply_to_id":"no-such-comment"}`, tokenA),
		http.StatusBadRequest, "VALIDATION")
}
