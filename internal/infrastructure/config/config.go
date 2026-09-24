package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env             string
	HTTPAddr        string
	DatabaseURL     string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	JWTSecret       string
	JWTTTL          time.Duration
	ShareBaseURL    string
	FlushInterval   time.Duration
	ViewWorkers     int
	RateLimitPerMin int64
	TrustedProxies  []string
}

func (c Config) IsProduction() bool { return c.Env == "production" }

func Load() (Config, error) {
	var errs []error

	cfg := Config{
		Env:           getenv("APP_ENV", "development"),
		HTTPAddr:      getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		ShareBaseURL:  getenv("SHARE_BASE_URL", "https://youpass.vn/s/"),
	}
	cfg.RedisDB = intEnv("REDIS_DB", 0, &errs)
	cfg.JWTTTL = durationEnv("JWT_TTL", 15*time.Minute, &errs)
	cfg.FlushInterval = durationEnv("FLUSH_INTERVAL", 10*time.Second, &errs)
	cfg.ViewWorkers = intEnv("VIEW_WORKERS", 4, &errs)
	cfg.RateLimitPerMin = int64(intEnv("RATE_LIMIT_PER_MIN", 60, &errs))

	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}

	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if cfg.ViewWorkers < 1 {
		errs = append(errs, errors.New("VIEW_WORKERS must be >= 1"))
	}
	if cfg.FlushInterval < time.Second {
		errs = append(errs, errors.New("FLUSH_INTERVAL must be >= 1s"))
	}
	if cfg.RateLimitPerMin < 1 {
		errs = append(errs, errors.New("RATE_LIMIT_PER_MIN must be >= 1"))
	}
	if !strings.HasSuffix(cfg.ShareBaseURL, "/") {
		cfg.ShareBaseURL += "/"
	}

	return cfg, errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int, errs *[]error) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return fallback
	}
	return n
}

func durationEnv(key string, fallback time.Duration, errs *[]error) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", key, err))
		return fallback
	}
	return d
}
