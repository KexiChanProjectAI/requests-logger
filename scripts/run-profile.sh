#!/bin/bash
# Profile runner for openai-go-proxy-logger
# NOTE: Profiling is not yet implemented. This script starts the servers
# for manual profiling if/when the feature is added.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

EVIDENCE_DIR="${REPO_DIR}/.sisyphus/evidence"
mkdir -p "$EVIDENCE_DIR"

PROXY_PORT=18080
LOG_SERVER_PORT=18081
PROXY_BINARY="${REPO_DIR}/proxy"
LOG_SERVER_BINARY="${REPO_DIR}/log-server"

PROXY_PROFILE_PORT=6060
LOG_SERVER_PROFILE_PORT=6061

TMP_DIR=$(mktemp -d)
LOG_DIR="${TMP_DIR}/logs"

LOG_SERVER_PID=""
PROXY_PID=""

cleanup() {
    echo "=== Cleaning up ==="
    if [[ -n "${LOG_SERVER_PID:-}" ]] && kill -0 "$LOG_SERVER_PID" 2>/dev/null; then
        kill -TERM "$LOG_SERVER_PID" 2>/dev/null || true
        for i in {1..50}; do
            if ! kill -0 "$LOG_SERVER_PID" 2>/dev/null; then
                break
            fi
            sleep 0.1
        done
        if kill -0 "$LOG_SERVER_PID" 2>/dev/null; then
            kill -9 "$LOG_SERVER_PID" 2>/dev/null || true
        fi
    fi
    if [[ -n "${PROXY_PID:-}" ]] && kill -0 "$PROXY_PID" 2>/dev/null; then
        kill -TERM "$PROXY_PID" 2>/dev/null || true
        for i in {1..50}; do
            if ! kill -0 "$PROXY_PID" 2>/dev/null; then
                break
            fi
            sleep 0.1
        done
        if kill -0 "$PROXY_PID" 2>/dev/null; then
            kill -9 "$PROXY_PID" 2>/dev/null || true
        fi
    fi
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$LOG_DIR"

echo "=== Building binaries ==="
(cd "$REPO_DIR" && go build -o "$PROXY_BINARY" ./cmd/proxy)
(cd "$REPO_DIR" && go build -o "$LOG_SERVER_BINARY" ./cmd/log-server)

echo "=== Starting log-server ==="
LISTEN_ADDR=":${LOG_SERVER_PORT}" \
LOG_SERVER_TOKEN="test-token" \
LOG_DIR="$LOG_DIR" \
UTC_HOURLY_LAYOUT="2006-01-02T00:00:00Z" \
"$LOG_SERVER_BINARY" > /dev/null 2>&1 &
LOG_SERVER_PID=$!
echo "log-server started (PID=$LOG_SERVER_PID)"

echo "=== Starting proxy ==="
LISTEN_ADDR=":${PROXY_PORT}" \
UPSTREAM_BASE_URL="http://localhost:99999" \
LOG_SERVER_URL="http://localhost:${LOG_SERVER_PORT}" \
LOG_SERVER_TOKEN="test-token" \
LOG_QUEUE_SIZE=100 \
"$PROXY_BINARY" > /dev/null 2>&1 &
PROXY_PID=$!
echo "proxy started (PID=$PROXY_PID)"

echo "=== Waiting for servers to be ready ==="
sleep 2

echo ""
echo "=== Servers running ==="
echo "Proxy: localhost:${PROXY_PORT}"
echo "Log-server: localhost:${LOG_SERVER_PORT}"
echo ""
echo "NOTE: Profiling endpoints not available - feature not yet implemented."
echo ""
echo "To manually profile:"
echo "  1. Import 'net/http/pprof' in cmd/proxy/main.go and cmd/log-server/main.go"
echo "  2. Add pprof server startup in each main.go"
echo "  3. Run this script again"
echo ""
echo "Servers will stay running until killed. Use: kill $PROXY_PID $LOG_SERVER_PID"

sleep infinity
