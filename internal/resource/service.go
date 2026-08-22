// Package resource 业务层:F7 资源库 + F8 文献元数据。
// 依据规格:docs/specs/f7-resource.md、f8-paper-meta.md;契约:api-contract.md §F7/§F8。
package resource

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"gorm.io/datatypes"

	"labnexus/internal/database"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrInvalidType        = errors.New("invalid resource type")
	ErrTitleEmpty         = errors.New("resource title is empty")
	ErrURLRequired        = errors.New("url is required for link resource")
	ErrDOIOrArxivRequired = errors.New("doi or arxiv_id is required for paper resource")
	ErrResourceNotFound   = errors.New("resource not found")
	ErrResourceForbidden  = errors.New("cannot modify others' resource")
	ErrPaperMetaNotFound  = errors.New("paper metadata not found")
	ErrPaperMetaUpstream  = errors.New("paper metadata upstream error")
	ErrFileTooLarge       = errors.New("file too large")
	ErrFileTypeNotAllowed = errors.New("file type not allowed")
)

// 限制(规格 §5)
const (
	MaxFileSize      = 20 << 20 // 20MB
	MetaFetchTimeout = 5 * time.Second

	// 外部元数据服务(测试可覆盖)
	DefaultCrossrefBase = "https://api.crossref.org"
	DefaultArxivBase    = "http://export.arxiv.org/api"
)

// allowedExts 文件类型白名单
var allowedExts = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".txt": true, ".md": true,
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true,
}

// Service 资源业务逻辑
type Service struct {
	repo    Repository
	tags    tag.Repository
	store   FileStore
	users   user.Repository
	client  *http.Client
	txRunner database.TxRunner

	crossrefBase string
	arxivBase    string
}

// NewService 构造函数(依赖注入);httpClient 默认带超时,测试可用 WithEndpoints 替换。
func NewService(
	repo Repository,
	tags tag.Repository,
	store FileStore,
	users user.Repository,
) *Service {
	return &Service{
		repo: repo, tags: tags, store: store, users: users,
		client:       &http.Client{Timeout: MetaFetchTimeout},
		txRunner:     database.NoopTxRunner(),
		crossrefBase: DefaultCrossrefBase,
		arxivBase:    DefaultArxivBase,
	}
}

// WithTxRunner 注入事务运行器。
func (s *Service) WithTxRunner(runner database.TxRunner) *Service {
	s.txRunner = runner
	return s
}

// WithEndpoints 注入 HTTP 客户端与外部服务端点(F8 测试用,替换真实网络)。
func (s *Service) WithEndpoints(client *http.Client, crossrefBase, arxivBase string) *Service {
	s.client = client
	s.crossrefBase = crossrefBase
	s.arxivBase = arxivBase
	return s
}

// ResourceView 资源视图(含上传者/标签)
type ResourceView struct {
	*Resource
	Uploader *user.User `json:"uploader"`
	Tags     []*tag.Tag `json:"tags"`
}

// PaperMeta 文献元数据(F8 返回)
type PaperMeta struct {
	Title   string   `json:"title"`
	Authors []string `json:"authors"`
	Journal string   `json:"journal,omitempty"`
	Year    int      `json:"year,omitempty"`
	DOI     string   `json:"doi,omitempty"`
	ArxivID string   `json:"arxiv_id,omitempty"`
}

// CreateLinkRequest 链接资源创建请求
type CreateLinkRequest struct {
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	TagIDs []string `json:"tag_ids"`
}

// CreatePaperRequest 文献资源创建请求
type CreatePaperRequest struct {
	Title    string         `json:"title"`
	DOI      string         `json:"doi"`
	ArxivID  string         `json:"arxiv_id"`
	Metadata map[string]any `json:"metadata"`
	TagIDs   []string       `json:"tag_ids"`
}

// UpdateRequest 修改资源请求
type UpdateRequest struct {
	Title  *string   `json:"title"`
	TagIDs *[]string `json:"tag_ids"`
}

// ---- F7:创建 ----

// CreateLink 创建链接资源。
func (s *Service) CreateLink(ctx context.Context, userID string, req CreateLinkRequest) (*ResourceView, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, ErrTitleEmpty
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, ErrURLRequired
	}
	if err := s.validateTags(ctx, req.TagIDs); err != nil {
		return nil, err
	}
	res := NewResource(TypeLink, req.Title, userID)
	res.URL = req.URL
	if err := s.createWithTags(ctx, res, req.TagIDs); err != nil {
		return nil, err
	}
	return s.buildView(ctx, res)
}

// CreatePaper 创建文献资源(DOI/arxiv 至少其一)。
func (s *Service) CreatePaper(ctx context.Context, userID string, req CreatePaperRequest) (*ResourceView, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, ErrTitleEmpty
	}
	if strings.TrimSpace(req.DOI) == "" && strings.TrimSpace(req.ArxivID) == "" {
		return nil, ErrDOIOrArxivRequired
	}
	if err := s.validateTags(ctx, req.TagIDs); err != nil {
		return nil, err
	}
	res := NewResource(TypePaper, req.Title, userID)
	res.DOI = req.DOI
	res.ArxivID = req.ArxivID
	if len(req.Metadata) > 0 {
		meta, err := json.Marshal(req.Metadata)
		if err != nil {
			return nil, err
		}
		res.Metadata = datatypes.JSON(meta)
	}
	if err := s.createWithTags(ctx, res, req.TagIDs); err != nil {
		return nil, err
	}
	return s.buildView(ctx, res)
}

// UploadFile 上传文件资源。
func (s *Service) UploadFile(ctx context.Context, userID, filename string, reader io.Reader, title string, tagIDs []string) (*ResourceView, error) {
	ext := strings.ToLower(path.Ext(filename))
	if !allowedExts[ext] {
		return nil, ErrFileTypeNotAllowed
	}
	if title == "" {
		title = filename
	}
	if err := s.validateTags(ctx, tagIDs); err != nil {
		return nil, err
	}
	filePath, err := s.store.Save(reader, filename)
	if err != nil {
		return nil, err
	}
	res := NewResource(TypeFile, title, userID)
	res.FilePath = filePath
	if err := s.createWithTags(ctx, res, tagIDs); err != nil {
		_ = s.store.Delete(filePath) // 回滚已存文件
		return nil, err
	}
	return s.buildView(ctx, res)
}

// createWithTags 事务:写资源 + 标签。
func (s *Service) createWithTags(ctx context.Context, res *Resource, tagIDs []string) error {
	return s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.Create(tctx, res); err != nil {
			return err
		}
		return s.repo.SetTags(tctx, res.ID, tagIDs)
	})
}

// ---- F7:列表/详情 ----

// List 资源列表(全组可见;type/tag/keyword 筛选 + 分页)。
func (s *Service) List(ctx context.Context, f ListFilter) ([]*ResourceView, int64, error) {
	list, total, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.buildViews(ctx, list)
	if err != nil {
		return nil, 0, err
	}
	return views, total, nil
}

// Get 资源详情(任意登录用户)。
func (s *Service) Get(ctx context.Context, id string) (*ResourceView, error) {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrResourceNotFound
	}
	return s.buildView(ctx, res)
}

// Update 修改资源(仅上传者或 admin)。
func (s *Service) Update(ctx context.Context, userID, id string, req UpdateRequest) (*ResourceView, error) {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrResourceNotFound
	}
	ok, err := s.canManage(ctx, userID, res)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrResourceForbidden
	}

	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, ErrTitleEmpty
		}
		res.Title = *req.Title
	}
	res.UpdatedAt = time.Now()

	err = s.txRunner(ctx, func(tctx context.Context) error {
		if err := s.repo.Update(tctx, res); err != nil {
			return err
		}
		if req.TagIDs != nil {
			if err := s.validateTags(tctx, *req.TagIDs); err != nil {
				return err
			}
			return s.repo.SetTags(tctx, res.ID, *req.TagIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, res)
}

// Delete 删除资源(仅上传者或 admin;file 同步删磁盘文件)。
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	res, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return ErrResourceNotFound
	}
	ok, err := s.canManage(ctx, userID, res)
	if err != nil {
		return err
	}
	if !ok {
		return ErrResourceForbidden
	}
	if err := s.repo.Delete(ctx, res.ID); err != nil {
		return err
	}
	return s.store.Delete(res.FilePath)
}

// ---- F8:文献元数据 ----

// FetchPaperMeta 抓取文献元数据:doi → Crossref,arxiv_id → arXiv。
func (s *Service) FetchPaperMeta(ctx context.Context, doi, arxivID string) (*PaperMeta, error) {
	if doi == "" && arxivID == "" {
		return nil, ErrDOIOrArxivRequired
	}
	if doi != "" {
		return s.fetchCrossref(ctx, doi)
	}
	return s.fetchArxiv(ctx, arxivID)
}

// crossrefResponse Crossref API 响应结构(仅解析所需字段)
type crossrefResponse struct {
	Message struct {
		Title   []string `json:"title"`
		Author  []struct {
			Given  string `json:"given"`
			Family string `json:"family"`
		} `json:"author"`
		ContainerTitle []string `json:"container-title"`
		PublishedPrint struct {
			DateParts [][]int `json:"date-parts"`
		} `json:"published-print"`
		DOI string `json:"DOI"`
	} `json:"message"`
}

func (s *Service) fetchCrossref(ctx context.Context, doi string) (*PaperMeta, error) {
	escaped := url.PathEscape(doi)
	reqURL := fmt.Sprintf("%s/works/%s", s.crossrefBase, escaped)
	body, err := s.doGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	var resp crossrefResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ErrPaperMetaUpstream
	}
	meta := &PaperMeta{DOI: doi}
	if len(resp.Message.Title) > 0 {
		meta.Title = resp.Message.Title[0]
	}
	for _, a := range resp.Message.Author {
		name := strings.TrimSpace(a.Given + " " + a.Family)
		if name != "" {
			meta.Authors = append(meta.Authors, name)
		}
	}
	if len(resp.Message.ContainerTitle) > 0 {
		meta.Journal = resp.Message.ContainerTitle[0]
	}
	if len(resp.Message.PublishedPrint.DateParts) > 0 && len(resp.Message.PublishedPrint.DateParts[0]) > 0 {
		meta.Year = resp.Message.PublishedPrint.DateParts[0][0]
	}
	if meta.Title == "" {
		return nil, ErrPaperMetaNotFound
	}
	return meta, nil
}

// arxivFeed arXiv Atom 响应结构
type arxivFeed struct {
	Entries []struct {
		Title     string `xml:"title"`
		Authors   []struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Published string `xml:"published"`
		ID        string `xml:"id"`
	} `xml:"entry"`
}

func (s *Service) fetchArxiv(ctx context.Context, arxivID string) (*PaperMeta, error) {
	reqURL := fmt.Sprintf("%s/query?id_list=%s", s.arxivBase, url.QueryEscape(arxivID))
	body, err := s.doGet(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, ErrPaperMetaUpstream
	}
	if len(feed.Entries) == 0 {
		return nil, ErrPaperMetaNotFound
	}
	e := feed.Entries[0]
	meta := &PaperMeta{Title: strings.TrimSpace(e.Title), ArxivID: arxivID}
	for _, a := range e.Authors {
		if name := strings.TrimSpace(a.Name); name != "" {
			meta.Authors = append(meta.Authors, name)
		}
	}
	if t, err := time.Parse(time.RFC3339, e.Published); err == nil {
		meta.Year = t.Year()
	}
	if meta.Title == "" {
		return nil, ErrPaperMetaNotFound
	}
	return meta, nil
}

// doGet GET 请求;非 2xx 区分 not found / upstream。
func (s *Service) doGet(ctx context.Context, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, ErrPaperMetaUpstream
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, ErrPaperMetaUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrPaperMetaNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ErrPaperMetaUpstream
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, ErrPaperMetaUpstream
	}
	return body, nil
}

// ---- 内部 helper ----

// validateTags 校验标签全部存在。
func (s *Service) validateTags(ctx context.Context, tagIDs []string) error {
	for _, id := range tagIDs {
		if _, err := s.tags.GetByID(ctx, id); err != nil {
			return tag.ErrTagNotFound
		}
	}
	return nil
}

// canManage 上传者本人或 admin 可管理。
func (s *Service) canManage(ctx context.Context, userID string, res *Resource) (bool, error) {
	if res.UploaderID == userID {
		return true, nil
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return u.Role == user.RoleAdmin, nil
}

func (s *Service) buildView(ctx context.Context, res *Resource) (*ResourceView, error) {
	views, err := s.buildViews(ctx, []*Resource{res})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// buildViews 批量组装视图(作者/标签一次查询,防 N+1)。
func (s *Service) buildViews(ctx context.Context, list []*Resource) ([]*ResourceView, error) {
	if len(list) == 0 {
		return []*ResourceView{}, nil
	}
	ids := make([]string, 0, len(list))
	uploaderIDs := make([]string, 0, len(list))
	for _, res := range list {
		ids = append(ids, res.ID)
		uploaderIDs = append(uploaderIDs, res.UploaderID)
	}

	users, err := s.users.GetByIDs(ctx, uploaderIDs)
	if err != nil {
		return nil, err
	}
	userByID := make(map[string]*user.User, len(users))
	for _, u := range users {
		userByID[u.ID] = u
	}

	tagsByRes, err := s.tags.ListByResourceIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	views := make([]*ResourceView, 0, len(list))
	for _, res := range list {
		tags := tagsByRes[res.ID]
		if tags == nil {
			tags = []*tag.Tag{} // JSON 输出 [] 而非 null
		}
		views = append(views, &ResourceView{
			Resource: res,
			Uploader: userByID[res.UploaderID],
			Tags:     tags,
		})
	}
	return views, nil
}
