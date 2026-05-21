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

### Proxy Configuration

| Variable | Description | Default |
|---|---|---|
| LISTEN_ADDR | Address the proxy listens on (e.g., `:8080`) | (empty) |
| UPSTREAM_BASE_URL | OpenAI API base URL | https://api.openai.com |
| LOG_SERVER_URL | Log server URL (e.g., `http://localhost:8081`) | (empty) |
| LOG_SERVER_TOKEN | Bearer token shared with log server for authentication | (empty) |
| LOG_QUEUE_SIZE | Size of the async log queue | 1024 |
| CAPTURE_MAX_BYTES | Max bytes to capture from request/response bodies (0 = unlimited) | 0 |

### Log Server Configuration

| Variable | Description | Default |
|---|---|---|
| LISTEN_ADDR | Address the log server listens on (e.g., `:8081`) | (empty) |
| LOG_SERVER_TOKEN | Bearer token for authenticating proxy requests | (empty) |
| LOG_DIR | Directory for JSONL log files | (empty, current directory) |
| UTC_HOURLY_LAYOUT | Go time layout for hourly file naming in UTC | 2006-01-02T00:00:00Z |
| ARCHIVE_ENABLED | Compress stale JSONL files to `.tar.zst` | true |
| ARCHIVE_ZSTD_WINDOW_MB | ZSTD search window in MiB, power of two, capped at 512 | 512 |
| ARCHIVE_ZSTD_CONCURRENCY | ZSTD encoder concurrency per archive job | 8 |
| ARCHIVE_MAX_CONCURRENT | Maximum archive jobs running simultaneously | 1 |

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

## Security Note

The LOG_SERVER_TOKEN must be identical on both the proxy and the log server. This token is sent as a Bearer token in the Authorization header when the proxy posts log records to the log server. Choose a strong, random token in production.
