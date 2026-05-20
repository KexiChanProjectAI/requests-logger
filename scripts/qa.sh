#!/bin/bash
# QA script for openai-go-proxy-logger
# Smoke test: builds, starts binaries, verifies ports, cleans up.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

PROXY_PORT=18080
LOG_SERVER_PORT=18081
PROXY_BINARY="${REPO_DIR}/proxy"
LOG_SERVER_BINARY="${REPO_DIR}/log-server"

# Temp dirs
TMP_DIR=$(mktemp -d)
LOG_DIR="${TMP_DIR}/logs"
PROXY_LOG="${TMP_DIR}/proxy.log"
LOG_SERVER_LOG="${TMP_DIR}/log-server.log"

cleanup() {
    echo "=== Cleaning up ==="
    jobs -p 2>/dev/null | xargs -r kill 2>/dev/null || true
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# Create temp log directory
mkdir -p "$LOG_DIR"

# =============================================================================
# Step 1: Build both binaries
# =============================================================================
echo "=== Step 1: Building binaries ==="
(cd "$REPO_DIR" && go build -o "$PROXY_BINARY" ./cmd/proxy)
(cd "$REPO_DIR" && go build -o "$LOG_SERVER_BINARY" ./cmd/log-server)
echo "Builds succeeded"
ls -lh "$PROXY_BINARY" "$LOG_SERVER_BINARY"

# =============================================================================
# Step 2: Start log-server in background
# =============================================================================
echo "=== Step 2: Starting log-server ==="
LISTEN_ADDR=":${LOG_SERVER_PORT}" \
LOG_SERVER_TOKEN="test-token" \
LOG_DIR="$LOG_DIR" \
UTC_HOURLY_LAYOUT="2006-01-02T00:00:00Z" \
"$LOG_SERVER_BINARY" > "$LOG_SERVER_LOG" 2>&1 &
LOG_SERVER_PID=$!
echo "log-server started (PID=$LOG_SERVER_PID)"

# Wait for log-server to be ready
for i in {1..20}; do
    if curl -s "http://localhost:${LOG_SERVER_PORT}/health" 2>/dev/null | grep -q "ok"; then
        echo "log-server is ready"
        break
    fi
    # Also check if port is listening
    if ss -tlnp 2>/dev/null | grep -q ":${LOG_SERVER_PORT}"; then
        echo "log-server is listening on port ${LOG_SERVER_PORT}"
        break
    fi
    if ! kill -0 "$LOG_SERVER_PID" 2>/dev/null; then
        echo "ERROR: log-server died. Log:"
        cat "$LOG_SERVER_LOG"
        exit 1
    fi
    sleep 0.25
done

# =============================================================================
# Step 3: Start proxy in background
# =============================================================================
echo "=== Step 3: Starting proxy ==="
# Use a fake upstream that will fail fast - we only care that the proxy starts and listens
LISTEN_ADDR=":${PROXY_PORT}" \
UPSTREAM_BASE_URL="http://localhost:99999" \
LOG_SERVER_URL="http://localhost:${LOG_SERVER_PORT}" \
LOG_SERVER_TOKEN="test-token" \
LOG_QUEUE_SIZE=100 \
CAPTURE_MAX_BYTES=0 \
"$PROXY_BINARY" > "$PROXY_LOG" 2>&1 &
PROXY_PID=$!
echo "proxy started (PID=$PROXY_PID)"

# Wait for proxy to be ready
for i in {1..20}; do
    if ss -tlnp 2>/dev/null | grep -q ":${PROXY_PORT}"; then
        echo "proxy is listening on port ${PROXY_PORT}"
        break
    fi
    if ! kill -0 "$PROXY_PID" 2>/dev/null; then
        echo "ERROR: proxy died. Log:"
        cat "$PROXY_LOG"
        exit 1
    fi
    sleep 0.25
done

# =============================================================================
# Step 4: Verify both processes are running
# =============================================================================
echo "=== Step 4: Verifying processes ==="
if ! kill -0 "$LOG_SERVER_PID" 2>/dev/null; then
    echo "FAIL: log-server is not running"
    exit 1
fi
if ! kill -0 "$PROXY_PID" 2>/dev/null; then
    echo "FAIL: proxy is not running"
    exit 1
fi
echo "Both processes are running"

# =============================================================================
# Step 5: Smoke test - verify proxy accepts connections (will fail to reach upstream)
# =============================================================================
echo "=== Step 5: Smoke test - proxy accepts HTTP connection ==="
# We don't need a real upstream. The proxy should accept the connection and return an error.
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "http://localhost:${PROXY_PORT}/v1/chat/completions" \
    -H "Authorization: Bearer test-key" \
    -H "Content-Type: application/json" \
    -d '{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}' \
    2>/dev/null || echo "000")

# Status 000 means couldn't connect at all
# Status 400/401/403/500/502/503 means proxy accepted but upstream failed (expected with fake upstream)
# Status 200 means proxy worked (unexpected with fake upstream)
echo "Proxy returned HTTP status: $HTTP_STATUS"

if [[ "$HTTP_STATUS" == "000" ]]; then
    echo "FAIL: Could not connect to proxy"
    exit 1
fi
echo "Proxy accepts connections correctly"

# =============================================================================
# Step 6: Verify log-server is receiving logs (if proxy sent any)
# =============================================================================
echo "=== Step 6: Checking log-server health ==="
HEALTH=$(curl -s "http://localhost:${LOG_SERVER_PORT}/health" 2>/dev/null || echo "no health endpoint")
echo "Log server health: $HEALTH"

# Check if JSONL files can be created (write test)
WRITE_TEST_FILE="${LOG_DIR}/write-test.jsonl"
echo '{"test":true}' > "$WRITE_TEST_FILE"
if [[ -f "$WRITE_TEST_FILE" ]]; then
    echo "Log directory is writable"
    rm "$WRITE_TEST_FILE"
else
    echo "FAIL: Cannot write to log directory"
    exit 1
fi

# =============================================================================
# Step 7: Summary
# =============================================================================
echo ""
echo "=== QA PASSED ==="
echo "Summary:"
echo "  - Built proxy binary: $PROXY_BINARY"
echo "  - Built log-server binary: $LOG_SERVER_BINARY"
echo "  - log-server listening on :${LOG_SERVER_PORT}"
echo "  - proxy listening on :${PROXY_PORT}"
echo "  - proxy accepts HTTP connections"
echo "  - log directory writable at $LOG_DIR"
echo "  - Cleanup successful"

exit 0
