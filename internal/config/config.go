package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config 应用配置,全部来自环境变量(有本地开发默认值)。
type Config struct {
	AppEnv        string // 运行环境:development / test / production
	Port          string // HTTP 监听端口
	DBHost        string
	DBPort        string
	DBUser        string
	DBPassword    string
	DBName        string
	MigrationsDir string // SQL 版本化迁移目录
	RedisAddr     string
	JWTSecret     string // 签名密钥(生产环境必须覆盖)
	WebDir        string // 前端静态目录(相对运行目录)

	// AuthzTestBypass 仅用于自动化测试环境,显式开启后 Casbin 授权检查全部放行。
	AuthzTestBypass bool

	AccessTokenTTL  time.Duration // access token 有效期(默认 15min)
	RefreshTokenTTL time.Duration // refresh token 有效期(默认 30 天)
}

// Load 从环境变量加载配置,缺失时使用本地开发默认值。
func Load() *Config {
	return &Config{
		AppEnv:        getenv("APP_ENV", "development"),
		Port:          getenv("PORT", "8080"),
		DBHost:        getenv("DB_HOST", "localhost"),
		DBPort:        getenv("DB_PORT", "5433"), // 项目容器映射 5433(避免本机 5432 冲突)
		DBUser:        getenv("DB_USER", "labnexus"),
		DBPassword:    getenv("DB_PASSWORD", "labnexus"),
		DBName:        getenv("DB_NAME", "labnexus"),
		MigrationsDir: getenv("MIGRATIONS_DIR", "migrations"),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6380"), // 项目容器映射 6380
		JWTSecret:     getenv("JWT_SECRET", "dev-secret-change-me"),
		WebDir:        getenv("WEB_DIR", "web"),

		AuthzTestBypass: getenvBool("AUTHZ_TEST_BYPASS", false),

		AccessTokenTTL:  getenvDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getenvDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
	}
}

// Validate 校验不允许在 production 中启用的危险组合。
func (c *Config) Validate() error {
	if c.AppEnv == "production" && c.AuthzTestBypass {
		return errors.New("AUTHZ_TEST_BYPASS cannot be enabled when APP_ENV=production")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
