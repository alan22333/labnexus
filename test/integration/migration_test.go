//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"labnexus/internal/config"
	"labnexus/internal/database"
)

const migrationsDir = "../../migrations"

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
	latest := latestMigrationVersion(t)

	// 全新库:up 后应有完整 schema 与默认任务类型;重复 up 必须幂等。
	require.NoError(t, database.MigrateUp(ctx, &cfg, migrationsDir))
	version, err := database.MigrationVersion(ctx, &cfg)
	require.NoError(t, err)
	require.Equal(t, latest, version)
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))

	require.NoError(t, database.MigrateUp(ctx, &cfg, migrationsDir))
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))

	// 回滚到版本 1 只删除增量迁移(含默认任务类型种子);再次 up 恢复。
	migrateDownToVersionOne(t, ctx, &cfg)
	require.Equal(t, int64(1), mustVersion(t, ctx, &cfg))
	require.Equal(t, int64(0), countRows(t, &cfg, "task_types"))
	require.NoError(t, database.MigrateUp(ctx, &cfg, migrationsDir))
	require.Equal(t, latest, mustVersion(t, ctx, &cfg))
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))

	// 模拟已经由 AutoMigrate 建表、但尚无 goose 版本记录的开发库:
	// schema 处于基线(v1)状态,goose 版本表缺失,应能 stamp 后补齐增量迁移。
	migrateDownToVersionOne(t, ctx, &cfg)
	adminDB, err := database.New(&cfg)
	require.NoError(t, err)
	require.NoError(t, adminDB.Exec("DROP TABLE goose_db_version").Error)
	closeDB(adminDB)

	require.NoError(t, database.MigrateBaseline(ctx, &cfg, migrationsDir))
	require.Equal(t, latest, mustVersion(t, ctx, &cfg))
	require.Equal(t, int64(5), countRows(t, &cfg, "task_types"))
}

func migrateDownToVersionOne(t *testing.T, ctx context.Context, cfg *config.Config) {
	t.Helper()
	for {
		v, err := database.MigrationVersion(ctx, cfg)
		require.NoError(t, err)
		if v <= 1 {
			return
		}
		require.NoError(t, database.MigrateDown(ctx, cfg, migrationsDir))
	}
}

func mustVersion(t *testing.T, ctx context.Context, cfg *config.Config) int64 {
	t.Helper()
	v, err := database.MigrationVersion(ctx, cfg)
	require.NoError(t, err)
	return v
}

// latestMigrationVersion 从 migrations 目录解析出最高版本号,避免每次新增迁移都要手改断言。
func latestMigrationVersion(t *testing.T) int64 {
	t.Helper()
	entries, err := os.ReadDir(filepath.FromSlash(migrationsDir))
	require.NoError(t, err)
	var latest int64
	for _, e := range entries {
		name := e.Name()
		idx := strings.IndexByte(name, '_')
		if idx <= 0 {
			continue
		}
		v, err := strconv.ParseInt(name[:idx], 10, 64)
		if err != nil {
			continue
		}
		if v > latest {
			latest = v
		}
	}
	require.Greater(t, latest, int64(0), "未在 %s 找到迁移文件", migrationsDir)
	return latest
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
