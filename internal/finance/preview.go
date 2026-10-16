// Package finance 经费管理域。
package finance

import (
	"context"
	"encoding/json"
	"time"

	"labnexus/internal/cache"
)

// previewKey 导入预览缓存 key。
const previewKey = "finance:import:preview:"

// ImportPreview 导入预览数据:有效行 + 被排除行数(错误/重复)。
type ImportPreview struct {
	Rows    []ImportRow `json:"rows"`
	Skipped int         `json:"skipped"`
}

// cachePreviewStore 基于 cache.Store 的导入预览存储。
type cachePreviewStore struct {
	store cache.Store
}

// NewCachePreviewStore 用通用键值存储实现导入预览(生产 = Redis)。
func NewCachePreviewStore(store cache.Store) ImportPreviewStore {
	return &cachePreviewStore{store: store}
}

func (c *cachePreviewStore) SetPreview(ctx context.Context, id string, p ImportPreview, ttl time.Duration) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, previewKey+id, string(data), ttl)
}

func (c *cachePreviewStore) GetPreview(ctx context.Context, id string) (ImportPreview, error) {
	data, err := c.store.Get(ctx, previewKey+id)
	if err != nil {
		return ImportPreview{}, ErrPreviewNotFound
	}
	var p ImportPreview
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return ImportPreview{}, ErrPreviewNotFound
	}
	return p, nil
}

// DelPreview 消费式失效:确认导入成功后删除预览,防止重复导入。
func (c *cachePreviewStore) DelPreview(ctx context.Context, id string) error {
	return c.store.Del(ctx, previewKey+id)
}
