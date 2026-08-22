package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"labnexus/internal/auth"
	"labnexus/internal/cache"
	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/space"
	"labnexus/internal/user"
)

func main() {
	cfg := config.Load()

	db, err := database.New(cfg)
	if err != nil {
		slog.Error("database init failed", "error", err)
		slog.Warn("hint: run `make up` to start Postgres & Redis, then retry")
		os.Exit(1)
	}

	// 开发期 AutoMigrate;正式部署前切换 goose 版本化迁移(schema 权威定义 docs/schema.sql)
	if err := db.AutoMigrate(&user.User{}, &user.InviteCode{}, &space.Space{}, &space.Folder{}); err != nil {
		slog.Error("auto migrate failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database migrated")

	// 依赖装配(规范 §3:构造函数注入)
	store := cache.NewRedisStore(cfg.RedisAddr)
	users := user.NewGormRepository(db)
	invites := user.NewGormInviteRepository(db)
	spaces := space.NewGormRepository(db)
	folders := space.NewGormFolderRepository(db)
	authSvc := auth.NewService(users, invites, spaces, store, cfg).
		WithTxRunner(database.GormTxRunner(db))
	authHandler := auth.NewHandler(authSvc)
	spaceSvc := space.NewService(spaces, folders)
	spaceHandler := space.NewHandler(spaceSvc)

	r := gin.Default()

	// 健康检查
	r.GET("/api/health", func(c *gin.Context) {
		sqlDB, err := db.DB()
		dbOK := err == nil && sqlDB.Ping() == nil
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

	// F1 账号系统(契约 docs/api-contract.md §F1)
	authHandler.RegisterRoutes(r, cfg.JWTSecret)

	// F2 个人空间与目录(契约 docs/api-contract.md §F2)
	spaceHandler.RegisterRoutes(r, cfg.JWTSecret)

	// TODO(阶段 1):F3 文档 / F4 信息流 / F5 标签 / F6 搜索

	slog.Info("server started", "addr", ":"+cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		slog.Error("server exited", "error", err)
	}
}
