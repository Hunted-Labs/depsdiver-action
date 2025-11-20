package config

import (
	"os"
	"strconv"
)

func LoadFromEnv() *Config {
	host := getEnv("SERVER_HOST", "localhost")
	port := getEnvInt("SERVER_PORT", 8080)
	dsn := getEnv("DATABASE_DSN", "")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")

	return &Config{
		Server: ServerConfig{
			Host: host,
			Port: port,
		},
		Database: DatabaseConfig{
			DSN: dsn,
		},
		Redis: RedisConfig{
			Addr: redisAddr,
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

