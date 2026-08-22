package main

import (
	"log/slog"
	"os"

	"labnexus/internal/app"
	"labnexus/internal/config"
)

func main() {
	cfg := config.Load()

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
