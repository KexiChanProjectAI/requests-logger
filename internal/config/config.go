package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ProxyConfig holds configuration for the OpenAI proxy server.
type ProxyConfig struct {
	ListenAddr              string
	UpstreamBaseURL         string
	LogServerURL            string
	LogServerToken          string
	LogQueueSize            int
	LogClientWorkers        int
	LogClientMaxRetries     int
	CaptureMaxBytes         int
	UpstreamTimeout         time.Duration
	UpstreamMaxIdleConns    int
	UpstreamIdleConnTimeout time.Duration
	ReadHeaderTimeout       time.Duration
	IdleTimeout             time.Duration
	ProfileEnabled          bool
	ProfileListenAddr       string
	UpstreamTLSInsecure     bool
	UpstreamTLSSNI          string
	ProxyTLSCertFile        string       // TLS cert file path for proxy HTTPS listener (empty = HTTP)
	ProxyTLSKeyFile         string       // TLS key file path for proxy HTTPS listener
	TrustedProxyCIDRs       []*net.IPNet // parsed CIDR list from TRUSTED_PROXY_CIDRS
	TrustedProxyXFFMode     string       // XFF handling mode for trusted proxies: "append" (default) or "forward"
}

// LogServerConfig holds configuration for the log server.
type LogServerConfig struct {
	ListenAddr             string
	LogServerToken         string
	LogDir                 string
	UTCHourlyLayout        string
	ArchiveEnabled         bool
	ArchiveZstdWindowMB    int
	ArchiveZstdConcurrency int
	ArchiveMaxConcurrent   int
	StaleHandleTimeout     time.Duration
	CleanupInterval        time.Duration
	ReadHeaderTimeout      time.Duration
	IdleTimeout            time.Duration
	ProfileEnabled         bool
	ProfileListenAddr      string
}

// LoadProxyConfig loads proxy configuration from environment variables.
// Environment variables:
//   - LISTEN_ADDR: address to listen on (default "")
//   - UPSTREAM_BASE_URL: upstream OpenAI API base URL (default "https://api.openai.com")
//   - LOG_SERVER_URL: log server URL (default "")
//   - LOG_SERVER_TOKEN: token for authentication with log server (default "")
//   - LOG_QUEUE_SIZE: size of the log queue (default 1024)
//   - LOG_CLIENT_WORKERS: number of log client workers (default 4)
//   - LOG_CLIENT_MAX_RETRIES: max retries for log client (default 3)
//   - CAPTURE_MAX_BYTES: max bytes to capture from request/response bodies (default 0, meaning unlimited)
//   - UPSTREAM_TIMEOUT: timeout for upstream requests (default 120s)
//   - UPSTREAM_MAX_IDLE_CONNS: max idle connections to upstream (default 100)
//   - UPSTREAM_IDLE_CONN_TIMEOUT: idle connection timeout (default 90s)
//   - READ_HEADER_TIMEOUT: read header timeout (default 10s)
//   - IDLE_TIMEOUT: idle timeout (default 120s)
//   - PROFILE_ENABLED: enable pprof server (default false)
//   - PROFILE_LISTEN_ADDR: pprof server listen address (default ":6060")
//   - UPSTREAM_TLS_INSECURE: skip upstream TLS certificate verification (default false)
//   - UPSTREAM_TLS_SNI: override TLS ServerName (SNI) for upstream HTTPS (default "")
//   - PROXY_TLS_CERT_FILE: TLS cert file path for proxy HTTPS listener (default "")
//   - PROXY_TLS_KEY_FILE: TLS key file path for proxy HTTPS listener (default "")
//   - TRUSTED_PROXY_CIDRS: comma-separated CIDRs of trusted reverse proxies whose XFF headers are preserved (default "")
//   - TRUSTED_PROXY_XFF_MODE: XFF handling mode for trusted proxies: "append" (append client IP to chain, default) or "forward" (passthrough as-is, default "append")
func LoadProxyConfig() ProxyConfig {
	return ProxyConfig{
		ListenAddr:              os.Getenv("LISTEN_ADDR"),
		UpstreamBaseURL:         getEnvOrDefault("UPSTREAM_BASE_URL", "https://api.openai.com"),
		LogServerURL:            os.Getenv("LOG_SERVER_URL"),
		LogServerToken:          os.Getenv("LOG_SERVER_TOKEN"),
		LogQueueSize:            getEnvIntOrDefault("LOG_QUEUE_SIZE", 1024),
		LogClientWorkers:        getEnvIntOrDefault("LOG_CLIENT_WORKERS", 4),
		LogClientMaxRetries:     getEnvIntOrDefault("LOG_CLIENT_MAX_RETRIES", 3),
		CaptureMaxBytes:         getEnvIntOrDefault("CAPTURE_MAX_BYTES", 0),
		UpstreamTimeout:         getEnvDurationOrDefault("UPSTREAM_TIMEOUT", 120*time.Second),
		UpstreamMaxIdleConns:    getEnvIntOrDefault("UPSTREAM_MAX_IDLE_CONNS", 100),
		UpstreamIdleConnTimeout: getEnvDurationOrDefault("UPSTREAM_IDLE_CONN_TIMEOUT", 90*time.Second),
		ReadHeaderTimeout:       getEnvDurationOrDefault("READ_HEADER_TIMEOUT", 10*time.Second),
		IdleTimeout:             getEnvDurationOrDefault("IDLE_TIMEOUT", 120*time.Second),
		ProfileEnabled:          getEnvBoolOrDefault("PROFILE_ENABLED", false),
		ProfileListenAddr:       getEnvOrDefault("PROFILE_LISTEN_ADDR", ":6060"),
		UpstreamTLSInsecure:     getEnvBoolOrDefault("UPSTREAM_TLS_INSECURE", false),
		UpstreamTLSSNI:          os.Getenv("UPSTREAM_TLS_SNI"),
		ProxyTLSCertFile:        os.Getenv("PROXY_TLS_CERT_FILE"),
		ProxyTLSKeyFile:         os.Getenv("PROXY_TLS_KEY_FILE"),
		TrustedProxyCIDRs:       ParseCIDRList(os.Getenv("TRUSTED_PROXY_CIDRS")),
		TrustedProxyXFFMode:     parseXFFMode(os.Getenv("TRUSTED_PROXY_XFF_MODE")),
}
}

func ParseCIDRList(s string) []*net.IPNet {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var cidrs []*net.IPNet
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			continue
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs
}
func parseXFFMode(s string) string {
	if s == "" {
		return "append"
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "forward" || s == "passthrough" {
		return "forward"
	}
	return "append"
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
//   - STALE_HANDLE_TIMEOUT: how long a file handle can be idle before closing (default 5m)
//   - CLEANUP_INTERVAL: how often to run stale handle cleanup (default 1m)
//   - READ_HEADER_TIMEOUT: read header timeout (default 10s)
//   - IDLE_TIMEOUT: idle timeout (default 120s)
//   - PROFILE_ENABLED: enable pprof server (default false)
//   - PROFILE_LISTEN_ADDR: pprof server listen address (default ":6060")
func LoadLogServerConfig() LogServerConfig {
	return LogServerConfig{
		ListenAddr:             os.Getenv("LISTEN_ADDR"),
		LogServerToken:         os.Getenv("LOG_SERVER_TOKEN"),
		LogDir:                 os.Getenv("LOG_DIR"),
		UTCHourlyLayout:        getEnvOrDefault("UTC_HOURLY_LAYOUT", "2006/01/02/15"),
		ArchiveEnabled:         getEnvBoolOrDefault("ARCHIVE_ENABLED", true),
		ArchiveZstdWindowMB:    getEnvIntOrDefault("ARCHIVE_ZSTD_WINDOW_MB", 512),
		ArchiveZstdConcurrency: getEnvIntOrDefault("ARCHIVE_ZSTD_CONCURRENCY", 8),
		ArchiveMaxConcurrent:   getEnvIntOrDefault("ARCHIVE_MAX_CONCURRENT", 1),
		StaleHandleTimeout:     getEnvDurationOrDefault("STALE_HANDLE_TIMEOUT", 5*time.Minute),
		CleanupInterval:        getEnvDurationOrDefault("CLEANUP_INTERVAL", 1*time.Minute),
		ReadHeaderTimeout:      getEnvDurationOrDefault("READ_HEADER_TIMEOUT", 10*time.Second),
		IdleTimeout:            getEnvDurationOrDefault("IDLE_TIMEOUT", 120*time.Second),
		ProfileEnabled:         getEnvBoolOrDefault("PROFILE_ENABLED", false),
		ProfileListenAddr:      getEnvOrDefault("PROFILE_LISTEN_ADDR", ":6060"),
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

func getEnvDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if durVal, err := time.ParseDuration(val); err == nil {
			return durVal
		}
	}
	return defaultVal
}
