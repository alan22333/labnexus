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
	fileSize, err := writeDemoFile(uploadDir)
	if err != nil {
		return nil, err
	}
	images, err := buildDemoImages(uploadDir)
	if err != nil {
		return nil, err
	}

	res := &Result{FileResourcePath: demoFileRel, FileResourceSize: fileSize}
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
			users: map[string]*user.User{}, spaces: map[string]*space.Space{}, taskTypes: map[string]string{},
		}
		if err := s.loadTaskTypes(); err != nil {
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
		if err := s.seedResources(fileSize); err != nil {
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

// writeDemoFile 写入演示文件资源(覆盖旧文件,保证幂等),返回字节数。
func writeDemoFile(uploadDir string) (int64, error) {
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return 0, fmt.Errorf("create upload dir: %w", err)
	}
	content := "LabNexus 演示文件资源\n" +
		"========================================\n" +
		"这是 make seed-demo 自动生成的示例文件,用于体验文件资源的下载与预览。\n\n" +
		"本周组会纪要(示例):\n" +
		"1. 张伟:扩散模型采样加速实验进展,已完成基线复现;\n" +
		"2. 王芳:文献综述初稿完成,待补充近三个月工作;\n" +
		"3. 陈浩:数据集清洗完成,CIFAR-10 子集可直接用于训练;\n" +
		"4. 下周重点:确定改进方案并开始消融实验。\n"
	path := filepath.Join(uploadDir, demoFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return 0, fmt.Errorf("write demo file: %w", err)
	}
	return int64(len(content)), nil
}

// ---------- seeder ----------

type seeder struct {
	tx        *gorm.DB
	res       *Result
	images    []demoImage
	users     map[string]*user.User
	spaces    map[string]*space.Space
	taskTypes map[string]string
	tags      map[string]*tag.Tag
	docs      map[string]*document.Document
	resources map[string]*resource.Resource
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

func (s *seeder) doc(username string, folderID *string, title, content, visibility string, pinned bool) (*document.Document, error) {
	d := document.NewDocument(s.users[username].ID, s.spaces[username].ID, folderID, title, content, visibility)
	d.Pinned = pinned
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

func (s *seeder) comment(docID, username, content string, replyTo *string) (*document.Comment, error) {
	c := document.NewComment(docID, s.users[username].ID, content, replyTo)
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
		key     string
		user    string
		folder  *string
		title   string
		content string
		pinned  bool
		tags    []string
	}{
		{"meeting", SupervisorUser, nil, "组会安排:本周五 15:00 306 会议室", "本周组会安排如下:\n- 时间:周五 15:00\n- 地点:306 会议室\n- 汇报顺序:张伟、王芳、陈浩\n\n请各位提前把 PPT 发到群里。", true, []string{"组会"}},
		{"sampling", "student1", nil, "本周汇报:扩散模型采样加速实验进展", "已完成 DDIM / DPM-Solver 基线复现,采样步数从 1000 降到 50,质量下降在可接受范围。\n\n下一步:\n1. 尝试一致性模型蒸馏;\n2. 补充消融实验。", true, []string{"实验", "深度学习"}},
		{"paper", "student2", &litFolder.ID, "文献分享:Classifier-Free Diffusion Guidance", "这篇是 CFG 的原始工作,核心是用同一个模型同时训练条件与无条件分支,推理时线性组合。\n\n值得注意的细节:训练时以 10%~20% 概率丢弃条件。", false, []string{"文献", "深度学习"}},
		{"dataset", "student3", nil, "数据集整理:CIFAR-10 清洗完成", "清洗后的 CIFAR-10 子集已放到共享盘,标注去重完成,可直接用于训练。\n\n坐标:shared/datasets/cifar10-clean-v1", false, []string{"数据集"}},
		{"writing", "student1", &paperFolder.ID, "投稿准备:AAAI 2027 时间线", "摘要截止:2026-08-15\n全文截止:2026-08-22\n\n当前进度:方法部分完成 60%,实验部分待补。", false, []string{"论文写作"}},
	}
	for _, p := range posts {
		d, err := s.doc(p.user, p.folder, p.title, p.content, document.VisibilityPublic, p.pinned)
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
		key     string
		user    string
		folder  *string
		title   string
		content string
		tags    []string
	}{
		{"exp-note", "student1", &expFolder.ID, "实验记录 2026-10-12", "配置:batch=64, lr=1e-4, steps=50k\n\n现象:step 30k 后 loss 震荡,怀疑学习率过大。\n待办:尝试 cosine schedule。", []string{"实验"}},
		{"ddpm-note", "student2", &litFolder.ID, "读书笔记:DDPM 前向/反向过程推导", "前向:q(x_t|x_0) 可以直接写闭式。\n反向:用网络预测噪声 eps,再重参数化采样。\n\n关键:变分下界可化简为噪声预测的 MSE。", []string{"文献", "深度学习"}},
	}
	for _, n := range notes {
		d, err := s.doc(n.user, n.folder, n.title, n.content, document.VisibilityPrivate, false)
		if err != nil {
			return err
		}
		if err := s.tagDoc(d.ID, n.tags...); err != nil {
			return err
		}
		s.docs[n.key] = d
	}

	// 评论 + 回复 + 点赞
	c1, err := s.comment(s.docs["sampling"].ID, SupervisorUser, "基线复现得不错,注意对比不同采样器的 FID。", nil)
	if err != nil {
		return err
	}
	if _, err := s.comment(s.docs["sampling"].ID, "student2", "收到,我这周补 FID 对比表。", &c1.ID); err != nil {
		return err
	}
	if _, err := s.comment(s.docs["paper"].ID, "student1", "这篇的 code 在 GitHub 上有非官方实现,可以直接跑。", nil); err != nil {
		return err
	}
	if _, err := s.comment(s.docs["dataset"].ID, "student4", "已下载,感谢整理!", nil); err != nil {
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

func (s *seeder) resourceLink(title, desc, url, uploader string, tagNames ...string) (*resource.Resource, error) {
	r := resource.NewResource(resource.TypeLink, title, s.users[uploader].ID)
	r.Description = desc
	r.URL = url
	if err := s.tx.Create(r).Error; err != nil {
		return nil, fmt.Errorf("create link resource: %w", err)
	}
	for _, name := range tagNames {
		link := resource.ResourceTag{ResourceID: r.ID, TagID: s.tags[name].ID}
		if err := s.tx.Create(&link).Error; err != nil {
			return nil, fmt.Errorf("tag resource: %w", err)
		}
	}
	s.res.Resources++
	return r, nil
}

func (s *seeder) resourceFile(title, desc, originalName, mime string, size int64, uploader string, tagNames ...string) (*resource.Resource, error) {
	return s.resourceFileAt(demoFileRel, title, desc, originalName, mime, size, uploader, tagNames...)
}

// resourceFileAt 与 resourceFile 一致,但允许指定落盘路径(演示图片用)。
func (s *seeder) resourceFileAt(fileRel, title, desc, originalName, mime string, size int64, uploader string, tagNames ...string) (*resource.Resource, error) {
	r := resource.NewResource(resource.TypeFile, title, s.users[uploader].ID)
	r.Description = desc
	r.FilePath = fileRel
	r.OriginalName = originalName
	r.MimeType = mime
	r.FileSize = size
	if err := s.tx.Create(r).Error; err != nil {
		return nil, fmt.Errorf("create file resource: %w", err)
	}
	for _, name := range tagNames {
		link := resource.ResourceTag{ResourceID: r.ID, TagID: s.tags[name].ID}
		if err := s.tx.Create(&link).Error; err != nil {
			return nil, fmt.Errorf("tag resource: %w", err)
		}
	}
	s.res.Resources++
	return r, nil
}

func (s *seeder) seedResources(fileSize int64) error {
	s.resources = map[string]*resource.Resource{}
	links := []struct {
		key, title, desc, url, uploader string
		tags                            []string
	}{
		{"attention", "Attention Is All You Need (arXiv)", "Transformer 原始论文,组会必读。", "https://arxiv.org/abs/1706.03762", SupervisorUser, []string{"文献", "深度学习"}},
		{"pytorch", "PyTorch 官方文档", "训练/分布式相关文档入口。", "https://pytorch.org/docs/stable/index.html", "student1", []string{"实验"}},
		{"paperswithcode", "Papers with Code - 图像生成", "图像生成任务的榜单与代码合集。", "https://paperswithcode.com/task/image-generation", "student3", []string{"文献", "数据集"}},
		{"ddim", "DDIM 论文 (arXiv)", "确定性采样,采样加速的经典工作。", "https://arxiv.org/abs/2010.02502", "student1", []string{"文献", "深度学习"}},
	}
	for _, l := range links {
		r, err := s.resourceLink(l.title, l.desc, l.url, l.uploader, l.tags...)
		if err != nil {
			return err
		}
		s.resources[l.key] = r
	}
	f, err := s.resourceFile("本周组会纪要(示例文件)", "演示文件资源的下载与预览。", demoFileName, "text/plain; charset=utf-8", fileSize, SupervisorUser, "组会")
	if err != nil {
		return err
	}
	s.resources["meeting-file"] = f

	// 图片资源:可在资源库卡片与信息流帖子中直接查看
	for _, def := range s.images {
		img, err := s.resourceFileAt(
			def.relPath, def.title, def.description, def.originalName,
			"image/png", def.size, def.uploader, def.tags...,
		)
		if err != nil {
			return err
		}
		s.resources["image:"+def.file] = img
		s.res.Images++
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

func daysFromNow(n int) string {
	return time.Now().AddDate(0, 0, n).Format("2006-01-02")
}
