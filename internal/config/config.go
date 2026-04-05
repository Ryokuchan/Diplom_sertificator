package config

import (
	"os"
	"strings"
)

type Config struct {
	ServerAddress string
	DatabaseURL   string
	RedisURL      string
	JWTSecret     string
	KafkaBrokers  []string
	KafkaGroup    string
	Environment   string
	AdminEmail    string
	AdminPassword string
}

func Load() *Config {
	return &Config{
		ServerAddress: getEnv("SERVER_ADDRESS", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/diasoft?sslmode=disable"),
		RedisURL:      getEnv("REDIS_URL", "localhost:6379"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		KafkaBrokers:  strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ","),
		KafkaGroup:    getEnv("KAFKA_GROUP", "diasoft-api"),
		Environment:   getEnv("ENVIRONMENT", "development"),
		AdminEmail:    getEnv("ADMIN_EMAIL", ""),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
