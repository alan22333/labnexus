// Command seed-demo 清空业务表并写入高保真演示数据(见 docs/specs/dev-demo-data.md)。
//
// 用法:
//
//	go run ./cmd/seed-demo            # 强制重建演示数据
//	go run ./cmd/seed-demo --if-empty # 仅当库中无用户时写入(供启动自动补种复用)
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"

	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/demo"
)

func main() {
	ifEmpty := flag.Bool("if-empty", false, "仅当数据库无任何用户时才写入演示数据")
	flag.Parse()

	ctx := context.Background()
	cfg := config.Load()

	if err := migrate(ctx, cfg); err != nil {
		slog.Error("database migration failed", "error", err)
		slog.Warn("hint: run `make up` to start Postgres, then retry")
		os.Exit(1)
	}

	db, err := database.New(cfg)
	if err != nil {
		slog.Error("connect database failed", "error", err)
		slog.Warn("hint: run `make up` to start Postgres, then retry")
		os.Exit(1)
	}

	if *ifEmpty {
		empty, err := demo.IsEmpty(ctx, db)
		if err != nil {
			slog.Error("check demo data state failed", "error", err)
			os.Exit(1)
		}
		if !empty {
			slog.Info("demo seed skipped: database not empty")
			return
		}
	}

	res, err := demo.Seed(ctx, db, demo.DefaultUploadDir)
	if err != nil {
		slog.Error("seed demo data failed", "error", err)
		os.Exit(1)
	}

	slog.Info("demo data seeded",
		"users", res.Users,
		"documents", res.Documents,
		"public", res.PublicDocuments,
		"private", res.PrivateDocuments,
		"resources", res.Resources,
		"projects", res.Projects,
		"tasks", res.Tasks,
		"transactions", res.Transactions,
	)
	slog.Info("login with", "admin", demo.AdminUsername+"/"+demo.AdminPassword, "supervisor", demo.SupervisorUser+"/"+demo.SupervisorPass)
}

func migrate(ctx context.Context, cfg *config.Config) error {
	err := database.MigrateBaseline(ctx, cfg, cfg.MigrationsDir)
	if errors.Is(err, database.ErrBaselineAlreadyVersioned) {
		return database.MigrateUp(ctx, cfg, cfg.MigrationsDir)
	}
	return err
}
