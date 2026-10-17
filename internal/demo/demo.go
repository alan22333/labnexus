// Package demo 提供开发/演示用的高保真种子数据。
//
// 目标(见 docs/specs/dev-demo-data.md):
//   - `make seed-demo` 一键重建确定性演示数据(覆盖 F1~F10 与管理后台);
//   - 开发环境启动时,若库中无任何业务用户,自动补种,保证重启后即可登录体验。
//
// 本包只做数据写入,不新增/变更 HTTP 端点、契约或 schema。
package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"labnexus/internal/document"
	"labnexus/internal/finance"
	"labnexus/internal/project"
	"labnexus/internal/resource"
	"labnexus/internal/space"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// 固定演示账号与密码(密码经 bcrypt 入库)。
const (
	AdminUsername  = "test_admin"
	AdminPassword  = "Test@123456"
	SupervisorUser = "supervisor"
	SupervisorPass = "Demo@123456"
	StudentPass    = "Demo@123456"
)

// DefaultUploadDir 与 internal/app 中 LocalFileStore 使用的目录保持一致。
const DefaultUploadDir = "data/uploads"

const (
	demoFileName = "demo-seed-group-meeting.txt"
	demoFileRel  = "uploads/" + demoFileName

	demoWeeklyTemplateName = "demo-weekly-report-template.txt"
	demoWeeklyTemplateRel  = "uploads/" + demoWeeklyTemplateName

	demoExpTemplateName = "demo-experiment-record-template.txt"
	demoExpTemplateRel  = "uploads/" + demoExpTemplateName
)

// DemoPasswordFor 返回指定演示账号的登录密码(供日志与文档展示)。
func DemoPasswordFor(username string) string {
	if username == AdminUsername {
		return AdminPassword
	}
	return SupervisorPass
}

// businessTables 需要清空的业务表(顺序无关,统一 CASCADE 兜底)。
// 与 test/integration 的 truncateTables 保持一致,保证演示数据"可重置"。
var businessTables = []string{
	"document_tags", "reactions", "comments", "documents",
	"folders", "spaces", "invite_codes", "tags", "users",
	"resource_tags", "resources",
	"task_links", "tasks", "milestones", "project_members", "projects", "task_types",
	"turnover_submissions", "turnover_items", "participants",
	"turnover_batches", "transactions", "accounts",
}

// Result 汇总种子写入的关键数量,用于日志与测试断言。
type Result struct {
	Users            int
	Tags             int
	Documents        int
	PublicDocuments  int
	PrivateDocuments int
	Comments         int
	Reactions        int
	Resources        int
	Images           int
	Favorites        int
	FileResourcePath string
	FileResourceSize int64
	Projects         int
	Tasks            int
	Milestones       int
	TurnoverBatches  int
	Transactions     int
}

// IsEmpty 判断业务库是否没有任何用户(开发环境自动补种的触发条件)。
func IsEmpty(ctx context.Context, db *gorm.DB) (bool, error) {
	var count int64
	if err := db.WithContext(ctx).Table("users").Count(&count).Error; err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count == 0, nil
}

// Seed 清空全部业务表并写入确定性演示数据;uploadDir 为空时使用 DefaultUploadDir。
// 可重复执行,结果一致。
func Seed(ctx context.Context, db *gorm.DB, uploadDir string) (*Result, error) {
	if uploadDir == "" {
		uploadDir = DefaultUploadDir
	}
	fileSizes, err := writeDemoFiles(uploadDir)
	if err != nil {
		return nil, err
	}
	images, err := buildDemoImages(uploadDir)
	if err != nil {
		return nil, err
	}

	res := &Result{FileResourcePath: demoFileRel, FileResourceSize: fileSizes[demoFileRel]}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := truncateBusinessTables(tx); err != nil {
			return err
		}
		// 内置任务类型字典(与 app.Build 一致)。
		if err := project.SeedDefaultTaskTypes(ctx, tx); err != nil {
			return fmt.Errorf("seed task types: %w", err)
		}
		s := &seeder{
			tx: tx, res: res, images: images,
			users: map[string]*user.User{}, spaces: map[string]*space.Space{},
			taskTypes: map[string]string{}, categories: map[string]string{},
		}
		if err := s.loadTaskTypes(); err != nil {
			return err
		}
		if err := s.loadCategories(); err != nil {
			return err
		}
		if err := s.seedUsers(); err != nil {
			return err
		}
		if err := s.seedInviteCodes(); err != nil {
			return err
		}
		if err := s.seedTags(); err != nil {
			return err
		}
		if err := s.seedDocuments(); err != nil {
			return err
		}
		if err := s.seedResources(fileSizes); err != nil {
			return err
		}
		if err := s.attachPostImages(); err != nil {
			return err
		}
		if err := s.seedProjects(); err != nil {
			return err
		}
		return s.seedFinance()
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func truncateBusinessTables(tx *gorm.DB) error {
	for _, tbl := range businessTables {
		if err := tx.Exec("TRUNCATE TABLE " + tbl + " CASCADE").Error; err != nil {
			return fmt.Errorf("truncate %s: %w", tbl, err)
		}
	}
	return nil
}

// writeDemoFiles 写入演示文本文件资源(覆盖旧文件,保证幂等),返回相对路径→字节数。
func writeDemoFiles(uploadDir string) (map[string]int64, error) {
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	defs := []struct {
		rel     string
		name    string
		content string
	}{
		{demoFileRel, demoFileName,
			"LabNexus 演示文件资源\n" +
				"========================================\n" +
				"这是 make seed-demo 自动生成的示例文件,用于体验文件资源的下载与预览。\n\n" +
				"本周组会纪要(示例):\n" +
				"1. 张伟:扩散模型采样加速实验进展,已完成基线复现;\n" +
				"2. 王芳:文献综述初稿完成,待补充近三个月工作;\n" +
				"3. 陈浩:数据集清洗完成,CIFAR-10 子集可直接用于训练;\n" +
				"4. 下周重点:确定改进方案并开始消融实验。\n"},
		{demoWeeklyTemplateRel, demoWeeklyTemplateName,
			"周报模板 v3(组内统一格式)\n" +
				"========================================\n" +
				"姓名:___________  周期:____ 月 ____ 日 ~ ____ 月 ____ 日\n\n" +
				"1. 本周进展(按任务列,标注完成度)\n" +
				"   - \n2. 遇到的问题 / 需要的支持\n" +
				"   - \n3. 下周计划\n" +
				"   - \n4. 数据与代码\n" +
				"   - 实验记录链接 / W&B run 链接:\n"},
		{demoExpTemplateRel, demoExpTemplateName,
			"实验记录模板(四段式)\n" +
				"========================================\n" +
				"【配置】模型 / 数据集 / 超参 / 随机种子\n\n" +
				"【现象】观察到的主要结果与关键指标(FID / PSNR / 准确率…)\n\n" +
				"【结论】与基线对比,是否支持当前假设\n\n" +
				"【待办】下一步要验证的消融或方向\n"},
	}
	sizes := map[string]int64{}
	for _, d := range defs {
		base := strings.TrimPrefix(d.rel, "uploads/")
		path := filepath.Join(uploadDir, base)
		if err := os.WriteFile(path, []byte(d.content), 0o644); err != nil {
			return nil, fmt.Errorf("write demo file %s: %w", d.name, err)
		}
		sizes[d.rel] = int64(len(d.content))
	}
	return sizes, nil
}

// ---------- seeder ----------

type seeder struct {
	tx         *gorm.DB
	res        *Result
	images     []demoImage
	users      map[string]*user.User
	spaces     map[string]*space.Space
	taskTypes  map[string]string
	categories map[string]string
	tags       map[string]*tag.Tag
	docs       map[string]*document.Document
	resources  map[string]*resource.Resource
}

func (s *seeder) loadTaskTypes() error {
	var list []project.TaskType
	if err := s.tx.Find(&list).Error; err != nil {
		return fmt.Errorf("load task types: %w", err)
	}
	for i := range list {
		s.taskTypes[list[i].Name] = list[i].ID
	}
	return nil
}

// loadCategories 读取迁移内置的资源分类字典(name → id),供资源分配分类。
func (s *seeder) loadCategories() error {
	var list []resource.ResourceCategory
	if err := s.tx.Find(&list).Error; err != nil {
		return fmt.Errorf("load resource categories: %w", err)
	}
	for i := range list {
		s.categories[list[i].Name] = list[i].ID
	}
	return nil
}

func (s *seeder) seedUsers() error {
	type spec struct {
		username string
		display  string
		role     string
		password string
	}
	specs := []spec{
		{AdminUsername, "测试管理员", user.RoleAdmin, AdminPassword},
		{SupervisorUser, "李文博(导师)", user.RoleSupervisor, SupervisorPass},
		{"student1", "张伟", user.RoleStudent, StudentPass},
		{"student2", "王芳", user.RoleStudent, StudentPass},
		{"student3", "陈浩", user.RoleStudent, StudentPass},
		{"student4", "刘洋", user.RoleStudent, StudentPass},
	}
	for _, sp := range specs {
		hash, err := bcrypt.GenerateFromPassword([]byte(sp.password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		u := user.NewUser(sp.username, sp.display, string(hash))
		u.Role = sp.role
		if err := s.tx.Create(u).Error; err != nil {
			return fmt.Errorf("create user %s: %w", sp.username, err)
		}
		spc := space.NewSpace(u.ID)
		if err := s.tx.Create(spc).Error; err != nil {
			return fmt.Errorf("create space for %s: %w", sp.username, err)
		}
		s.users[sp.username] = u
		s.spaces[sp.username] = spc
		s.res.Users++
	}
	return nil
}

func (s *seeder) seedInviteCodes() error {
	admin := s.users[AdminUsername]
	codes := []string{"WELCOME-2026", "JOIN-LABNEXUS"}
	for _, code := range codes {
		ic := user.NewInviteCode(admin.ID, code, nil)
		if err := s.tx.Create(ic).Error; err != nil {
			return fmt.Errorf("create invite code %s: %w", code, err)
		}
	}
	return nil
}

func (s *seeder) seedTags() error {
	s.tags = map[string]*tag.Tag{}
	defs := []struct {
		name  string
		color string
	}{
		{"文献", "#8b5cf6"},
		{"实验", "#3b82f6"},
		{"组会", "#f59e0b"},
		{"深度学习", "#10b981"},
		{"数据集", "#06b6d4"},
		{"论文写作", "#ef4444"},
	}
	for _, d := range defs {
		t := tag.NewTag(d.name, d.color)
		if err := s.tx.Create(t).Error; err != nil {
			return fmt.Errorf("create tag %s: %w", d.name, err)
		}
		s.tags[d.name] = t
		s.res.Tags++
	}
	return nil
}

func (s *seeder) folder(username, name string, parent *string, order int) (*space.Folder, error) {
	f := space.NewFolder(s.spaces[username].ID, parent, name, order)
	if err := s.tx.Create(f).Error; err != nil {
		return nil, fmt.Errorf("create folder %s: %w", name, err)
	}
	return f, nil
}

func (s *seeder) doc(username string, folderID *string, title, content, visibility string, pinned bool, ageHours int) (*document.Document, error) {
	d := document.NewDocument(s.users[username].ID, s.spaces[username].ID, folderID, title, content, visibility)
	d.Pinned = pinned
	if ageHours > 0 {
		d.CreatedAt = hoursAgo(ageHours)
		d.UpdatedAt = d.CreatedAt
	}
	if err := s.tx.Create(d).Error; err != nil {
		return nil, fmt.Errorf("create document %q: %w", title, err)
	}
	s.res.Documents++
	if visibility == document.VisibilityPublic {
		s.res.PublicDocuments++
	} else {
		s.res.PrivateDocuments++
	}
	return d, nil
}

func (s *seeder) tagDoc(docID string, tagNames ...string) error {
	for _, name := range tagNames {
		link := document.DocumentTag{DocumentID: docID, TagID: s.tags[name].ID}
		if err := s.tx.Create(&link).Error; err != nil {
			return fmt.Errorf("tag document: %w", err)
		}
	}
	return nil
}

func (s *seeder) comment(docID, username, content string, replyTo *string, ageHours int) (*document.Comment, error) {
	c := document.NewComment(docID, s.users[username].ID, content, replyTo)
	if ageHours > 0 {
		c.CreatedAt = hoursAgo(ageHours)
	}
	if err := s.tx.Create(c).Error; err != nil {
		return nil, fmt.Errorf("create comment: %w", err)
	}
	s.res.Comments++
	return c, nil
}

func (s *seeder) reaction(docID, username, emoji string) error {
	r := document.NewReaction(docID, s.users[username].ID, emoji)
	if err := s.tx.Create(r).Error; err != nil {
		return fmt.Errorf("create reaction: %w", err)
	}
	s.res.Reactions++
	return nil
}

func (s *seeder) seedDocuments() error {
	s.docs = map[string]*document.Document{}

	// 张伟:空间内目录树
	expFolder, err := s.folder("student1", "实验记录", nil, 1)
	if err != nil {
		return err
	}
	litFolder, err := s.folder("student1", "文献阅读", nil, 2)
	if err != nil {
		return err
	}
	paperFolder, err := s.folder("student1", "论文草稿", nil, 3)
	if err != nil {
		return err
	}

	// 公开帖子(信息流)
	posts := []struct {
		key      string
		user     string
		folder   *string
		title    string
		content  string
		pinned   bool
		ageHours int
		tags     []string
	}{
		{"meeting", SupervisorUser, nil, "组会安排:本周五 15:00 306 会议室", "本周组会安排如下:\n- 时间:周五 15:00\n- 地点:306 会议室\n- 汇报顺序:张伟、王芳、陈浩\n\n请各位提前把 PPT 发到群里。", true, 6, []string{"组会"}},
		{"sampling", "student1", nil, "本周汇报:扩散模型采样加速实验进展", "已完成 DDIM / DPM-Solver 基线复现,采样步数从 1000 降到 50,质量下降在可接受范围。\n\n下一步:\n1. 尝试一致性模型蒸馏;\n2. 补充消融实验。", true, 22, []string{"实验", "深度学习"}},
		{"paper", "student2", &litFolder.ID, "文献分享:Classifier-Free Diffusion Guidance", "这篇是 CFG 的原始工作,核心是用同一个模型同时训练条件与无条件分支,推理时线性组合。\n\n值得注意的细节:训练时以 10%~20% 概率丢弃条件。", false, 24*3 + 2, []string{"文献", "深度学习"}},
		{"dataset", "student3", nil, "数据集整理:CIFAR-10 清洗完成", "清洗后的 CIFAR-10 子集已放到共享盘,标注去重完成,可直接用于训练。\n\n坐标:shared/datasets/cifar10-clean-v1", false, 24*5 + 7, []string{"数据集"}},
		{"writing", "student1", &paperFolder.ID, "投稿准备:AAAI 2027 时间线", "摘要截止:2026-08-15\n全文截止:2026-08-22\n\n当前进度:方法部分完成 60%,实验部分待补。", false, 24*8 + 4, []string{"论文写作"}},
	}
	for _, p := range posts {
		d, err := s.doc(p.user, p.folder, p.title, p.content, document.VisibilityPublic, p.pinned, p.ageHours)
		if err != nil {
			return err
		}
		if err := s.tagDoc(d.ID, p.tags...); err != nil {
			return err
		}
		s.docs[p.key] = d
	}

	// 私密笔记
	notes := []struct {
		key      string
		user     string
		folder   *string
		title    string
		content  string
		ageHours int
		tags     []string
	}{
		{"exp-note", "student1", &expFolder.ID, "实验记录 2026-10-12", "配置:batch=64, lr=1e-4, steps=50k\n\n现象:step 30k 后 loss 震荡,怀疑学习率过大。\n待办:尝试 cosine schedule。", 24*2 + 3, []string{"实验"}},
		{"ddpm-note", "student2", &litFolder.ID, "读书笔记:DDPM 前向/反向过程推导", "前向:q(x_t|x_0) 可以直接写闭式。\n反向:用网络预测噪声 eps,再重参数化采样。\n\n关键:变分下界可化简为噪声预测的 MSE。", 24*6 + 9, []string{"文献", "深度学习"}},
	}
	for _, n := range notes {
		d, err := s.doc(n.user, n.folder, n.title, n.content, document.VisibilityPrivate, false, n.ageHours)
		if err != nil {
			return err
		}
		if err := s.tagDoc(d.ID, n.tags...); err != nil {
			return err
		}
		s.docs[n.key] = d
	}

	// 评论 + 回复 + 点赞
	c1, err := s.comment(s.docs["sampling"].ID, SupervisorUser, "基线复现得不错,注意对比不同采样器的 FID。", nil, 18)
	if err != nil {
		return err
	}
	if _, err := s.comment(s.docs["sampling"].ID, "student2", "收到,我这周补 FID 对比表。", &c1.ID, 15); err != nil {
		return err
	}
	if _, err := s.comment(s.docs["paper"].ID, "student1", "这篇的 code 在 GitHub 上有非官方实现,可以直接跑。", nil, 24*3-3); err != nil {
		return err
	}
	if _, err := s.comment(s.docs["dataset"].ID, "student4", "已下载,感谢整理!", nil, 24*5+2); err != nil {
		return err
	}
	reactions := []struct {
		doc, user, emoji string
	}{
		{"sampling", SupervisorUser, "👍"},
		{"sampling", "student2", "🎉"},
		{"sampling", "student3", "👍"},
		{"paper", "student1", "👍"},
		{"dataset", "student1", "👍"},
		{"dataset", "student2", "🔥"},
		{"meeting", "student3", "👍"},
	}
	for _, r := range reactions {
		if err := s.reaction(s.docs[r.doc].ID, r.user, r.emoji); err != nil {
			return err
		}
	}
	return nil
}

// resourceSpec 描述一条种子资源的完整字段(含资源库 v2 的分类/置顶/计数/时间线)。
type resourceSpec struct {
	key       string
	typ       string
	title     string
	desc      string
	url       string
	file      string
	origName  string
	mime      string
	size      int64
	uploader  string
	category  string
	tags      []string
	pinned    bool
	views     int64
	downloads int64
	ageHours  int
}

// createResource 按规格写入一条资源(含分类/置顶/计数/时间线与标签)。
func (s *seeder) createResource(sp resourceSpec) (*resource.Resource, error) {
	r := resource.NewResource(sp.typ, sp.title, s.users[sp.uploader].ID)
	r.Description = sp.desc
	if sp.typ == resource.TypeLink {
		r.URL = sp.url
	} else {
		r.FilePath = sp.file
		r.OriginalName = sp.origName
		r.MimeType = sp.mime
		r.FileSize = sp.size
	}
	if id, ok := s.categories[sp.category]; ok {
		r.CategoryID = &id
	}
	r.Pinned = sp.pinned
	r.ViewCount = sp.views
	r.DownloadCount = sp.downloads
	if sp.ageHours > 0 {
		r.CreatedAt = hoursAgo(sp.ageHours)
		r.UpdatedAt = r.CreatedAt
	}
	if err := s.tx.Create(r).Error; err != nil {
		return nil, fmt.Errorf("create resource %q: %w", sp.title, err)
	}
	for _, name := range sp.tags {
		link := resource.ResourceTag{ResourceID: r.ID, TagID: s.tags[name].ID}
		if err := s.tx.Create(&link).Error; err != nil {
			return nil, fmt.Errorf("tag resource %q: %w", sp.title, err)
		}
	}
	s.res.Resources++
	return r, nil
}

// linkResourceSpecs 返回全部链接资源的纯规格定义(不依赖 DB,可单测)。
func linkResourceSpecs() []resourceSpec {
	// 链接资源:覆盖 6 个分类,时间线从数小时前到数周前。
	return []resourceSpec{
		{
			key: "attention", typ: resource.TypeLink,
			title: "Attention Is All You Need (arXiv)", desc: "Transformer 原始论文,组会必读,建议先看 §3.2 多头注意力推导。",
			url: "https://arxiv.org/abs/1706.03762", uploader: SupervisorUser, category: "文献",
			tags: []string{"文献", "深度学习"}, pinned: true, views: 268, ageHours: 24*26 + 2,
		},
		{
			key: "ddim", typ: resource.TypeLink,
			title: "DDIM: Denoising Diffusion Implicit Models (arXiv)", desc: "确定性采样加速的经典工作,步数可从 1000 降到 50 而质量损失可控。",
			url: "https://arxiv.org/abs/2010.02502", uploader: "student1", category: "文献",
			tags: []string{"文献", "深度学习"}, pinned: true, views: 143, ageHours: 24*9 + 5,
		},
		{
			key: "cfg", typ: resource.TypeLink,
			title: "Classifier-Free Diffusion Guidance (arXiv)", desc: "CFG 原始论文,训练时以 10%~20% 概率丢弃条件,推理时线性组合。",
			url: "https://arxiv.org/abs/2207.12598", uploader: "student2", category: "文献",
			tags: []string{"文献", "深度学习"}, views: 96, ageHours: 24*6 + 3,
		},
		{
			key: "consistency", typ: resource.TypeLink,
			title: "Consistency Models (arXiv)", desc: "一致性模型:单步/少步生成的代表工作,蒸馏路线的主要参考。",
			url: "https://arxiv.org/abs/2303.01469", uploader: "student2", category: "文献",
			tags: []string{"文献", "深度学习"}, views: 71, ageHours: 24*3 + 8,
		},
		{
			key: "paperswithcode", typ: resource.TypeLink,
			title: "Papers with Code - Image Generation", desc: "图像生成任务的榜单与开源实现合集,找基线/对比结果很方便。",
			url: "https://paperswithcode.com/task/image-generation", uploader: "student3", category: "数据集",
			tags: []string{"数据集", "文献"}, views: 58, ageHours: 24*4 + 11,
		},
		{
			key: "pytorch", typ: resource.TypeLink,
			title: "PyTorch 官方文档", desc: "训练/分布式相关的权威入口,版本升级先查这里。",
			url: "https://pytorch.org/docs/stable/index.html", uploader: "student1", category: "工具",
			tags: []string{"实验"}, views: 187, ageHours: 24*21 + 6,
		},
		{
			key: "wandb", typ: resource.TypeLink,
			title: "Weights & Biases 实验记录指南", desc: "组内统一用 W&B 记录实验,这里是命名与对比的约定。",
			url: "https://docs.wandb.ai/", uploader: "student1", category: "工具",
			tags: []string{"实验"}, views: 44, ageHours: 24*2 + 2,
		},
		{
			key: "diffusers", typ: resource.TypeLink,
			title: "Hugging Face Diffusers 源码", desc: "扩散模型工程实现参考,采样器集中在 src/diffusers/schedulers。",
			url: "https://github.com/huggingface/diffusers", uploader: "student4", category: "代码",
			tags: []string{"深度学习"}, views: 63, ageHours: 24*7 + 6,
		},
		{
			key: "aaai", typ: resource.TypeLink,
			title: "AAAI 2027 投稿时间线与注意事项", desc: "投稿节点、匿名要求与格式检查清单,组内投稿统一参考。",
			url: "https://aaai.org/conference/aaai/aaai-27/", uploader: "student1", category: "其他",
			tags: []string{"论文写作"}, views: 31, ageHours: 24*11 + 3,
		},
		{
			key: "shared-drive", typ: resource.TypeLink,
			title: "共享盘目录说明与访问方式", desc: "组内共享盘目录约定与挂载方式,数据/代码统一放这里。",
			url: "https://example.internal/labnexus/shared-drive", uploader: "student3", category: "其他",
			tags: []string{"数据集"}, views: 52, ageHours: 24*15 + 9,
		},
	}
}

// fileResourceSpecs 返回全部文件资源的纯规格定义(不依赖 DB,可单测)。
func fileResourceSpecs(fileSizes map[string]int64) []resourceSpec {
	// 文件资源:本地落盘,可下载/预览。
	return []resourceSpec{
		{
			key: "meeting-file", typ: resource.TypeFile,
			title: "本周组会纪要(2026-10-16)", desc: "本周组会纪要,含各成员汇报要点与下周安排。",
			file: demoFileRel, origName: demoFileName, mime: "text/plain; charset=utf-8", size: fileSizes[demoFileRel],
			uploader: SupervisorUser, category: "模板", tags: []string{"组会"},
			views: 39, downloads: 12, ageHours: 30,
		},
		{
			key: "weekly-template", typ: resource.TypeFile,
			title: "周报模板(v3)", desc: "组内周报统一模板:本周进展 / 遇到的问题 / 下周计划 / 数据与代码。",
			file: demoWeeklyTemplateRel, origName: demoWeeklyTemplateName, mime: "text/plain; charset=utf-8", size: fileSizes[demoWeeklyTemplateRel],
			uploader: "student2", category: "模板", tags: []string{"组会"},
			views: 84, downloads: 27, ageHours: 24*12 + 4,
		},
		{
			key: "exp-template", typ: resource.TypeFile,
			title: "实验记录模板(四段式)", desc: "实验记录规范模板:配置 / 现象 / 结论 / 待办四段式。",
			file: demoExpTemplateRel, origName: demoExpTemplateName, mime: "text/plain; charset=utf-8", size: fileSizes[demoExpTemplateRel],
			uploader: "student1", category: "模板", tags: []string{"实验"},
			views: 66, downloads: 19, ageHours: 24*5 + 4,
		},
	}
}

// imageResourceSpec 把演示图片转换为资源规格(纯函数,可单测)。
func imageResourceSpec(def demoImage) resourceSpec {
	return resourceSpec{
		key: "image:" + def.file, typ: resource.TypeFile,
		title: def.title, desc: def.description,
		file: def.relPath, origName: def.originalName, mime: "image/png", size: def.size,
		uploader: def.uploader, category: def.category, tags: def.tags,
		views: def.views, downloads: def.downloads, ageHours: def.ageHours,
	}
}

func (s *seeder) seedResources(fileSizes map[string]int64) error {
	s.resources = map[string]*resource.Resource{}

	links := linkResourceSpecs()
	files := fileResourceSpecs(fileSizes)

	specs := make([]resourceSpec, 0, len(links)+len(files)+len(s.images))
	specs = append(specs, links...)
	specs = append(specs, files...)

	// 图片资源:可在资源库卡片与信息流帖子中直接查看。
	for _, def := range s.images {
		specs = append(specs, imageResourceSpec(def))
		s.res.Images++
	}

	for _, sp := range specs {
		r, err := s.createResource(sp)
		if err != nil {
			return err
		}
		if sp.key != "" {
			s.resources[sp.key] = r
		}
	}

	return s.seedFavorites()
}

// seedFavorites 为演示账号写入个人收藏,让“我的收藏”有真实内容。
func (s *seeder) seedFavorites() error {
	favs := []struct{ user, key string }{
		{"student1", "attention"},
		{"student1", "ddim"},
		{"student1", "pytorch"},
		{"student2", "attention"},
		{"student2", "consistency"},
		{"student2", "weekly-template"},
		{"student3", "image:" + demoCifarImage},
		{"student4", "diffusers"},
		{SupervisorUser, "image:" + demoSamplingImage},
	}
	for _, f := range favs {
		r, ok := s.resources[f.key]
		if !ok {
			continue
		}
		fav := resource.ResourceFavorite{UserID: s.users[f.user].ID, ResourceID: r.ID, CreatedAt: time.Now()}
		if err := s.tx.Create(&fav).Error; err != nil {
			return fmt.Errorf("create favorite %s→%s: %w", f.user, f.key, err)
		}
		s.res.Favorites++
	}
	return nil
}

// attachPostImages 把已生成的图片资源以 Markdown 形式嵌入对应公开帖。
func (s *seeder) attachPostImages() error {
	added := map[string]string{}
	for _, def := range s.images {
		if def.postKey == "" {
			continue
		}
		res, ok := s.resources["image:"+def.file]
		if !ok {
			continue
		}
		md := fmt.Sprintf("![%s](%s)", strings.NewReplacer("(", "", ")", "").Replace(def.description), previewAPI(res.ID))
		if cur := added[def.postKey]; cur != "" {
			added[def.postKey] = cur + "\n" + md
		} else {
			added[def.postKey] = md
		}
	}
	for key, md := range added {
		doc, ok := s.docs[key]
		if !ok {
			continue
		}
		doc.Content = doc.Content + "\n\n" + md
		if err := s.tx.Model(&document.Document{}).Where("id = ?", doc.ID).Update("content", doc.Content).Error; err != nil {
			return fmt.Errorf("attach post image %s: %w", key, err)
		}
	}
	return nil
}

func previewAPI(resourceID string) string {
	return "/api/resources/" + resourceID + "/preview"
}

func (s *seeder) project(name, desc, owner string, members []string, status string) (*project.Project, error) {
	p := project.NewProject(name, desc, s.users[owner].ID)
	p.Status = status
	if err := s.tx.Create(p).Error; err != nil {
		return nil, fmt.Errorf("create project %s: %w", name, err)
	}
	mem := project.ProjectMember{ProjectID: p.ID, UserID: s.users[owner].ID, Role: "owner"}
	if err := s.tx.Create(&mem).Error; err != nil {
		return nil, err
	}
	for _, m := range members {
		pm := project.ProjectMember{ProjectID: p.ID, UserID: s.users[m].ID, Role: "member"}
		if err := s.tx.Create(&pm).Error; err != nil {
			return nil, err
		}
	}
	s.res.Projects++
	return p, nil
}

func (s *seeder) milestone(projectID, name, due string, completed bool) (*project.Milestone, error) {
	m := &project.Milestone{ID: uuid.NewString(), ProjectID: projectID, Name: name, CreatedAt: time.Now()}
	if due != "" {
		m.DueDate = &due
	}
	if completed {
		now := time.Now()
		m.CompletedAt = &now
	}
	if err := s.tx.Create(m).Error; err != nil {
		return nil, fmt.Errorf("create milestone %s: %w", name, err)
	}
	s.res.Milestones++
	return m, nil
}

func (s *seeder) task(projectID, title, desc, typeName, assignee, status, priority, due string, milestoneID *string) (*project.Task, error) {
	var typeID *string
	if id, ok := s.taskTypes[typeName]; ok {
		typeID = &id
	}
	var assigneeID *string
	if assignee != "" {
		id := s.users[assignee].ID
		assigneeID = &id
	}
	var duePtr *string
	if due != "" {
		duePtr = &due
	}
	t := project.NewTask(projectID, title, desc, typeID, assigneeID, priority, duePtr, milestoneID)
	t.Status = status
	if err := s.tx.Create(t).Error; err != nil {
		return nil, fmt.Errorf("create task %s: %w", title, err)
	}
	s.res.Tasks++
	return t, nil
}

func (s *seeder) seedProjects() error {
	// 项目 A:扩散模型采样加速
	// 项目成员含演示管理员,保证 admin 账号登录后 /projects 有可见内容(契约 GET /projects 仅返回本人成员项目)。
	pa, err := s.project("扩散模型采样加速", "研究扩散模型的快速采样方法,目标在保持生成质量的前提下显著降低采样步数。", SupervisorUser, []string{"student1", "student2", AdminUsername}, project.ProjectStatusActive)
	if err != nil {
		return err
	}
	m1, err := s.milestone(pa.ID, "完成基线复现", daysFromNow(-20), true)
	if err != nil {
		return err
	}
	m2, err := s.milestone(pa.ID, "完成改进方案", daysFromNow(10), false)
	if err != nil {
		return err
	}
	m3, err := s.milestone(pa.ID, "撰写论文初稿", daysFromNow(40), false)
	if err != nil {
		return err
	}
	tasksA := []struct {
		title, desc, typ, assignee, status, priority, due string
		milestone                                         *string
	}{
		{"复现 DDIM 基线", "在 CIFAR-10 上复现 DDIM,记录不同步数下的 FID。", "实验", "student1", project.TaskStatusDone, project.PriorityHigh, daysFromNow(-18), &m1.ID},
		{"复现 DPM-Solver", "对比 DPM-Solver 与 DDIM 的采样质量/速度。", "实验", "student1", project.TaskStatusInProgress, project.PriorityHigh, daysFromNow(-3), &m2.ID},
		{"调研一致性模型", "阅读 consistency models 相关工作并整理笔记。", "文献", "student2", project.TaskStatusDone, project.PriorityMedium, daysFromNow(-10), &m1.ID},
		{"实现蒸馏训练脚本", "基于一致性蒸馏实现训练脚本(待基线稳定后启动)。", "代码", "student2", project.TaskStatusTodo, project.PriorityMedium, daysFromNow(7), &m2.ID},
		{"补充消融实验设计", "确定步数/引导强度/网络规模的消融维度。", "实验", "student1", project.TaskStatusBlocked, project.PriorityHigh, daysFromNow(-1), &m2.ID},
		{"撰写方法章节初稿", "整理方法部分,形成可评审初稿。", "写作", "student1", project.TaskStatusTodo, project.PriorityLow, daysFromNow(30), &m3.ID},
	}
	var samplingTask *project.Task
	for i, t := range tasksA {
		task, err := s.task(pa.ID, t.title, t.desc, t.typ, t.assignee, t.status, t.priority, t.due, t.milestone)
		if err != nil {
			return err
		}
		if i == 0 {
			samplingTask = task
		}
	}

	// 项目 B:实验室知识库建设
	pb, err := s.project("实验室知识库建设", "把组内资料、文献、模板统一沉淀到 LabNexus 资源库,降低重复沟通成本。", "student1", []string{"student2", "student3", "student4"}, project.ProjectStatusActive)
	if err != nil {
		return err
	}
	mb1, err := s.milestone(pb.ID, "资料盘点", daysFromNow(-5), true)
	if err != nil {
		return err
	}
	mb2, err := s.milestone(pb.ID, "分类与标签规范", daysFromNow(14), false)
	if err != nil {
		return err
	}
	tasksB := []struct {
		title, desc, typ, assignee, status, priority, due string
		milestone                                         *string
	}{
		{"盘点现有共享盘资料", "统计共享盘中的常用文档/数据/代码。", "其他", "student3", project.TaskStatusDone, project.PriorityMedium, daysFromNow(-6), &mb1.ID},
		{"制定标签规范", "约定文献/实验/数据集等标签的使用规范。", "写作", "student2", project.TaskStatusInProgress, project.PriorityHigh, daysFromNow(10), &mb2.ID},
		{"整理常用文献清单", "把组内必读文献整理为链接资源。", "文献", "student4", project.TaskStatusTodo, project.PriorityMedium, daysFromNow(5), &mb2.ID},
		{"迁移历史模板", "把实验记录/周报模板迁入资源库。", "其他", "student3", project.TaskStatusBlocked, project.PriorityLow, daysFromNow(-2), &mb2.ID},
	}
	var knowledgeTask *project.Task
	for i, t := range tasksB {
		task, err := s.task(pb.ID, t.title, t.desc, t.typ, t.assignee, t.status, t.priority, t.due, t.milestone)
		if err != nil {
			return err
		}
		if i == 0 {
			knowledgeTask = task
		}
	}

	// 任务关联(多态:文档 / 资源)
	links := []project.TaskLink{
		{TaskID: samplingTask.ID, TargetType: "document", TargetID: s.docs["sampling"].ID},
		{TaskID: samplingTask.ID, TargetType: "resource", TargetID: s.resources["ddim"].ID},
		{TaskID: knowledgeTask.ID, TargetType: "document", TargetID: s.docs["dataset"].ID},
	}
	for _, l := range links {
		if err := s.tx.Create(&l).Error; err != nil {
			return fmt.Errorf("create task link: %w", err)
		}
	}
	return nil
}

func (s *seeder) seedFinance() error {
	admin := s.users[AdminUsername]
	acc := finance.NewAccount("课题组经费账户", "6222-****-****-8888", "演示用资金池账户")
	if err := s.tx.Create(acc).Error; err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	participants := []struct{ name, no string }{
		{"张伟", "2021001"},
		{"王芳", "2021002"},
		{"陈浩", "2021003"},
		{"刘洋", "2021004"},
	}
	pIDs := map[string]string{}
	for _, p := range participants {
		pp := finance.NewParticipant(p.name, p.no, "")
		if err := s.tx.Create(pp).Error; err != nil {
			return fmt.Errorf("create participant %s: %w", p.name, err)
		}
		pIDs[p.name] = pp.ID
	}

	// 批次 1:已归档(全部交清)
	doneBatch := finance.NewBatch("2026-09", "已结清归档", admin.ID)
	doneBatch.Status = finance.BatchStatusDone
	if err := s.tx.Create(doneBatch).Error; err != nil {
		return err
	}
	s.res.TurnoverBatches++
	doneItems := []struct {
		name    string
		payroll int64
		tip     int64
	}{
		{"张伟", 150000, 10000},
		{"王芳", 120000, 8000},
		{"陈浩", 100000, 5000},
	}
	for _, it := range doneItems {
		should := it.payroll - it.tip
		item := finance.NewItem(doneBatch.ID, pIDs[it.name], daysFromNow(-40), it.payroll, 0, it.tip, &should, "", admin.ID)
		item.Returned = should
		if err := s.tx.Create(item).Error; err != nil {
			return fmt.Errorf("create done item: %w", err)
		}
		sub := finance.NewSubmission(item.ID, should, daysFromNow(-35), "已结清", admin.ID)
		if err := s.tx.Create(sub).Error; err != nil {
			return err
		}
		tx := finance.NewTransaction(acc.ID, finance.TxIncome, should, finance.CategoryTurnover, "turnover_item", item.ID, it.name+" 上交(2026-09)", time.Now().AddDate(0, 0, -35), admin.ID)
		if err := s.tx.Create(tx).Error; err != nil {
			return err
		}
		s.res.Transactions++
	}

	// 批次 2:进行中(未交 / 部分交 / 已交)
	activeBatch := finance.NewBatch("2026-10", "本月进行中", admin.ID)
	if err := s.tx.Create(activeBatch).Error; err != nil {
		return err
	}
	s.res.TurnoverBatches++
	activeItems := []struct {
		name     string
		payroll  int64
		tip      int64
		returned int64
	}{
		{"张伟", 150000, 10000, 140000}, // 已交清
		{"王芳", 150000, 10000, 70000},  // 部分交
		{"陈浩", 120000, 8000, 0},       // 未交
	}
	for _, it := range activeItems {
		should := it.payroll - it.tip
		note := ""
		if it.returned == 0 {
			note = "待上交"
		}
		item := finance.NewItem(activeBatch.ID, pIDs[it.name], daysFromNow(-8), it.payroll, 0, it.tip, &should, note, admin.ID)
		item.Returned = it.returned
		if err := s.tx.Create(item).Error; err != nil {
			return fmt.Errorf("create active item: %w", err)
		}
		if it.returned > 0 {
			sub := finance.NewSubmission(item.ID, it.returned, daysFromNow(-5), "", admin.ID)
			if err := s.tx.Create(sub).Error; err != nil {
				return err
			}
			tx := finance.NewTransaction(acc.ID, finance.TxIncome, it.returned, finance.CategoryTurnover, "turnover_item", item.ID, it.name+" 上交(2026-10)", time.Now().AddDate(0, 0, -5), admin.ID)
			if err := s.tx.Create(tx).Error; err != nil {
				return err
			}
			s.res.Transactions++
		}
	}

	// 支出与导师补充(让资金池余额为正)
	expense := finance.NewTransaction(acc.ID, finance.TxExpense, 300000, finance.CategoryLabor, "none", "", "发放劳务费(2026-09)", time.Now().AddDate(0, 0, -38), admin.ID)
	if err := s.tx.Create(expense).Error; err != nil {
		return err
	}
	s.res.Transactions++
	supplement := finance.NewTransaction(acc.ID, finance.TxIncome, 500000, finance.CategorySupplement, "none", "", "导师补充经费", time.Now().AddDate(0, 0, -30), s.users[SupervisorUser].ID)
	if err := s.tx.Create(supplement).Error; err != nil {
		return err
	}
	s.res.Transactions++
	return nil
}

// hoursAgo 返回距今 n 小时的时间,用于生成分散的演示时间线。
func hoursAgo(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * time.Hour)
}

func daysFromNow(n int) string {
	return time.Now().AddDate(0, 0, n).Format("2006-01-02")
}
