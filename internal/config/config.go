package config

import "os"

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
}

// Load 从环境变量加载配置,缺失时使用本地开发默认值。
func Load() *Config {
	return &Config{
		Port:       getenv("PORT", "8080"),
		DBHost:     getenv("DB_HOST", "localhost"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBUser:     getenv("DB_USER", "labnexus"),
		DBPassword: getenv("DB_PASSWORD", "labnexus"),
		DBName:     getenv("DB_NAME", "labnexus"),
		RedisAddr:  getenv("REDIS_ADDR", "localhost:6379"),
		JWTSecret:  getenv("JWT_SECRET", "dev-secret-change-me"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
