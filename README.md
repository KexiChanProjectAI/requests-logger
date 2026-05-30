# OpenAI Proxy Logger

A fail-open HTTP proxy that logs all OpenAI API traffic to UTC-hourly JSONL files.

## Overview

This project provides two binaries:

- **proxy** - An HTTP proxy that forwards requests to OpenAI, logging each transaction asynchronously to a log server. The proxy never blocks OpenAI traffic, even if the log server is unavailable.
- **log-server** - An HTTP server that receives log records and writes them to UTC-hourly JSONL files.

## Architecture

```
Client  -->  [Proxy]  -->  OpenAI API
                  |
                  +----> [Log Client]  ---->  [Log Server]  ---->  JSONL Files (UTC-hourly)
```

1. Client sends HTTP request to proxy
2. Proxy forwards request to OpenAI API, keeping a copy of request/response
3. Proxy asynchronously sends log record to log server (non-blocking)
4. Log server writes each record as a JSON object per line to a file named by UTC hour
5. OpenAI response flows back to client regardless of logging outcome

## Environment Variables

Both `proxy` and `log-server` binaries read from environment variables.
Copy `config.example.env` to `.env` and customize for your deployment.

### Proxy Configuration

| Variable | Description | Default |
|---|---|---|
| LISTEN_ADDR | Address the proxy HTTP server listens on (e.g., `:8080`) | (empty) |
| UPSTREAM_BASE_URL | Upstream OpenAI API base URL | https://api.openai.com |
| LOG_SERVER_URL | Log server URL (e.g., `http://localhost:8081`) | (empty) |
| LOG_SERVER_TOKEN | Bearer token shared with log server for authentication | (empty) |
| LOG_QUEUE_SIZE | Size of the async log-record buffer (drops when full) | 1024 |
| LOG_CLIENT_WORKERS | Number of worker goroutines draining the log queue | 4 |
| LOG_CLIENT_MAX_RETRIES | Max retries per record on transient delivery failure | 3 |
| CAPTURE_MAX_BYTES | Max bytes captured from request/response bodies (0 = unlimited) | 0 |
| UPSTREAM_TIMEOUT | Full round-trip timeout for upstream API requests | 120s |
| UPSTREAM_MAX_IDLE_CONNS | Max idle connections in the pool (per upstream host) | 100 |
| UPSTREAM_IDLE_CONN_TIMEOUT | How long an idle connection stays alive | 90s |
| UPSTREAM_RESPONSE_HEADER_TIMEOUT | Timeout waiting for upstream response headers (0 = no timeout) | 60s |
| UPSTREAM_TLS_HANDSHAKE_TIMEOUT | TLS handshake timeout for upstream connections | 10s |
| UPSTREAM_DIAL_TIMEOUT | TCP dial timeout for establishing upstream connections | 30s |
| UPSTREAM_TLS_INSECURE | Skip upstream TLS certificate verification | false |
| UPSTREAM_TLS_SNI | Override TLS ServerName (SNI) for upstream HTTPS | (empty) |
| PROXY_TLS_CERT_FILE | Path to TLS cert for the proxy's own HTTPS listener (empty = HTTP) | (empty) |
| PROXY_TLS_KEY_FILE | Path to TLS key for the proxy's own HTTPS listener | (empty) |
| TRUSTED_PROXY_CIDRS | Comma-separated CIDRs of trusted reverse proxies (localhost always trusted) | (empty) |
| TRUSTED_PROXY_XFF_MODE | X-Forwarded-For mode: `append` (default) or `forward` | append |
| READ_HEADER_TIMEOUT | Timeout for reading request headers (slow-loris protection) | 10s |
| IDLE_TIMEOUT | Timeout for idle keep-alive connections | 120s |
| PROFILE_ENABLED | Enable pprof debug HTTP server | false |
| PROFILE_LISTEN_ADDR | Address for the pprof HTTP server | :6060 |

### Log Server Configuration

| Variable | Description | Default |
|---|---|---|
| LISTEN_ADDR | Address the log server listens on (e.g., `:8081`) | (empty) |
| LOG_SERVER_TOKEN | Bearer token for authenticating proxy log records | (empty) |
| LOG_DIR | Directory for UTC-hourly JSONL log files | (empty, current dir) |
| UTC_HOURLY_LAYOUT | Go time layout for hourly file naming in UTC | 2006/01/02/15 |
| ARCHIVE_ENABLED | Compress stale JSONL files to `.tar.zst` | true |
| ARCHIVE_ZSTD_WINDOW_MB | ZSTD search window in MiB (power of two, cap 512) | 512 |
| ARCHIVE_ZSTD_CONCURRENCY | ZSTD encoder concurrency per archive job | 8 |
| ARCHIVE_MAX_CONCURRENT | Maximum concurrent archive jobs | 1 |
| STALE_HANDLE_TIMEOUT | How long a file handle can be idle before closing | 5m |
| CLEANUP_INTERVAL | How often to scan for stale handles | 1m |
| READ_HEADER_TIMEOUT | Timeout for reading request headers (slow-loris protection) | 10s |
| IDLE_TIMEOUT | Timeout for idle keep-alive connections | 120s |
| PROFILE_ENABLED | Enable pprof debug HTTP server | false |
| PROFILE_LISTEN_ADDR | Address for the pprof HTTP server | :6060 |

## Quickstart

Build both binaries:

```bash
go build -o bin/proxy ./cmd/proxy
go build -o bin/log-server ./cmd/log-server
```

Set up environment variables:

```bash
source config.example.env
```

Start the log server in the background:

```bash
./bin/log-server &
```

Start the proxy:

```bash
./bin/proxy
```

## JSONL File Format

Log files are written to the directory specified by LOG_DIR, with one file per UTC hour. The file name matches the UTC_HOURLY_LAYOUT pattern.

Each line in a JSONL file is a valid JSON object with the following fields:

| Field | Type | Description |
|---|---|---|
| log_id | string | Unique identifier for this log record |
| request_id | string | HTTP request ID (X-Request-ID header or generated) |
| route | string | Request path |
| method | string | HTTP method |
| url | string | Full request URL |
| query | string | Query string |
| request_timestamp | string | RFC3339 timestamp when request was received |
| response_timestamp | string | RFC3339 timestamp when response was sent |
| duration_ms | int64 | Request duration in milliseconds |
| terminal_status | string | completed, client_disconnected, upstream_error, or proxy_error |
| request_headers | object | Request headers (keys with multiple values joined by comma) |
| request_body | any | Request body as JSON |
| response_headers | object | Response headers (keys with multiple values joined by comma) |
| response_body | any | Response body as JSON |
| upstream_status | int | HTTP status code from OpenAI API |
| error | string | Error message if any |
| stream | bool | Whether this was a streaming response |
| truncation_info | object | Present only when body was truncated (truncated, original_bytes, capture_max_bytes) |

The LOG_SERVER_TOKEN must be identical on both the proxy and the log server. This token is sent as a Bearer token in the Authorization header when the proxy posts log records to the log server. Choose a strong, random token in production.

## Migration Notes

### v0.9.0 — Trusted Proxy X-Forwarded-For Handling

The proxy now supports trusted reverse proxy CIDRs for secure `X-Forwarded-For` handling:

- `TRUSTED_PROXY_CIDRS`: Comma-separated list of CIDR ranges (e.g. `"10.0.0.0/8,192.168.0.0/16"`) whose `X-Forwarded-For` headers are trusted. `localhost` is always implicitly trusted. Requests from non-trusted sources have their `X-Forwarded-For` overwritten with the direct client IP, preventing spoofing.
- `TRUSTED_PROXY_XFF_MODE`: Controls how trusted proxies propagate the client IP:
  - `append` (default): Append the direct client IP to the existing `X-Forwarded-For` chain.
  - `forward`: Pass through the existing `X-Forwarded-For` header as-is, no modification.

### v0.8.0 — Gin Framework and HTTPS Listener

The proxy now uses Gin (`github.com/gin-gonic/gin`) as its HTTP framework, providing panic recovery middleware and improved routing:

- The proxy handler is mounted with `router.Any("/*path", ...)` — all paths and methods are proxied to the upstream.
- Gin is set to release mode by default to suppress debug logs.
- The proxy can now terminate TLS directly via `PROXY_TLS_CERT_FILE` and `PROXY_TLS_KEY_FILE`. When both are set the proxy listens on HTTPS; leave both empty for plain HTTP.

### v0.7.0 — Profiling and Graceful Shutdown

The proxy and log server now support opt-in pprof profiling and proper graceful shutdown:

- `PROFILE_ENABLED`: Set to `true` to enable a pprof debug HTTP server on `PROFILE_LISTEN_ADDR`.
- `PROFILE_LISTEN_ADDR`: Address for the pprof server (default `:6060`). Binds to all interfaces by default — restrict in production.
- Both `proxy` and `log-server` binaries now handle `SIGINT`/`SIGTERM` with graceful `Shutdown()`.

### v0.6.0 — Upstream TLS Configuration

The proxy now supports configurable TLS for upstream HTTPS connections:

- `UPSTREAM_TLS_INSECURE`: Set to `true` to skip TLS certificate verification for the upstream (e.g. self-signed or internal CA certificates).
- `UPSTREAM_TLS_SNI`: Override the TLS ServerName (SNI) sent during the upstream TLS handshake. Leave empty to use the hostname from `UPSTREAM_BASE_URL`.

### v0.5.0 — Multi-Worker Log Client

The async log client now uses multiple workers for higher throughput:

- `LOG_CLIENT_WORKERS`: Number of goroutines that drain the log queue and POST to the log server (default 4). Increase for higher throughput under load.
- `LOG_CLIENT_MAX_RETRIES`: Maximum retry attempts per record on transient delivery failure (default 3).

### v0.4.0 — Route-Based Log Filtering

The proxy can now selectively skip logging based on request path using configurable route policies:

- The route policy helper (`internal/proxy/route_policy.go`) evaluates allow/deny rules per-request.
- Log record enqueuing is gated by the route policy at all three capture points: non-streaming response, streaming finalization, and error paths.
- Route policy rules are compiled into the binary — configure via environment or code changes.

### v0.3.0 — Upstream HTTP Transport Timeouts

The upstream HTTP transport is now fully configurable with explicit timeout settings:

- `UPSTREAM_TIMEOUT`: Overall round-trip timeout for upstream API requests (default 120s).
- `UPSTREAM_MAX_IDLE_CONNS`: Maximum idle connections kept in the connection pool per upstream host (default 100).
- `UPSTREAM_IDLE_CONN_TIMEOUT`: How long an idle connection is kept alive before closing (default 90s).
- `UPSTREAM_RESPONSE_HEADER_TIMEOUT`: Timeout waiting for upstream response headers after sending the request (default 60s). Set to `0s` for no timeout (Go's default).
- `UPSTREAM_TLS_HANDSHAKE_TIMEOUT`: TLS handshake timeout for upstream connections (default 10s).
- `UPSTREAM_DIAL_TIMEOUT`: TCP dial timeout for establishing upstream connections (default 30s).

### v0.2.0 — Archive and Stale Handle Cleanup

The log server now automatically archives stale JSONL files and manages file handle lifecycles:

- `ARCHIVE_ENABLED`: Compress stale JSONL files to `.tar.zst` using zstd (default true).
- `ARCHIVE_ZSTD_WINDOW_MB`: ZSTD compression window in MiB, power of two, max 512 (default 512).
- `ARCHIVE_ZSTD_CONCURRENCY`: Concurrent encoders per archive job, 0 = `runtime.NumCPU` (default 8).
- `ARCHIVE_MAX_CONCURRENT`: Max simultaneous archive jobs (default 1).
- `STALE_HANDLE_TIMEOUT`: How long a file handle can be idle before closing (default 5m).
- `CLEANUP_INTERVAL`: How often stale handle cleanup runs (default 1m).

The `UTC_HOURLY_LAYOUT` default changed from `2006-01-02T00:00:00Z` to `2006/01/02/15` to produce more directory-friendly file paths.

