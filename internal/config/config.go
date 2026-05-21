package config

import (
	"os"
	"strconv"
)

// ProxyConfig holds configuration for the OpenAI proxy server.
type ProxyConfig struct {
	ListenAddr        string
	UpstreamBaseURL   string
	LogServerURL      string
	LogServerToken    string
	LogQueueSize      int
	CaptureMaxBytes   int
}

// LogServerConfig holds configuration for the log server.
type LogServerConfig struct {
	ListenAddr        string
	LogServerToken    string
	LogDir            string
	UTCHourlyLayout   string
	ArchiveEnabled    bool
	ArchiveZstdWindowMB int
	ArchiveZstdConcurrency int
	ArchiveMaxConcurrent int
}

// LoadProxyConfig loads proxy configuration from environment variables.
// Environment variables:
//   - LISTEN_ADDR: address to listen on (default "")
//   - UPSTREAM_BASE_URL: upstream OpenAI API base URL (default "https://api.openai.com")
//   - LOG_SERVER_URL: log server URL (default "")
//   - LOG_SERVER_TOKEN: token for authentication with log server (default "")
//   - LOG_QUEUE_SIZE: size of the log queue (default 1024)
//   - CAPTURE_MAX_BYTES: max bytes to capture from request/response bodies (default 0, meaning unlimited)
func LoadProxyConfig() ProxyConfig {
	return ProxyConfig{
		ListenAddr:      os.Getenv("LISTEN_ADDR"),
		UpstreamBaseURL: getEnvOrDefault("UPSTREAM_BASE_URL", "https://api.openai.com"),
		LogServerURL:    os.Getenv("LOG_SERVER_URL"),
		LogServerToken:  os.Getenv("LOG_SERVER_TOKEN"),
		LogQueueSize:    getEnvIntOrDefault("LOG_QUEUE_SIZE", 1024),
		CaptureMaxBytes: getEnvIntOrDefault("CAPTURE_MAX_BYTES", 0),
	}
}

// LoadLogServerConfig loads log server configuration from environment variables.
// Environment variables:
//   - LISTEN_ADDR: address to listen on (default "")
//   - LOG_SERVER_TOKEN: token for authentication (default "")
//   - LOG_DIR: directory for log files (default "")
//   - UTC_HOURLY_LAYOUT: Go time layout for hourly log files in UTC (default "2006/01/02/15")
//   - ARCHIVE_ENABLED: whether to compress stale JSONL files to .tar.zst (default "true")
//   - ARCHIVE_ZSTD_WINDOW_MB: zstd search window in MiB, power of two, max 512 (default 512)
//   - ARCHIVE_ZSTD_CONCURRENCY: zstd encoder concurrency per archive (default 8)
//   - ARCHIVE_MAX_CONCURRENT: max archive jobs running at once (default 1)
func LoadLogServerConfig() LogServerConfig {
	return LogServerConfig{
		ListenAddr:       os.Getenv("LISTEN_ADDR"),
		LogServerToken:   os.Getenv("LOG_SERVER_TOKEN"),
		LogDir:           os.Getenv("LOG_DIR"),
		UTCHourlyLayout:  getEnvOrDefault("UTC_HOURLY_LAYOUT", "2006/01/02/15"),
		ArchiveEnabled:   getEnvBoolOrDefault("ARCHIVE_ENABLED", true),
		ArchiveZstdWindowMB: getEnvIntOrDefault("ARCHIVE_ZSTD_WINDOW_MB", 512),
		ArchiveZstdConcurrency: getEnvIntOrDefault("ARCHIVE_ZSTD_CONCURRENCY", 8),
		ArchiveMaxConcurrent: getEnvIntOrDefault("ARCHIVE_MAX_CONCURRENT", 1),
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvBoolOrDefault(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "1" || val == "true" || val == "yes"
	}
	return defaultVal
}
