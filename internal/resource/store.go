// Package resource 文件存储抽象:F7 上传文件落盘(data/uploads/)。
package resource

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// FileStore 文件存储接口(测试可用内存替身)
type FileStore interface {
	// Save 保存文件,返回内部相对路径(如 uploads/xxx.pdf)与真实字节数。
	Save(reader io.Reader, filename string) (path string, size int64, err error)
	// Open 打开已保存的文件(相对路径来自 Save 返回值);供下载/预览。
	Open(path string) (io.ReadSeekCloser, error)
	// Delete 删除已保存的文件(路径来自 Save 返回值);用于上传流程回滚。
	Delete(path string) error
	// Quarantine 将文件移动到不可经由资源接口访问的隔离区;文件不存在时返回空路径。
	Quarantine(path string) (quarantinePath string, err error)
	// Restore 将隔离文件恢复到原路径(数据库删除失败时回滚)。
	Restore(path, quarantinePath string) error
	// DeleteQuarantined 永久清理隔离文件(数据库删除成功后调用)。
	DeleteQuarantined(quarantinePath string) error
}

// LocalFileStore 本地磁盘实现(数据目录 data/uploads/)
type LocalFileStore struct {
	baseDir string // 绝对路径,如 <repo>/data/uploads
}

// NewLocalFileStore 创建本地存储(确保目录存在)。
func NewLocalFileStore(baseDir string) (*LocalFileStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &LocalFileStore{baseDir: baseDir}, nil
}

// Save 用随机文件名保存,避免覆盖与路径注入;返回真实字节数。
func (s *LocalFileStore) Save(reader io.Reader, filename string) (string, int64, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) > 10 {
		ext = ""
	}
	rel := "uploads/" + uuid.NewString() + ext
	abs := filepath.Join(s.baseDir, filepath.Base(rel))
	f, err := os.Create(abs)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	n, err := io.Copy(f, reader)
	if err != nil {
		return "", 0, err
	}
	return rel, n, nil
}

// Open 打开文件(相对路径;防目录穿越,只允许 baseDir 内)。
func (s *LocalFileStore) Open(path string) (io.ReadSeekCloser, error) {
	if path == "" {
		return nil, errors.New("empty file path")
	}
	abs := filepath.Join(s.baseDir, filepath.Base(path))
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Delete 删除文件;不存在视为成功(幂等)。
func (s *LocalFileStore) Delete(path string) error {
	if path == "" {
		return nil
	}
	abs := filepath.Join(s.baseDir, filepath.Base(path))
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Quarantine 将原文件移入 baseDir/.trash,避免数据库删除成功前留下可见孤儿文件。
// 返回的隔离路径是存储实现内部句柄,调用方不得拼接或经由资源接口访问。
func (s *LocalFileStore) Quarantine(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	src := filepath.Join(s.baseDir, filepath.Base(path))
	trashDir := filepath.Join(s.baseDir, ".trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(trashDir, uuid.NewString()+strings.ToLower(filepath.Ext(path)))
	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return dst, nil
}

// Restore 将隔离文件恢复到原路径;用于数据库删除失败时回滚。
func (s *LocalFileStore) Restore(path, quarantinePath string) error {
	if quarantinePath == "" {
		return nil
	}
	dst := filepath.Join(s.baseDir, filepath.Base(path))
	return os.Rename(quarantinePath, dst)
}

// DeleteQuarantined 永久删除隔离文件;不存在视为成功(幂等)。
func (s *LocalFileStore) DeleteQuarantined(quarantinePath string) error {
	if quarantinePath == "" {
		return nil
	}
	if err := os.Remove(quarantinePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
