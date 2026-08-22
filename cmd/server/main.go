package main

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/config"
	"labnexus/internal/database"
)

func main() {
	cfg := config.Load()

	db, err := database.New(cfg)
	if err != nil {
		// 开发期不退出:允许先起服务看健康检查降级提示
		slog.Error("postgres connect failed", "error", err)
		slog.Warn("hint: run `docker compose up -d` to start Postgres & Redis")
	}

	r := gin.Default()

	// 健康检查
	r.GET("/api/health", func(c *gin.Context) {
		dbOK := false
		if db != nil {
			sqlDB, err := db.DB()
			dbOK = err == nil && sqlDB.Ping() == nil
		}
		status := "ok"
		if !dbOK {
			status = "degraded"
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  status,
			"service": "labnexus",
			"db":      dbOK,
		})
	})

	// TODO(阶段 1):注册业务路由
	//   auth(注册/登录/刷新/登出)、space、document、feed、tag、search、admin
	// 按 docs/api-contract.md 实现,模块内 handler → service → repository 分层。

	slog.Info("server started", "addr", ":"+cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("server exited", "error", err)
	}
}
