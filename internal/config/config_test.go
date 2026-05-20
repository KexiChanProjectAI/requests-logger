package config

import (
	"os"
	"testing"
)

func TestProxyConfigDefaults(t *testing.T) {
	unsetAllProxyEnv()
	defer unsetAllProxyEnv()

	cfg := LoadProxyConfig()

	if cfg.ListenAddr != "" {
		t.Errorf("expected empty ListenAddr, got %q", cfg.ListenAddr)
	}
	if cfg.UpstreamBaseURL != "https://api.openai.com" {
		t.Errorf("expected UpstreamBaseURL default https://api.openai.com, got %q", cfg.UpstreamBaseURL)
	}
	if cfg.LogServerURL != "" {
		t.Errorf("expected empty LogServerURL, got %q", cfg.LogServerURL)
	}
	if cfg.LogServerToken != "" {
		t.Errorf("expected empty LogServerToken, got %q", cfg.LogServerToken)
	}
	if cfg.LogQueueSize != 1024 {
		t.Errorf("expected LogQueueSize default 1024, got %d", cfg.LogQueueSize)
	}
	if cfg.CaptureMaxBytes != 0 {
		t.Errorf("expected CaptureMaxBytes default 0 (unlimited), got %d", cfg.CaptureMaxBytes)
	}
}

func TestProxyConfigEnvOverrides(t *testing.T) {
	unsetAllProxyEnv()
	os.Setenv("LISTEN_ADDR", ":8080")
	os.Setenv("UPSTREAM_BASE_URL", "https://api.openai.com/v1")
	os.Setenv("LOG_SERVER_URL", "http://logserver:9090")
	os.Setenv("LOG_SERVER_TOKEN", "secret-token")
	os.Setenv("LOG_QUEUE_SIZE", "2048")
	os.Setenv("CAPTURE_MAX_BYTES", "4096")
	defer unsetAllProxyEnv()

	cfg := LoadProxyConfig()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected ListenAddr :8080, got %q", cfg.ListenAddr)
	}
	if cfg.UpstreamBaseURL != "https://api.openai.com/v1" {
		t.Errorf("expected UpstreamBaseURL https://api.openai.com/v1, got %q", cfg.UpstreamBaseURL)
	}
	if cfg.LogServerURL != "http://logserver:9090" {
		t.Errorf("expected LogServerURL http://logserver:9090, got %q", cfg.LogServerURL)
	}
	if cfg.LogServerToken != "secret-token" {
		t.Errorf("expected LogServerToken secret-token, got %q", cfg.LogServerToken)
	}
	if cfg.LogQueueSize != 2048 {
		t.Errorf("expected LogQueueSize 2048, got %d", cfg.LogQueueSize)
	}
	if cfg.CaptureMaxBytes != 4096 {
		t.Errorf("expected CaptureMaxBytes 4096, got %d", cfg.CaptureMaxBytes)
	}
}

func TestLogServerConfigDefaults(t *testing.T) {
	unsetAllLogServerEnv()
	defer unsetAllLogServerEnv()

	cfg := LoadLogServerConfig()

	if cfg.ListenAddr != "" {
		t.Errorf("expected empty ListenAddr, got %q", cfg.ListenAddr)
	}
	if cfg.LogServerToken != "" {
		t.Errorf("expected empty LogServerToken, got %q", cfg.LogServerToken)
	}
	if cfg.LogDir != "" {
		t.Errorf("expected empty LogDir, got %q", cfg.LogDir)
	}
	if cfg.UTCHourlyLayout != "2006/01/02/15" {
		t.Errorf("expected UTCHourlyLayout default 2006/01/02/15, got %q", cfg.UTCHourlyLayout)
	}
	if !cfg.ArchiveEnabled {
		t.Errorf("expected ArchiveEnabled default true, got %v", cfg.ArchiveEnabled)
	}
}

func TestLogServerConfigEnvOverrides(t *testing.T) {
	unsetAllLogServerEnv()
	os.Setenv("LISTEN_ADDR", ":9090")
	os.Setenv("LOG_SERVER_TOKEN", "log-token")
	os.Setenv("LOG_DIR", "/var/log/openai-proxy")
	os.Setenv("UTC_HOURLY_LAYOUT", "2006-01-02-15")
	os.Setenv("ARCHIVE_ENABLED", "false")
	defer unsetAllLogServerEnv()

	cfg := LoadLogServerConfig()

	if cfg.ListenAddr != ":9090" {
		t.Errorf("expected ListenAddr :9090, got %q", cfg.ListenAddr)
	}
	if cfg.LogServerToken != "log-token" {
		t.Errorf("expected LogServerToken log-token, got %q", cfg.LogServerToken)
	}
	if cfg.LogDir != "/var/log/openai-proxy" {
		t.Errorf("expected LogDir /var/log/openai-proxy, got %q", cfg.LogDir)
	}
	if cfg.UTCHourlyLayout != "2006-01-02-15" {
		t.Errorf("expected UTCHourlyLayout 2006-01-02-15, got %q", cfg.UTCHourlyLayout)
	}
	if cfg.ArchiveEnabled {
		t.Errorf("expected ArchiveEnabled false, got %v", cfg.ArchiveEnabled)
	}
}

func TestCaptureMaxBytesZeroIsUnlimited(t *testing.T) {
	unsetAllProxyEnv()
	os.Setenv("CAPTURE_MAX_BYTES", "0")
	defer unsetAllProxyEnv()

	cfg := LoadProxyConfig()
	if cfg.CaptureMaxBytes != 0 {
		t.Errorf("expected CaptureMaxBytes 0 (unlimited), got %d", cfg.CaptureMaxBytes)
	}
}

func unsetAllProxyEnv() {
	os.Unsetenv("LISTEN_ADDR")
	os.Unsetenv("UPSTREAM_BASE_URL")
	os.Unsetenv("LOG_SERVER_URL")
	os.Unsetenv("LOG_SERVER_TOKEN")
	os.Unsetenv("LOG_QUEUE_SIZE")
	os.Unsetenv("CAPTURE_MAX_BYTES")
}

func unsetAllLogServerEnv() {
	os.Unsetenv("LISTEN_ADDR")
	os.Unsetenv("LOG_SERVER_TOKEN")
	os.Unsetenv("LOG_DIR")
	os.Unsetenv("UTC_HOURLY_LAYOUT")
	os.Unsetenv("ARCHIVE_ENABLED")
}
