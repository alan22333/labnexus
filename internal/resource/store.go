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
	// Save 保存文件,返回内部相对路径(如 uploads/xxx.pdf)。
	Save(reader io.Reader, filename string) (string, error)
	// Delete 删除已保存的文件(路径来自 Save 返回值)。
	Delete(path string) error
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

// Save 用随机文件名保存,避免覆盖与路径注入。
func (s *LocalFileStore) Save(reader io.Reader, filename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) > 10 {
		ext = ""
	}
	rel := "uploads/" + uuid.NewString() + ext
	abs := filepath.Join(s.baseDir, filepath.Base(rel))
	f, err := os.Create(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return "", err
	}
	return rel, nil
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
