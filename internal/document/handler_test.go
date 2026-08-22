package document_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/document"
	"labnexus/internal/token"
)

func newTestRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	h := document.NewHandler(f.svc)
	r := gin.New()
	h.RegisterRoutes(r, "test-secret")
	return r, f
}

func authHeader(userID string) string {
	access, _ := token.GenerateAccessToken("test-secret", userID, "student", 15*time.Minute)
	return "Bearer " + access
}

func docDo(t *testing.T, r *gin.Engine, method, path, body, tokenStr string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
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

// ensureUser 补齐用户与其个人空间(幂等)
func (f *fixture) ensureUser(id string) {
	if f.users.byID[id] == nil {
		f.seedUser(id, id)
	}
	if f.spaces.byUser[id] == nil {
		f.seedSpace(id)
	}
}

func TestDoc_RequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/feed"},
		{http.MethodPost, "/api/me/documents"},
		{http.MethodGet, "/api/documents/x"},
		{http.MethodPost, "/api/documents/x/reactions"},
		{http.MethodPost, "/api/documents/x/comments"},
		{http.MethodGet, "/api/tags/x/contents"},
	} {
		w := docDo(t, r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", tc.method, tc.path)
	}
}

func TestDoc_CreateAndGet(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	tg := f.seedTag("组会")

	w := docDo(t, r, http.MethodPost, "/api/me/documents",
		fmt.Sprintf(`{"title":"组会纪要","content":"## 议题","visibility":"public","tag_ids":["%s"]}`, tg.ID),
		authHeader(userA))
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"title":"组会纪要"`)

	var resp struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// 他人可看公开文档
	w2 := docDo(t, r, http.MethodGet, "/api/documents/"+resp.Document.ID, "", authHeader(userB))
	require.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), `"author"`)
}

func TestDoc_PrivateHiddenFromOthers(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)

	w := docDo(t, r, http.MethodPost, "/api/me/documents",
		`{"title":"私密笔记","visibility":"private"}`, authHeader(userA))
	require.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	w2 := docDo(t, r, http.MethodGet, "/api/documents/"+resp.Document.ID, "", authHeader(userB))
	assert.Equal(t, http.StatusNotFound, w2.Code)
}

func TestDoc_FeedPagination(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	sp := f.spaces.byUser[userA]
	f.seedDoc(userA, sp.ID, nil, "帖1", document.VisibilityPublic)
	f.seedDoc(userA, sp.ID, nil, "帖2", document.VisibilityPublic)

	w := docDo(t, r, http.MethodGet, "/api/feed?page=1&page_size=10", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":2`)
	assert.Contains(t, w.Body.String(), `"帖1"`)
}

func TestDoc_ReactionAndCommentFlow(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	f.ensureUser(userB)
	sp := f.spaces.byUser[userA]
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)

	// B 点赞
	w := docDo(t, r, http.MethodPost, "/api/documents/"+doc.ID+"/reactions", `{"emoji":"👍"}`, authHeader(userB))
	assert.Equal(t, http.StatusNoContent, w.Code)

	// B 评论
	w2 := docDo(t, r, http.MethodPost, "/api/documents/"+doc.ID+"/comments", `{"content":"好帖"}`, authHeader(userB))
	require.Equal(t, http.StatusCreated, w2.Code)
	assert.Contains(t, w2.Body.String(), `"author"`)

	// 评论列表含作者
	w3 := docDo(t, r, http.MethodGet, "/api/documents/"+doc.ID+"/comments", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w3.Code)
	assert.Contains(t, w3.Body.String(), "好帖")

	// 评论计数出现在 feed
	w4 := docDo(t, r, http.MethodGet, "/api/feed", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w4.Code)
	assert.Contains(t, w4.Body.String(), `"reactions_count":1`)
	assert.Contains(t, w4.Body.String(), `"comments_count":1`)
}

func TestDoc_TagContents(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	tg := f.seedTag("投稿")
	sp := f.spaces.byUser[userA]
	doc := f.seedDoc(userA, sp.ID, nil, "投稿经验", document.VisibilityPublic)
	require.NoError(t, f.docs.SetTags(context.Background(), doc.ID, []string{tg.ID}))

	w := docDo(t, r, http.MethodGet, "/api/tags/"+tg.ID+"/contents", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "投稿经验")
}

func TestDoc_DeleteCommentForbidden(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	f.ensureUser(userB)
	sp := f.spaces.byUser[userA]
	doc := f.seedDoc(userA, sp.ID, nil, "帖", document.VisibilityPublic)
	comment := document.NewComment(doc.ID, userB, "B的评论", nil)
	require.NoError(t, f.comms.Create(context.Background(), comment))

	// A 删 B 的评论 → 403
	w := docDo(t, r, http.MethodDelete, "/api/comments/"+comment.ID, "", authHeader(userA))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSearch_RequiresAuth(t *testing.T) {
	r, _ := newTestRouter(t)
	w := docDo(t, r, http.MethodGet, "/api/search?q=x", "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSearch_MissingQuery(t *testing.T) {
	r, _ := newTestRouter(t)
	w := docDo(t, r, http.MethodGet, "/api/search", "", authHeader(userA))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "VALIDATION")
}

func TestSearch_Success(t *testing.T) {
	r, f := newTestRouter(t)
	f.ensureUser(userA)
	f.ensureUser(userB)
	spA := f.spaces.byUser[userA]
	f.seedDocContent(userA, spA.ID, nil, "投稿避坑指南", "关于投稿的经验", document.VisibilityPublic)
	spB := f.spaces.byUser[userB]
	f.seedDocContent(userB, spB.ID, nil, "B的秘密", "投稿 绝密", document.VisibilityPrivate)

	w := docDo(t, r, http.MethodGet, "/api/search?q=投稿", "", authHeader(userA))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "投稿避坑指南")
	assert.NotContains(t, w.Body.String(), "B的秘密")
	assert.Contains(t, w.Body.String(), `"resources":[]`)
	assert.Contains(t, w.Body.String(), `"tasks":[]`)
}
