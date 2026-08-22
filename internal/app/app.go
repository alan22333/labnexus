// Package app 应用装配:连接数据库、迁移、依赖注入、注册全部路由。
// 生产入口(main)与集成测试共用本装配,保证"测试即生产"(test/integration)。
package app

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"labnexus/internal/auth"
	"labnexus/internal/cache"
	"labnexus/internal/config"
	"labnexus/internal/database"
	"labnexus/internal/document"
	"labnexus/internal/space"
	"labnexus/internal/tag"
	"labnexus/internal/user"
)

// Build 完成全部装配并返回 gin 路由。
func Build(cfg *config.Config) (*gin.Engine, error) {
	db, err := database.New(cfg)
	if err != nil {
		return nil, err
	}

	// 开发期 AutoMigrate;正式部署前切换 goose 版本化迁移(schema 权威定义 docs/schema.sql)
	if err := db.AutoMigrate(
		&user.User{}, &user.InviteCode{},
		&space.Space{}, &space.Folder{},
		&document.Document{}, &document.DocumentTag{}, &document.Comment{}, &document.Reaction{},
		&tag.Tag{},
	); err != nil {
		return nil, err
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
	spaceSvc := space.NewService(spaces, folders).
		WithDocCounter(document.NewGormRepository(db).CountByFolder)
	spaceHandler := space.NewHandler(spaceSvc)
	tagRepo := tag.NewGormRepository(db)
	tagSvc := tag.NewService(tagRepo)
	tagHandler := tag.NewHandler(tagSvc)
	docSvc := document.NewService(
		document.NewGormRepository(db),
		document.NewGormCommentRepository(db),
		document.NewGormReactionRepository(db),
		tagRepo, users, spaces, folders,
	).WithTxRunner(database.GormTxRunner(db))
	docHandler := document.NewHandler(docSvc)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

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

	// 路由注册(契约 docs/api-contract.md)
	authHandler.RegisterRoutes(r, cfg.JWTSecret)
	spaceHandler.RegisterRoutes(r, cfg.JWTSecret)
	docHandler.RegisterRoutes(r, cfg.JWTSecret)
	tagHandler.RegisterRoutes(r, cfg.JWTSecret)

	return r, nil
}
