package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Env                string
	HTTPPort           string
	DatabaseURL        string
	JWTSecret          string
	JWTAccessTTL       time.Duration
	JWTRefreshTTL      time.Duration
	AIServiceURL       string
	CORSAllowedOrigins []string
	ExamMode           string // "online" | "lan"
	LogLevel           string
}

func Load() *Config {
	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		panic("JWT_SECRET environment variable is required and must not be empty — set a strong random value")
	}

	return &Config{
		Env:      getEnv("APP_ENV", "development"),
		HTTPPort: getEnv("HTTP_PORT", "8081"),

		// Local development MySQL:
		// Username: root | Password: empty | Host: 127.0.0.1 | Port: 3306 | DB: examshield
		DatabaseURL: getEnv(
			"DATABASE_URL",
			"root:@tcp(127.0.0.1:3306)/examshield?charset=utf8mb4&parseTime=True&loc=Local",
		),

		JWTSecret:     jwtSecret,
		JWTAccessTTL:  getDurationMinutes("JWT_ACCESS_TTL_MINUTES", 15),
		JWTRefreshTTL: getDurationMinutes("JWT_REFRESH_TTL_MINUTES", 60*24*7),

		AIServiceURL: getEnv("AI_SERVICE_URL", "http://localhost:9000"),

		// CORS_ALLOWED_ORIGINS may hold several comma-separated origins.
		// Falls back to CORS_ALLOWED_ORIGIN (single) for backward compat.
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS",
			getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:5173"))),

		ExamMode: getEnv("EXAM_MODE", "online"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getDurationMinutes(key string, fallbackMinutes int) time.Duration {
	return time.Duration(getEnvInt(key, fallbackMinutes)) * time.Minute
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
