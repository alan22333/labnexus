//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"labnexus/internal/config"
	"labnexus/internal/database"
)

func TestMigrationLifecycleAndBaseline(t *testing.T) {
	baseCfg := config.Load()
	admin, err := database.New(baseCfg)
	if err != nil {
		t.Skipf("集成环境未就绪(先 `make up` 启动 Postgres/Redis): %v", err)
	}
	defer closeDB(admin)

	dbName := "labnexus_migration_" + time.Now().Format("20060102150405") + "_" + uuid.NewString()[:8]
	require.NoError(t, admin.Exec("CREATE DATABASE "+quoteIdent(dbName)).Error)
	defer func() {
		_ = admin.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ?", dbName).Error
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(dbName)).Error
	}()

	cfg := *baseCfg
	cfg.DBName = dbName
	ctx := context.Background()

	// 全新库:up 后应有完整 schema 与默认任务类型;重复 up 必须幂等。
	require.NoError(t, database.MigrateUp(ctx, &cfg, "../../migrations"))
	version, err := database.MigrationVersion(ctx, &cfg)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))

	require.NoError(t, database.MigrateUp(ctx, &cfg, "../../migrations"))
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))

	// 回滚最近一版只删除种子;再次 up 恢复。
	require.NoError(t, database.MigrateDown(ctx, &cfg, "../../migrations"))
	version, err = database.MigrationVersion(ctx, &cfg)
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	require.Equal(t, int64(0), countRows(t, &cfg, "task_types"))
	require.NoError(t, database.MigrateUp(ctx, &cfg, "../../migrations"))

	// 模拟已经由 AutoMigrate 建表、但尚无 goose 版本记录的开发库。
	adminDB, err := database.New(&cfg)
	require.NoError(t, err)
	require.NoError(t, adminDB.Exec("DROP TABLE goose_db_version").Error)
	closeDB(adminDB)

	require.NoError(t, database.MigrateBaseline(ctx, &cfg, "../../migrations"))
	version, err = database.MigrationVersion(ctx, &cfg)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))
}

func countRows(t *testing.T, cfg *config.Config, table string) int64 {
	t.Helper()
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer closeDB(db)
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM "+quoteIdent(table)).Scan(&count).Error)
	return count
}

func quoteIdent(s string) string {
	return `"` + s + `"`
}
