package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains all runtime settings. Values are intentionally loaded from
// environment variables so the same binary works on Vercel, Docker, and local.
type Config struct {
	Port              int
	AllowedReferers   []string
	MaxImageSizeBytes int64
	MaxImagePixels    int
	DownSampleSize    int
	RedisAddress      string
	RedisPassword     string
	RedisDB           int
	UseRedisCache     bool
	MongoURI          string
	MongoDB           string
	UseMongoDB        bool
	CacheTTL          time.Duration
	RequestTimeout    time.Duration
	ExtractTimeout    time.Duration
	RateLimitPerMin   int
	MaxConcurrent     int
}

func Load() Config {
	return Config{
		Port:              envInt("PORT", 8080),
		AllowedReferers:   splitCSV(envString("ALLOWED_REFERERS", "*")),
		MaxImageSizeBytes: int64(envInt("MAX_IMAGE_SIZE_MB", 10)) * 1024 * 1024,
		MaxImagePixels:    envInt("MAX_IMAGE_PIXELS", 16777216),
		DownSampleSize:    envInt("DOWN_SAMPLE_SIZE", 400),
		RedisAddress:      os.Getenv("REDIS_ADDRESS"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		RedisDB:           envInt("REDIS_DB", 0),
		UseRedisCache:     envBool("USE_REDIS_CACHE", false),
		MongoURI:          os.Getenv("MONGO_URI"),
		MongoDB:           envString("MONGO_DB", "img2color"),
		UseMongoDB:        envBool("USE_MONGODB", false),
		CacheTTL:          time.Duration(envInt("CACHE_TTL_HOURS", 168)) * time.Hour,
		RequestTimeout:    time.Duration(envInt("REQUEST_TIMEOUT_SEC", 10)) * time.Second,
		ExtractTimeout:    time.Duration(envInt("EXTRACT_TIMEOUT_SEC", 5)) * time.Second,
		RateLimitPerMin:   envInt("RATE_LIMIT_PER_MIN", 60),
		MaxConcurrent:     envInt("MAX_CONCURRENT", 50),
	}
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}
