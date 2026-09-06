// Package user 用户域:User 模型与数据访问(对应 schema.sql users 表)。
package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"labnexus/internal/database"
)

// ErrNotFound 记录不存在(仓库契约统一错误)
var ErrNotFound = errors.New("user: not found")

// 角色枚举(PRD §3.1,三枚举为 supervisor 差异化权限预留)
const (
	RoleAdmin      = "admin"
	RoleSupervisor = "supervisor"
	RoleStudent    = "student"
)

// User 用户(schema.sql: users)
type User struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Username     string    `gorm:"size:50;uniqueIndex" json:"username"`
	DisplayName  string    `gorm:"size:50" json:"display_name"`
	PasswordHash string    `gorm:"size:255" json:"-"`
	Role         string    `gorm:"size:20;default:student" json:"role"`
	AvatarURL    string    `gorm:"size:255" json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewUser 构造新用户(role 默认 student)。
func NewUser(username, displayName, passwordHash string) *User {
	now := time.Now()
	return &User{
		ID:           uuid.NewString(),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Role:         RoleStudent,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// InviteCode 邀请码(schema.sql: invite_codes)
type InviteCode struct {
	ID        string     `gorm:"type:uuid;primaryKey" json:"id"`
	Code      string     `gorm:"size:32;uniqueIndex" json:"code"`
	CreatedBy string     `gorm:"type:uuid" json:"created_by"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	UsedBy    *string    `gorm:"type:uuid" json:"used_by,omitempty"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// NewInviteCode 构造邀请码。
func NewInviteCode(createdBy, code string, expiresAt *time.Time) *InviteCode {
	return &InviteCode{
		ID:        uuid.NewString(),
		Code:      code,
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
}

// IsExpired 判断邀请码是否过期。
func (c *InviteCode) IsExpired() bool {
	return c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt)
}

// IsUsed 判断邀请码是否已使用。
func (c *InviteCode) IsUsed() bool {
	return c.UsedBy != nil && c.UsedAt != nil
}

// Repository 用户数据访问接口(测试可用内存替身,规范 §2.1)
type Repository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByIDs(ctx context.Context, ids []string) ([]*User, error)
	Update(ctx context.Context, u *User) error
	// ListAll 全量用户(可空关键字按 username/display_name 子串过滤;按 display_name 排序;管理端用)
	ListAll(ctx context.Context, keyword string) ([]*User, error)
}

// GormRepository User 的 GORM 实现
type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, u *User) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(u).Error
}

func (r *GormRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&u, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *GormRepository) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&u, "username = ?", username).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *GormRepository) GetByIDs(ctx context.Context, ids []string) ([]*User, error) {
	if len(ids) == 0 {
		return []*User{}, nil
	}
	var users []*User
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Where("id IN ?", ids).Find(&users).Error
	return users, err
}

func (r *GormRepository) Update(ctx context.Context, u *User) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Save(u).Error
}

// ListAll 全量用户;keyword 非空时按 username/display_name ILIKE 过滤;按 display_name 排序。
func (r *GormRepository) ListAll(ctx context.Context, keyword string) ([]*User, error) {
	q := database.TxFromContext(ctx, r.db).WithContext(ctx)
	if strings.TrimSpace(keyword) != "" {
		kw := "%" + escapeLike(strings.TrimSpace(keyword)) + "%"
		q = q.Where("username ILIKE ? ESCAPE '\\' OR display_name ILIKE ? ESCAPE '\\'", kw, kw)
	}
	var list []*User
	err := q.Order("display_name ASC").Find(&list).Error
	return list, err
}

// escapeLike 转义 LIKE 通配符(与 project/resource 一致)。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// InviteRepository 邀请码数据访问接口
type InviteRepository interface {
	Create(ctx context.Context, c *InviteCode) error
	GetByCode(ctx context.Context, code string) (*InviteCode, error)
	MarkUsed(ctx context.Context, id, userID string) error
	// List 全部邀请码(按创建时间倒序;管理端用)
	List(ctx context.Context) ([]*InviteCode, error)
	// Delete 删除(撤销)邀请码;不存在返回 ErrNotFound
	Delete(ctx context.Context, id string) error
}

// GormInviteRepository InviteCode 的 GORM 实现
type GormInviteRepository struct {
	db *gorm.DB
}

func NewGormInviteRepository(db *gorm.DB) *GormInviteRepository {
	return &GormInviteRepository{db: db}
}

func (r *GormInviteRepository) Create(ctx context.Context, c *InviteCode) error {
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Create(c).Error
}

func (r *GormInviteRepository) GetByCode(ctx context.Context, code string) (*InviteCode, error) {
	var c InviteCode
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).First(&c, "code = ?", code).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *GormInviteRepository) MarkUsed(ctx context.Context, id, userID string) error {
	now := time.Now()
	return database.TxFromContext(ctx, r.db).WithContext(ctx).Model(&InviteCode{}).
		Where("id = ?", id).
		Updates(map[string]any{"used_by": userID, "used_at": now}).Error
}

func (r *GormInviteRepository) List(ctx context.Context) ([]*InviteCode, error) {
	var list []*InviteCode
	err := database.TxFromContext(ctx, r.db).WithContext(ctx).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *GormInviteRepository) Delete(ctx context.Context, id string) error {
	res := database.TxFromContext(ctx, r.db).WithContext(ctx).Delete(&InviteCode{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
