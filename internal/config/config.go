package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr              string
	DSN               string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	SkillPackagePath  string
	PublicURL         string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Addr:              env("TYPE_REGISTRY_ADDR", ":8080"),
		DSN:               env("TYPE_REGISTRY_DSN", "type_center:type_center@tcp(127.0.0.1:3306)/x_type_center?charset=utf8mb4&parseTime=true&loc=UTC"),
		ReadTimeout:       durationEnv("TYPE_REGISTRY_READ_TIMEOUT", 10*time.Second),
		WriteTimeout:      durationEnv("TYPE_REGISTRY_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       durationEnv("TYPE_REGISTRY_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:   durationEnv("TYPE_REGISTRY_SHUTDOWN_TIMEOUT", 10*time.Second),
		SkillPackagePath:  env("TYPE_REGISTRY_SKILL_PACKAGE_PATH", ""),
		PublicURL:         strings.TrimRight(strings.TrimSpace(env("TYPE_REGISTRY_PUBLIC_URL", "")), "/"),
		DBMaxOpenConns:    intEnv("TYPE_REGISTRY_DB_MAX_OPEN_CONNS", 20),
		DBMaxIdleConns:    intEnv("TYPE_REGISTRY_DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLifetime: durationEnv("TYPE_REGISTRY_DB_CONN_MAX_LIFETIME", 30*time.Minute),
	}
	if cfg.DSN == "" {
		return Config{}, fmt.Errorf("TYPE_REGISTRY_DSN is required")
	}
	if cfg.PublicURL != "" {
		parsed, err := url.Parse(cfg.PublicURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, fmt.Errorf("TYPE_REGISTRY_PUBLIC_URL must be an absolute http(s) URL")
		}
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
