// Package document 业务层:F3 文档 + F4 信息流/点赞/评论。
// 依据规格:docs/specs/f3-document.md、f4-feed.md;契约:api-contract.md §F3/§F4。
package document

import (
	"context"
	"errors"
	"strings"
	"time"

	"labnexus/internal/database"
	"labnexus/internal/project"
	"labnexus/internal/resource"
	"labnexus/internal/space"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrDocumentNotFound  = errors.New("document not found")
	ErrDocumentForbidden = errors.New("cannot modify others' document")
	ErrTitleEmpty        = errors.New("document title is empty")
	ErrInvalidVisibility = errors.New("invalid visibility")
	ErrCommentNotFound   = errors.New("comment not found")
	ErrCommentForbidden  = errors.New("cannot delete others' comment")
	ErrContentEmpty      = errors.New("content is empty")
	ErrInvalidReply      = errors.New("invalid reply target")
	ErrEmptyQuery        = errors.New("search query is empty")
	ErrInvalidSearchType = errors.New("invalid search type")
)

// 分页默认值
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
	SearchLimit     = 50
)

// NormalizePage 归一化分页参数(handler 与 service 共用,保证回显与实际一致)。
func NormalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// Service 文档/信息流业务逻辑
type Service struct {
	docs      Repository
	comments  CommentRepository
	reactions ReactionRepository
	tags      tag.Repository
	users     user.Repository
	spaces    space.Repository
	folders   space.FolderRepository
	txRunner  database.TxRunner

	resourceSearch ResourceSearchFn // 可选:资源搜索(阶段 2)
	taskSearch     TaskSearchFn     // 可选:任务搜索(阶段 2)
	resourceByTag  ResourceByTagFn  // 可选:标签资源列表(阶段 2 F7)
}

// NewService 构造函数(依赖注入)
func NewService(
	docs Repository,
	comments CommentRepository,
	reactions ReactionRepository,
	tags tag.Repository,
	users user.Repository,
	spaces space.Repository,
	folders space.FolderRepository,
) *Service {
	return &Service{
		docs: docs, comments: comments, reactions: reactions,
		tags: tags, users: users, spaces: spaces, folders: folders,
		txRunner: database.NoopTxRunner(),
	}
}

// WithTxRunner 注入事务运行器(写操作事务化)。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// CreateDocumentRequest 创建文档请求
type CreateDocumentRequest struct {
	FolderID   *string  `json:"folder_id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Visibility string   `json:"visibility"`
	TagIDs     []string `json:"tag_ids"`
}

// UpdateDocumentRequest 修改文档请求(folder_id 空串 = 移出目录)
type UpdateDocumentRequest struct {
	Title      *string   `json:"title"`
	Content    *string   `json:"content"`
	Visibility *string   `json:"visibility"`
	FolderID   *string   `json:"folder_id"`
	Pinned     *bool     `json:"pinned"`
	TagIDs     *[]string `json:"tag_ids"`
}

// DocumentView 文档视图(含作者/标签/计数)
type DocumentView struct {
	*Document
	Author         *user.User `json:"author"`
	Tags           []*tag.Tag `json:"tags"`
	ReactionsCount int64      `json:"reactions_count"`
	CommentsCount  int64      `json:"comments_count"`
}

// CommentView 评论视图(含作者)
type CommentView struct {
	*Comment
	Author *user.User `json:"author"`
}

// ---- F3:文档 ----

// CreateDocument 创建文档(校验目录归属、标签存在;事务写文档+标签)。
func (s *Service) CreateDocument(ctx context.Context, userID string, req CreateDocumentRequest) (*DocumentView, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, ErrTitleEmpty
	}
	if !validVisibility(req.Visibility) {
		return nil, ErrInvalidVisibility
	}
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.validateFolder(ctx, sp, req.FolderID); err != nil {
		return nil, err
	}
	if err := s.validateTags(ctx, req.TagIDs); err != nil {
		return nil, err
	}

	doc := NewDocument(userID, sp.ID, req.FolderID, req.Title, req.Content, req.Visibility)
	err = s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.docs.Create(tctx, doc); err != nil {
			return err
		}
		return s.docs.SetTags(tctx, doc.ID, req.TagIDs)
	})
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, doc)
}

// UpdateDocument 修改文档(仅作者;可见性切换=发布/撤回)。
func (s *Service) UpdateDocument(ctx context.Context, userID, docID string, req UpdateDocumentRequest) (*DocumentView, error) {
	doc, err := s.docs.GetByID(ctx, docID)
	if err != nil {
		return nil, ErrDocumentNotFound
	}
	if doc.AuthorID != userID {
		return nil, ErrDocumentForbidden
	}

	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, ErrTitleEmpty
		}
		doc.Title = *req.Title
	}
	if req.Content != nil {
		doc.Content = *req.Content
	}
	if req.Visibility != nil {
		if !validVisibility(*req.Visibility) {
			return nil, ErrInvalidVisibility
		}
		doc.Visibility = *req.Visibility
	}
	if req.Pinned != nil {
		doc.Pinned = *req.Pinned
	}
	if req.FolderID != nil {
		// 空串 = 移出目录;否则校验归属
		if *req.FolderID == "" {
			doc.FolderID = nil
		} else {
			sp, err := s.spaceOf(ctx, userID)
			if err != nil {
				return nil, err
			}
			if err := s.validateFolder(ctx, sp, req.FolderID); err != nil {
				return nil, err
			}
			doc.FolderID = req.FolderID
		}
	}

	doc.UpdatedAt = time.Now()
	err = s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.docs.Update(tctx, doc); err != nil {
			return err
		}
		if req.TagIDs != nil {
			if err := s.validateTags(tctx, *req.TagIDs); err != nil {
				return err
			}
			return s.docs.SetTags(tctx, doc.ID, *req.TagIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, doc)
}

// GetDocument 查看文档(作者或公开;他人私有 → 404 不泄露)。
func (s *Service) GetDocument(ctx context.Context, userID, docID string) (*DocumentView, error) {
	doc, err := s.visibleDoc(ctx, userID, docID)
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, doc)
}

// DeleteDocument 软删除(仅作者)。
func (s *Service) DeleteDocument(ctx context.Context, userID, docID string) error {
	doc, err := s.docs.GetByID(ctx, docID)
	if err != nil {
		return ErrDocumentNotFound
	}
	if doc.AuthorID != userID {
		return ErrDocumentForbidden
	}
	return s.docs.SoftDelete(ctx, doc.ID)
}

// ListMyDocuments 我的文档列表(按目录/可见性筛选)。
func (s *Service) ListMyDocuments(ctx context.Context, userID string, folderID *string, visibility string) ([]*DocumentView, error) {
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	docs, err := s.docs.ListBySpace(ctx, sp.ID, folderID, visibility)
	if err != nil {
		return nil, err
	}
	return s.buildViews(ctx, docs)
}

// TagContents 标签内容页(F5):打了该标签的文档(可见性过滤)与资源(全组可见)。
func (s *Service) TagContents(ctx context.Context, userID, tagID string) (*TagContentsResult, error) {
	if _, err := s.tags.GetByID(ctx, tagID); err != nil {
		return nil, tag.ErrTagNotFound
	}
	docs, err := s.docs.ListByTag(ctx, tagID)
	if err != nil {
		return nil, err
	}
	var visible []*Document
	for _, d := range docs {
		if d.Visibility == VisibilityPublic || d.AuthorID == userID {
			visible = append(visible, d)
		}
	}
	docViews, err := s.buildViews(ctx, visible)
	if err != nil {
		return nil, err
	}

	res := &TagContentsResult{Documents: docViews, Resources: []*resource.ResourceView{}}
	if s.resourceByTag != nil {
		views, err := s.resourceByTag(ctx, tagID)
		if err != nil {
			return nil, err
		}
		if views == nil {
			views = []*resource.ResourceView{}
		}
		res.Resources = views
	}
	return res, nil
}

// TagContentsResult 标签内容页聚合结果(文档 + 资源)
type TagContentsResult struct {
	Documents []*DocumentView          `json:"documents"`
	Resources []*resource.ResourceView `json:"resources"`
}

// ---- F4:信息流 ----

// GetFeed 社区信息流(latest 创建倒序 / hot 点赞数倒序,分页)。
func (s *Service) GetFeed(ctx context.Context, sort string, page, pageSize int) ([]*DocumentView, int64, error) {
	page, pageSize = NormalizePage(page, pageSize)
	offset := (page - 1) * pageSize
	docs, total, err := s.docs.ListPublic(ctx, sort, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.buildViews(ctx, docs)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// ToggleReaction 点赞 toggle(存在则取消)。
func (s *Service) ToggleReaction(ctx context.Context, userID, docID, emoji string) error {
	if _, err := s.visibleDoc(ctx, userID, docID); err != nil {
		return err
	}
	existing, err := s.reactions.Find(ctx, docID, userID, emoji)
	if err == nil {
		return s.reactions.Delete(ctx, existing.ID)
	}
	if errors.Is(err, ErrNotFound) {
		return s.reactions.Create(ctx, NewReaction(docID, userID, emoji))
	}
	return err
}

// ListComments 文档评论列表。
func (s *Service) ListComments(ctx context.Context, docID string) ([]*CommentView, error) {
	comments, err := s.comments.ListByDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	return s.buildCommentViews(ctx, comments)
}

// CreateComment 发表评论(公开文档或作者;一级回复)。
func (s *Service) CreateComment(ctx context.Context, userID, docID, content string, replyToID *string) (*CommentView, error) {
	if strings.TrimSpace(content) == "" {
		return nil, ErrContentEmpty
	}
	if _, err := s.visibleDoc(ctx, userID, docID); err != nil {
		return nil, err
	}
	if replyToID != nil {
		reply, err := s.comments.GetByID(ctx, *replyToID)
		if err != nil || reply.DocumentID != docID {
			return nil, ErrInvalidReply
		}
	}
	comment := NewComment(docID, userID, content, replyToID)
	if err := s.comments.Create(ctx, comment); err != nil {
		return nil, err
	}
	author, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &CommentView{Comment: comment, Author: author}, nil
}

// DeleteComment 删除评论(仅作者)。
func (s *Service) DeleteComment(ctx context.Context, userID, commentID string) error {
	comment, err := s.comments.GetByID(ctx, commentID)
	if err != nil {
		return ErrCommentNotFound
	}
	if comment.AuthorID != userID {
		return ErrCommentForbidden
	}
	return s.comments.Delete(ctx, comment.ID)
}

// ---- F6:搜索 ----

// SearchResult 搜索结果(三组结构固定;资源/任务由阶段 2 搜索器注入)
type SearchResult struct {
	Documents []*DocumentView      `json:"documents"`
	Resources []*resource.Resource `json:"resources"`
	Tasks     []*project.Task      `json:"tasks"`
}

// ResourceSearchFn 资源搜索器(由 app 装配注入,阶段 2 F7)
type ResourceSearchFn func(ctx context.Context, q string, limit int) ([]*resource.Resource, error)

// TaskSearchFn 任务搜索器(由 app 装配注入,阶段 2 F9)
type TaskSearchFn func(ctx context.Context, q string, limit int) ([]*project.Task, error)

// WithSearchProviders 注入跨类型搜索器(资源/任务;nil 时对应分组返回空)。
func (s *Service) WithSearchProviders(resFn ResourceSearchFn, taskFn TaskSearchFn) *Service {
	s.resourceSearch = resFn
	s.taskSearch = taskFn
	return s
}

// ResourceByTagFn 按标签列出资源(由 app 装配注入,F7)。
type ResourceByTagFn func(ctx context.Context, tagID string) ([]*resource.ResourceView, error)

// WithResourceByTag 注入标签资源列表器(/tags/:id/contents 聚合资源)。
func (s *Service) WithResourceByTag(fn ResourceByTagFn) *Service {
	s.resourceByTag = fn
	return s
}

// Search 关键词搜索:文档(标题/正文,公开+本人)+ 资源/任务(若已注入搜索器)。
func (s *Service) Search(ctx context.Context, userID, q, contentType string) (*SearchResult, error) {
	if strings.TrimSpace(q) == "" {
		return nil, ErrEmptyQuery
	}
	empty := &SearchResult{
		Documents: []*DocumentView{},
		Resources: []*resource.Resource{},
		Tasks:     []*project.Task{},
	}
	switch contentType {
	case "document":
		docs, err := s.docs.Search(ctx, userID, q, SearchLimit)
		if err != nil {
			return nil, err
		}
		views, err := s.buildViews(ctx, docs)
		if err != nil {
			return nil, err
		}
		return &SearchResult{Documents: views, Resources: []*resource.Resource{}, Tasks: []*project.Task{}}, nil
	case "resource":
		if s.resourceSearch == nil {
			return empty, nil
		}
		list, err := s.resourceSearch(ctx, q, SearchLimit)
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []*resource.Resource{}
		}
		return &SearchResult{Documents: []*DocumentView{}, Resources: list, Tasks: []*project.Task{}}, nil
	case "task":
		if s.taskSearch == nil {
			return empty, nil
		}
		list, err := s.taskSearch(ctx, q, SearchLimit)
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []*project.Task{}
		}
		return &SearchResult{Documents: []*DocumentView{}, Resources: []*resource.Resource{}, Tasks: list}, nil
	case "":
		// 默认:聚合文档 + 资源 + 任务(资源/任务依赖注入的搜索器)
		docs, err := s.docs.Search(ctx, userID, q, SearchLimit)
		if err != nil {
			return nil, err
		}
		views, err := s.buildViews(ctx, docs)
		if err != nil {
			return nil, err
		}
		result := &SearchResult{Documents: views, Resources: []*resource.Resource{}, Tasks: []*project.Task{}}
		if s.resourceSearch != nil {
			list, err := s.resourceSearch(ctx, q, SearchLimit)
			if err != nil {
				return nil, err
			}
			result.Resources = list
		}
		if s.taskSearch != nil {
			list, err := s.taskSearch(ctx, q, SearchLimit)
			if err != nil {
				return nil, err
			}
			result.Tasks = list
		}
		return result, nil
	default:
		return nil, ErrInvalidSearchType
	}
}

// ---- 视图组装(防 N+1:批量查询) ----

// buildView 单个文档视图(复用批量查询)。
func (s *Service) buildView(ctx context.Context, doc *Document) (*DocumentView, error) {
	views, err := s.buildViews(ctx, []*Document{doc})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

func (s *Service) buildViews(ctx context.Context, docs []*Document) ([]*DocumentView, error) {
	if len(docs) == 0 {
		return []*DocumentView{}, nil
	}
	docIDs := make([]string, 0, len(docs))
	authorIDs := make([]string, 0, len(docs))
	for _, d := range docs {
		docIDs = append(docIDs, d.ID)
		authorIDs = append(authorIDs, d.AuthorID)
	}

	// 批量查作者
	users, err := s.users.GetByIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	// 批量查标签与计数
	tagsByDoc, err := s.tags.ListByDocumentIDs(ctx, docIDs)
	if err != nil {
		return nil, err
	}
	reactionStats, err := s.docs.ReactionStats(ctx, docIDs)
	if err != nil {
		return nil, err
	}
	commentStats, err := s.docs.CommentStats(ctx, docIDs)
	if err != nil {
		return nil, err
	}

	views := make([]*DocumentView, 0, len(docs))
	for _, d := range docs {
		tags := tagsByDoc[d.ID]
		if tags == nil {
			tags = []*tag.Tag{} // JSON 输出 [] 而非 null
		}
		views = append(views, &DocumentView{
			Document:       d,
			Author:         userByID[d.AuthorID],
			Tags:           tags,
			ReactionsCount: reactionStats[d.ID],
			CommentsCount:  commentStats[d.ID],
		})
	}
	return views, nil
}

func (s *Service) buildCommentViews(ctx context.Context, comments []*Comment) ([]*CommentView, error) {
	if len(comments) == 0 {
		return []*CommentView{}, nil
	}
	authorIDs := make([]string, 0, len(comments))
	for _, c := range comments {
		authorIDs = append(authorIDs, c.AuthorID)
	}
	users, err := s.users.GetByIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}
	views := make([]*CommentView, 0, len(comments))
	for _, c := range comments {
		views = append(views, &CommentView{Comment: c, Author: userByID[c.AuthorID]})
	}
	return views, nil
}

// ---- 内部 helper ----

// visibleDoc 校验文档对 userID 可见(公开或作者),返回文档。
func (s *Service) visibleDoc(ctx context.Context, userID, docID string) (*Document, error) {
	doc, err := s.docs.GetByID(ctx, docID)
	if err != nil {
		return nil, ErrDocumentNotFound
	}
	if doc.Visibility != VisibilityPublic && doc.AuthorID != userID {
		return nil, ErrDocumentNotFound // 他人私有 → 404,不泄露存在
	}
	return doc, nil
}

// spaceOf 获取用户空间。
func (s *Service) spaceOf(ctx context.Context, userID string) (*space.Space, error) {
	sp, err := s.spaces.GetByUserID(ctx, userID)
	if err != nil {
		return nil, space.ErrSpaceNotFound
	}
	return sp, nil
}

// validateFolder 校验目录属于该空间(可空)。
func (s *Service) validateFolder(ctx context.Context, sp *space.Space, folderID *string) error {
	if folderID == nil || *folderID == "" {
		return nil
	}
	f, err := s.folders.GetByID(ctx, *folderID)
	if err != nil {
		return space.ErrFolderNotFound
	}
	if f.SpaceID != sp.ID {
		return space.ErrFolderNotOwned
	}
	return nil
}

// validateTags 校验标签全部存在。
func (s *Service) validateTags(ctx context.Context, tagIDs []string) error {
	for _, id := range tagIDs {
		if _, err := s.tags.GetByID(ctx, id); err != nil {
			return tag.ErrTagNotFound
		}
	}
	return nil
}

// validVisibility 校验可见性枚举。
func validVisibility(v string) bool {
	return v == VisibilityPrivate || v == VisibilityPublic
}
