// Package space 个人空间域:F2 空间/目录业务逻辑。
// 分层:handler → service → repository;依据规格 docs/specs/f2-space.md。
package space

import (
	"context"
	"errors"
	"strings"
	"time"
)

// 哨兵错误(handler 层统一映射 HTTP)
var (
	ErrSpaceNotFound   = errors.New("space not found")
	ErrFolderNotFound  = errors.New("folder not found")
	ErrFolderNotOwned  = errors.New("folder does not belong to current user")
	ErrFolderNotEmpty  = errors.New("folder is not empty")
	ErrFolderNameEmpty = errors.New("folder name is empty")
)

// Service 空间/目录业务逻辑
type Service struct {
	spaces  Repository
	folders FolderRepository
	// docCounter 统计目录下文档数(F3 引入;nil = 不检查文档占用)
	docCounter func(ctx context.Context, folderID string) (int64, error)
}

// NewService 构造函数(依赖注入)
func NewService(spaces Repository, folders FolderRepository) *Service {
	return &Service{spaces: spaces, folders: folders}
}

// WithDocCounter 注入文档计数函数(目录删除时校验文档占用)。
func (s *Service) WithDocCounter(fn func(ctx context.Context, folderID string) (int64, error)) *Service {
	s.docCounter = fn
	return s
}

// FolderNode 树形目录节点(嵌套 children)
type FolderNode struct {
	*Folder
	Children []*FolderNode `json:"children,omitempty"`
}

// GetSpace 获取当前用户的空间与目录树。
func (s *Service) GetSpace(ctx context.Context, userID string) (*Space, []*FolderNode, error) {
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	folders, err := s.folders.ListBySpace(ctx, sp.ID)
	if err != nil {
		return nil, nil, err
	}
	return sp, buildTree(folders), nil
}

// CreateFolder 创建目录(校验父目录归属当前用户空间)。
func (s *Service) CreateFolder(ctx context.Context, userID, name string, parentID *string) (*Folder, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if parentID != nil {
		parent, err := s.folders.GetByID(ctx, *parentID)
		if err != nil {
			return nil, ErrFolderNotFound
		}
		if parent.SpaceID != sp.ID {
			return nil, ErrFolderNotOwned
		}
	}
	f := NewFolder(sp.ID, parentID, name, 0)
	if err := s.folders.Create(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// UpdateFolder 修改目录名称/排序(校验归属)。
func (s *Service) UpdateFolder(ctx context.Context, userID, folderID string, name *string, sortOrder *int) (*Folder, error) {
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	f, err := s.ownedFolder(ctx, sp, folderID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		if err := validateName(*name); err != nil {
			return nil, err
		}
		f.Name = *name
	}
	if sortOrder != nil {
		f.SortOrder = *sortOrder
	}
	touch(f)
	if err := s.folders.Update(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteFolder 删除目录(仅空目录:无子目录且无文档)。
func (s *Service) DeleteFolder(ctx context.Context, userID, folderID string) error {
	sp, err := s.spaceOf(ctx, userID)
	if err != nil {
		return err
	}
	f, err := s.ownedFolder(ctx, sp, folderID)
	if err != nil {
		return err
	}
	children, err := s.folders.CountChildren(ctx, f.ID)
	if err != nil {
		return err
	}
	if children > 0 {
		return ErrFolderNotEmpty
	}
	if s.docCounter != nil {
		docs, err := s.docCounter(ctx, f.ID)
		if err != nil {
			return err
		}
		if docs > 0 {
			return ErrFolderNotEmpty
		}
	}
	return s.folders.Delete(ctx, f.ID)
}

// spaceOf 获取用户的空间(内部 helper)。
func (s *Service) spaceOf(ctx context.Context, userID string) (*Space, error) {
	sp, err := s.spaces.GetByUserID(ctx, userID)
	if err != nil {
		return nil, ErrSpaceNotFound
	}
	return sp, nil
}

// ownedFolder 校验目录属于该空间并返回(内部 helper)。
func (s *Service) ownedFolder(ctx context.Context, sp *Space, folderID string) (*Folder, error) {
	f, err := s.folders.GetByID(ctx, folderID)
	if err != nil {
		return nil, ErrFolderNotFound
	}
	if f.SpaceID != sp.ID {
		return nil, ErrFolderNotOwned
	}
	return f, nil
}

// validateName 目录名校验。
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrFolderNameEmpty
	}
	return nil
}

// touch 更新时间戳。
func touch(f *Folder) {
	f.UpdatedAt = time.Now()
}

// buildTree 将扁平的目录列表组装为树(依赖 ListBySpace 已按 sort_order 排序)。
func buildTree(folders []*Folder) []*FolderNode {
	nodes := make(map[string]*FolderNode, len(folders))
	for _, f := range folders {
		nodes[f.ID] = &FolderNode{Folder: f}
	}
	var roots []*FolderNode
	for _, f := range folders { // 按已排序的切片顺序,保证 children 有序
		n := nodes[f.ID]
		if f.ParentID != nil {
			if parent, ok := nodes[*f.ParentID]; ok {
				parent.Children = append(parent.Children, n)
			} else {
				roots = append(roots, n) // 父目录不在列表(数据异常),按根处理
			}
		} else {
			roots = append(roots, n)
		}
	}
	return roots
}
