// Package project 项目任务域:F9 项目/成员/里程碑/任务(状态机)。
// 依据规格:docs/specs/f9-project.md;契约:api-contract.md §F9。
package project

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
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

// Task 任务(schema.sql: tasks;DeletedAt 软删除)
type Task struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	ProjectID   string         `gorm:"type:uuid;index" json:"project_id"`
	Title       string         `gorm:"size:200" json:"title"`
	Description string         `gorm:"type:text" json:"description"`
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
func NewTask(projectID, title, description string, assigneeID *string, priority string, dueDate *string, milestoneID *string) *Task {
	now := time.Now()
	return &Task{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Description: description,
		AssigneeID: assigneeID, Status: TaskStatusTodo, Priority: priority,
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

	LinkTask(ctx context.Context, taskID, targetType, targetID string) error
	ListLinks(ctx context.Context, taskID string) ([]*TaskLink, error)
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
