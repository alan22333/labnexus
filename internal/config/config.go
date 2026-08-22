package config

import (
	"os"
	"time"
)

// Config 应用配置,全部来自环境变量(有本地开发默认值)。
type Config struct {
	Port       string // HTTP 监听端口
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	RedisAddr  string
	JWTSecret  string // 签名密钥(生产环境必须覆盖)
	WebDir     string // 前端静态目录(相对运行目录)

	AccessTokenTTL  time.Duration // access token 有效期(默认 15min)
	RefreshTokenTTL time.Duration // refresh token 有效期(默认 30 天)
}

// Load 从环境变量加载配置,缺失时使用本地开发默认值。
func Load() *Config {
	return &Config{
		Port:       getenv("PORT", "8080"),
		DBHost:     getenv("DB_HOST", "localhost"),
		DBPort:     getenv("DB_PORT", "5433"), // 项目容器映射 5433(避免本机 5432 冲突)
		DBUser:     getenv("DB_USER", "labnexus"),
		DBPassword: getenv("DB_PASSWORD", "labnexus"),
		DBName:     getenv("DB_NAME", "labnexus"),
		RedisAddr:  getenv("REDIS_ADDR", "localhost:6380"), // 项目容器映射 6380
		JWTSecret:  getenv("JWT_SECRET", "dev-secret-change-me"),
		WebDir:     getenv("WEB_DIR", "web"),

		AccessTokenTTL:  getenvDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getenvDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
