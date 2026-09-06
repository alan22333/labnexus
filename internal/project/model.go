// Package project 项目任务域:F9 项目/成员/里程碑/任务(状态机)。
// 依据规格:docs/specs/f9-project.md;契约:api-contract.md §F9。
package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
	"labnexus/internal/user"
)

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("project: not found")

// 任务状态机常量
const (
	TaskStatusTodo       = "todo"
	TaskStatusInProgress = "in_progress"
	TaskStatusBlocked    = "blocked"
	TaskStatusDone       = "done"

	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"

	ProjectStatusActive = "active"
	ProjectStatusDone   = "done"
)

// DefaultTaskTypes 内置任务类型种子(name, color, sort)。
var DefaultTaskTypes = []struct {
	Name  string
	Color string
	Sort  int
}{
	{"实验", "#3b82f6", 1},
	{"文献", "#8b5cf6", 2},
	{"写作", "#10b981", 3},
	{"代码", "#f59e0b", 4},
	{"其他", "#94a3b8", 5},
}

// SeedDefaultTaskTypes 幂等写入内置类型(名已存在则跳过;app.Build 与集成测试重建时调用)。
func SeedDefaultTaskTypes(ctx context.Context, db *gorm.DB) error {
	for _, d := range DefaultTaskTypes {
		var count int64
		if err := db.WithContext(ctx).Model(&TaskType{}).Where("name = ?", d.Name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		ty := NewTaskType(d.Name, d.Color, d.Sort, true)
		if err := db.WithContext(ctx).Create(ty).Error; err != nil {
			return err
		}
	}
	return nil
}

// Project 项目(schema.sql: projects)
type Project struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string    `gorm:"size:100" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	OwnerID     string    `gorm:"type:uuid;index" json:"owner_id"`
	Status      string    `gorm:"size:20;default:active" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewProject 构造项目。
func NewProject(name, description, ownerID string) *Project {
	now := time.Now()
	return &Project{
		ID: uuid.NewString(), Name: name, Description: description,
		OwnerID: ownerID, Status: ProjectStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
}

// ProjectMember 项目成员(schema.sql: project_members;role: owner/member)
type ProjectMember struct {
	ProjectID string `gorm:"type:uuid;primaryKey" json:"project_id"`
	UserID    string `gorm:"type:uuid;primaryKey" json:"user_id"`
	Role      string `gorm:"size:20;default:member" json:"role"`
}

// Milestone 里程碑(schema.sql: milestones)
type Milestone struct {
	ID          string     `gorm:"type:uuid;primaryKey" json:"id"`
	ProjectID   string     `gorm:"type:uuid;index" json:"project_id"`
	Name        string     `gorm:"size:100" json:"name"`
	DueDate     *string    `gorm:"type:date" json:"due_date,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// TaskType 任务类型字典(schema.sql: task_types;课题组级,管理界面随管理后台)
type TaskType struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"size:50;uniqueIndex" json:"name"`
	Color     string    `gorm:"size:20;default:#94a3b8" json:"color"`
	Sort      int       `gorm:"default:0" json:"sort"`
	Builtin   bool      `gorm:"default:false" json:"builtin"`
	Active    bool      `gorm:"default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// NewTaskType 构造任务类型。
func NewTaskType(name, color string, sort int, builtin bool) *TaskType {
	return &TaskType{
		ID: uuid.NewString(), Name: name, Color: color, Sort: sort,
		Builtin: builtin, Active: true, CreatedAt: time.Now(),
	}
}

// Task 任务(schema.sql: tasks;DeletedAt 软删除)
type Task struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	ProjectID   string         `gorm:"type:uuid;index" json:"project_id"`
	Title       string         `gorm:"size:200" json:"title"`
	Description string         `gorm:"type:text" json:"description"`
	TypeID      *string        `gorm:"type:uuid;index" json:"type_id,omitempty"`
	AssigneeID  *string        `gorm:"type:uuid;index" json:"assignee_id,omitempty"`
	Status      string         `gorm:"size:20;default:todo" json:"status"`
	Priority    string         `gorm:"size:10;default:medium" json:"priority"`
	DueDate     *string        `gorm:"type:date" json:"due_date,omitempty"`
	MilestoneID *string        `gorm:"type:uuid" json:"milestone_id,omitempty"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// NewTask 构造任务。
func NewTask(projectID, title, description string, typeID, assigneeID *string, priority string, dueDate *string, milestoneID *string) *Task {
	now := time.Now()
	return &Task{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Description: description,
		TypeID: typeID, AssigneeID: assigneeID, Status: TaskStatusTodo, Priority: priority,
		DueDate: dueDate, MilestoneID: milestoneID,
		CreatedAt: now, UpdatedAt: now,
	}
}

// TaskLink 任务关联(schema.sql: task_links;多态 document/resource)
type TaskLink struct {
	TaskID     string `gorm:"type:uuid;primaryKey" json:"task_id"`
	TargetType string `gorm:"size:20;primaryKey" json:"target_type"`
	TargetID   string `gorm:"type:uuid;primaryKey" json:"target_id"`
}

// ListFilter 任务列表筛选
type ListFilter struct {
	Status      string
	AssigneeID  string
	MilestoneID string
}

// Repository 项目域数据访问接口
type Repository interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	ListProjectsByMember(ctx context.Context, userID string) ([]*Project, error)

	AddMember(ctx context.Context, m *ProjectMember) error
	RemoveMember(ctx context.Context, projectID, userID string) error
	GetMember(ctx context.Context, projectID, userID string) (*ProjectMember, error)
	ListMembers(ctx context.Context, projectID string) ([]*ProjectMember, error)

	CreateMilestone(ctx context.Context, m *Milestone) error
	GetMilestone(ctx context.Context, id string) (*Milestone, error)
	UpdateMilestone(ctx context.Context, m *Milestone) error
	ListMilestones(ctx context.Context, projectID string) ([]*Milestone, error)

	CreateTask(ctx context.Context, t *Task) error
	GetTask(ctx context.Context, id string) (*Task, error)
	UpdateTask(ctx context.Context, t *Task) error
	SoftDeleteTask(ctx context.Context, id string) error
	ListTasks(ctx context.Context, projectID string, f ListFilter) ([]*Task, error)
	// SearchTasks 跨项目搜索任务(标题 ILIKE,用于全局搜索聚合)
	SearchTasks(ctx context.Context, keyword string, limit int) ([]*Task, error)

	LinkTask(ctx context.Context, taskID, targetType, targetID string) error
	ListLinks(ctx context.Context, taskID string) ([]*TaskLink, error)

	// 任务类型字典(F9 v2;candidates/stats 见 specs/f9-project.md §8)
	ListTaskTypes(ctx context.Context) ([]*TaskType, error)
	GetTaskType(ctx context.Context, id string) (*TaskType, error)
	// ListCandidateUsers 返回未入组用户(加成员候选,排除已入组成员)。
	ListCandidateUsers(ctx context.Context, projectID string) ([]*user.User, error)
	// TaskStats 按项目批量统计任务(含过期数);返回 map[projectID]*TaskStats。
	TaskStats(ctx context.Context, projectIDs []string) (map[string]*TaskStats, error)

	// 任务类型字典管理(admin console;specs/admin-console.md §3.3)
	ListAllTaskTypes(ctx context.Context) ([]*TaskType, error)
	CreateTaskType(ctx context.Context, t *TaskType) error
	UpdateTaskType(ctx context.Context, t *TaskType) error
	DeleteTaskType(ctx context.Context, id string) error
	// ClearTasksType 删除类型时,把引用该类型的任务 type_id 置空(事务内与 DeleteTaskType 同事务)。
	ClearTasksType(ctx context.Context, typeID string) error
}

// TaskStats 项目任务进度统计。
type TaskStats struct {
	Total      int `json:"total"`
	Todo       int `json:"todo"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Done       int `json:"done"`
	Overdue    int `json:"overdue"`
}

// GormRepository 项目域 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateProject(ctx context.Context, p *Project) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(p).Error
}

func (r *GormRepository) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *GormRepository) UpdateProject(ctx context.Context, p *Project) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(p).Error
}

func (r *GormRepository) ListProjectsByMember(ctx context.Context, userID string) ([]*Project, error) {
	var list []*Project
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Joins("JOIN project_members ON project_members.project_id = projects.id").
		Where("project_members.user_id = ?", userID).
		Order("projects.created_at DESC").
		Find(&list).Error
	return list, err
}

func (r *GormRepository) AddMember(ctx context.Context, m *ProjectMember) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(m).Error
}

func (r *GormRepository) RemoveMember(ctx context.Context, projectID, userID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		Delete(&ProjectMember{}).Error
}

func (r *GormRepository) GetMember(ctx context.Context, projectID, userID string) (*ProjectMember, error) {
	var m ProjectMember
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		First(&m, "project_id = ? AND user_id = ?", projectID, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *GormRepository) ListMembers(ctx context.Context, projectID string) ([]*ProjectMember, error) {
	var list []*ProjectMember
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("project_id = ?", projectID).Find(&list).Error
	return list, err
}

func (r *GormRepository) CreateMilestone(ctx context.Context, m *Milestone) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(m).Error
}

func (r *GormRepository) GetMilestone(ctx context.Context, id string) (*Milestone, error) {
	var m Milestone
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&m, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *GormRepository) UpdateMilestone(ctx context.Context, m *Milestone) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(m).Error
}

func (r *GormRepository) ListMilestones(ctx context.Context, projectID string) ([]*Milestone, error) {
	var list []*Milestone
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("due_date ASC NULLS LAST, created_at ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) CreateTask(ctx context.Context, t *Task) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(t).Error
}

func (r *GormRepository) GetTask(ctx context.Context, id string) (*Task, error) {
	var t Task
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *GormRepository) UpdateTask(ctx context.Context, t *Task) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(t).Error
}

func (r *GormRepository) SoftDeleteTask(ctx context.Context, id string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Delete(&Task{}, "id = ?", id).Error
}

func (r *GormRepository) ListTasks(ctx context.Context, projectID string, f ListFilter) ([]*Task, error) {
	q := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("project_id = ?", projectID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.AssigneeID != "" {
		q = q.Where("assignee_id = ?", f.AssigneeID)
	}
	if f.MilestoneID != "" {
		q = q.Where("milestone_id = ?", f.MilestoneID)
	}
	var list []*Task
	err := q.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *GormRepository) SearchTasks(ctx context.Context, keyword string, limit int) ([]*Task, error) {
	var list []*Task
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("title ILIKE ? ESCAPE '\\'", "%"+escapeLike(keyword)+"%"). // 透传通配符?按字面匹配
		Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

// escapeLike 转义 LIKE 通配符(与 document/resource 一致)
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (r *GormRepository) LinkTask(ctx context.Context, taskID, targetType, targetID string) error {
	link := TaskLink{TaskID: taskID, TargetType: targetType, TargetID: targetID}
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(&link).Error
}

func (r *GormRepository) ListLinks(ctx context.Context, taskID string) ([]*TaskLink, error) {
	var list []*TaskLink
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("task_id = ?", taskID).Find(&list).Error
	return list, err
}

// ---- F9 v2:任务类型字典 / 加成员候选 / 进度统计 ----

func (r *GormRepository) ListTaskTypes(ctx context.Context) ([]*TaskType, error) {
	var list []*TaskType
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("active = ?", true).Order("sort ASC, name ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) GetTaskType(ctx context.Context, id string) (*TaskType, error) {
	var t TaskType
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---- admin console:任务类型字典管理(specs/admin-console.md §3.3) ----

func (r *GormRepository) ListAllTaskTypes(ctx context.Context) ([]*TaskType, error) {
	var list []*TaskType
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("sort ASC, name ASC").Find(&list).Error
	return list, err
}

func (r *GormRepository) CreateTaskType(ctx context.Context, t *TaskType) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(t).Error
}

func (r *GormRepository) UpdateTaskType(ctx context.Context, t *TaskType) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(t).Error
}

func (r *GormRepository) DeleteTaskType(ctx context.Context, id string) error {
	res := database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&TaskType{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *GormRepository) ClearTasksType(ctx context.Context, typeID string) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).
		Model(&Task{}).Where("type_id = ?", typeID).
		Updates(map[string]any{"type_id": nil}).Error
}

func (r *GormRepository) ListCandidateUsers(ctx context.Context, projectID string) ([]*user.User, error) {
	var list []*user.User
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Raw(`SELECT id, username, display_name, role, avatar_url, created_at, updated_at
		     FROM users
		     WHERE id NOT IN (SELECT user_id FROM project_members WHERE project_id = ?)
		     ORDER BY display_name`, projectID).
		Scan(&list).Error
	return list, err
}

// TaskStats 按项目统计任务状态分布与过期数。
// overdue = 非 done 且 due_date < 今天(数据库 CURRENT_DATE,与前端同库)。
func (r *GormRepository) TaskStats(ctx context.Context, projectIDs []string) (map[string]*TaskStats, error) {
	out := map[string]*TaskStats{}
	if len(projectIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		ProjectID  string
		Total      int
		Todo       int
		InProgress int
		Blocked    int
		Done       int
		Overdue    int
	}{}
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Raw(`SELECT project_id,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE status = 'todo') AS todo,
		       COUNT(*) FILTER (WHERE status = 'in_progress') AS in_progress,
		       COUNT(*) FILTER (WHERE status = 'blocked') AS blocked,
		       COUNT(*) FILTER (WHERE status = 'done') AS done,
		       COUNT(*) FILTER (WHERE status <> 'done' AND due_date < CURRENT_DATE) AS overdue
		     FROM tasks WHERE project_id IN ? AND deleted_at IS NULL
		     GROUP BY project_id`, projectIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ProjectID] = &TaskStats{
			Total: row.Total, Todo: row.Todo, InProgress: row.InProgress,
			Blocked: row.Blocked, Done: row.Done, Overdue: row.Overdue,
		}
	}
	return out, nil
}
