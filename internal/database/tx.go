package database

import (
	"context"

	"gorm.io/gorm"
)

// 事务上下文:repo 层通过 TxFromContext 取当前事务连接(规范 §5 写操作事务化)。

type txKey struct{}

// WithTx 将事务连接放入 context。
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext 优先返回 ctx 中的事务连接,否则回退到默认 db。
func TxFromContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx
	}
	return db
}

// TxRunner 事务运行器:fn 内的所有 repo 调用共享同一事务连接。
type TxRunner func(ctx context.Context, fn func(ctx context.Context) error) error

// GormTxRunner 基于 GORM 事务的实现(失败自动回滚)。
func GormTxRunner(db *gorm.DB) TxRunner {
	return func(ctx context.Context, fn func(ctx context.Context) error) error {
		return db.Transaction(func(tx *gorm.DB) error {
			return fn(WithTx(ctx, tx))
		})
	}
}

// NoopTxRunner 无事务直执行(测试替身用)。
func NoopTxRunner() TxRunner {
	return func(ctx context.Context, fn func(ctx context.Context) error) error {
		return fn(ctx)
	}
}
