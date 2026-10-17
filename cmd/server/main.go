package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"

	"labnexus/internal/app"
	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/demo"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()
	if cfg.AppEnv != "production" {
		migrateErr := database.MigrateBaseline(ctx, cfg, cfg.MigrationsDir)
		if errors.Is(migrateErr, database.ErrBaselineAlreadyVersioned) {
			migrateErr = database.MigrateUp(ctx, cfg, cfg.MigrationsDir)
		}
		if migrateErr != nil {
			err := migrateErr
			slog.Error("database migration failed", "error", err)
			slog.Warn("hint: run `make up` to start Postgres, then retry")
			os.Exit(1)
		}
		demoSeedIfEmpty(ctx, cfg)
	} else {
		slog.Info("production startup: database migrations must be run explicitly")
	}

	r, err := app.Build(cfg)
	if err != nil {
		slog.Error("app build failed", "error", err)
		slog.Warn("hint: run `make up` to start Postgres & Redis, then retry")
		os.Exit(1)
	}

	slog.Info("server started", "addr", ":"+cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("server exited", "error", err)
	}
}

// demoSeedIfEmpty 开发环境空库自动补种演示数据(见 docs/specs/dev-demo-data.md)。
// 仅在非 production 且 `DEMO_SEED` 未显式关闭、users 表为空时触发;失败只告警不阻断启动。
func demoSeedIfEmpty(ctx context.Context, cfg *config.Config) {
	if !envBool("DEMO_SEED", true) {
		return
	}
	db, err := database.New(cfg)
	if err != nil {
		slog.Warn("demo seed skipped: connect database failed", "error", err)
		return
	}
	empty, err := demo.IsEmpty(ctx, db)
	if err != nil {
		slog.Warn("demo seed skipped: check database state failed", "error", err)
		return
	}
	if !empty {
		return
	}
	res, err := demo.Seed(ctx, db, demo.DefaultUploadDir)
	if err != nil {
		slog.Warn("demo seed failed", "error", err)
		return
	}
	slog.Info("demo data auto-seeded (empty database)",
		"users", res.Users, "documents", res.Documents, "projects", res.Projects, "tasks", res.Tasks)
	slog.Info("demo login", "admin", demo.AdminUsername+"/"+demo.AdminPassword, "supervisor", demo.SupervisorUser+"/"+demo.SupervisorPass)
}

// envBool 读取布尔环境变量,缺失或非法时返回 fallback。
func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
