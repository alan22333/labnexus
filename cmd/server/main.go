package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"labnexus/internal/app"
	"labnexus/internal/config"
	"labnexus/internal/database"
)

func main() {
	cfg := config.Load()
	if cfg.AppEnv != "production" {
		migrateErr := database.MigrateBaseline(context.Background(), cfg, cfg.MigrationsDir)
		if errors.Is(migrateErr, database.ErrBaselineAlreadyVersioned) {
			migrateErr = database.MigrateUp(context.Background(), cfg, cfg.MigrationsDir)
		}
		if migrateErr != nil {
			err := migrateErr
			slog.Error("database migration failed", "error", err)
			slog.Warn("hint: run `make up` to start Postgres, then retry")
			os.Exit(1)
		}
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
