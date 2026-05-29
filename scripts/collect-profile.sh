#!/bin/bash
# Profile collection script for openai-go-proxy-logger
# Collects CPU, heap, and goroutine profiles from pprof endpoints
#
# Usage:
#   ./collect-profile.sh <profile_type> <duration> <port> [output_name]
#   Profile types: cpu, heap, goroutine, threadcreate, block, mutex
#   Duration: for cpu profile, time in seconds to collect; use 1 for heap/goroutine
#   Port: the profile server port (from PROFILE_LISTEN_ADDR)
#   Output name: optional, defaults to profile type
#
# Examples:
#   ./collect-profile.sh cpu 30 6060           # Collect 30s CPU profile on port 6060
#   ./collect-profile.sh heap 1 6060           # Collect heap profile on port 6060
#   ./collect-profile.sh goroutine 1 6060       # Collect goroutine profile on port 6060

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
EVIDENCE_DIR="${REPO_DIR}/.sisyphus/evidence"

# Ensure evidence directory exists
mkdir -p "$EVIDENCE_DIR"

# Check arguments
if [ $# -lt 3 ]; then
    echo "Usage: $0 <profile_type> <duration> <port> [output_name]"
    echo "  profile_type: cpu, heap, goroutine, threadcreate, block, mutex"
    echo "  duration: seconds for cpu profile, 1 for others"
    echo "  port: the profiling server port"
    echo "  output_name: optional output file name suffix"
    exit 1
fi

PROFILE_TYPE="$1"
DURATION="$2"
PORT="$3"
OUTPUT_NAME="${4:-${PROFILE_TYPE}}"

# Validate profile type
VALID_TYPES=("cpu" "heap" "goroutine" "threadcreate" "block" "mutex")
if [[ ! " ${VALID_TYPES[*]} " =~ " ${PROFILE_TYPE} " ]]; then
    echo "Error: Invalid profile type '$PROFILE_TYPE'"
    echo "Valid types: ${VALID_TYPES[*]}"
    exit 1
fi

# Build URL
URL="http://localhost:${PORT}/debug/pprof/${PROFILE_TYPE}"

# Add seconds parameter for CPU profile
if [ "$PROFILE_TYPE" = "cpu" ]; then
    URL="${URL}?seconds=${DURATION}"
fi

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="${EVIDENCE_DIR}/profile_${OUTPUT_NAME}_${TIMESTAMP}.prof"

echo "=== Profile Collection ==="
echo "Type: $PROFILE_TYPE"
echo "URL: $URL"
echo "Output: $OUTPUT_FILE"

# Check if port is reachable first
if ! curl -s --max-time 5 "http://localhost:${PORT}/debug/pprof/" > /dev/null 2>&1; then
    echo "Error: Cannot reach profiling server on port ${PORT}"
    echo "Make sure PROFILE_ENABLED=true and the server is running"
    exit 1
fi

# Collect profile
echo "Collecting ${PROFILE_TYPE} profile..."
curl -s -o "$OUTPUT_FILE" "$URL"

# Verify we got a valid profile
if [ ! -s "$OUTPUT_FILE" ]; then
    echo "Error: Empty response from pprof endpoint"
    exit 1
fi

# Check file size
SIZE=$(stat -c%s "$OUTPUT_FILE" 2>/dev/null || stat -f%z "$OUTPUT_FILE" 2>/dev/null)
echo "Profile collected successfully: ${SIZE} bytes"

# For CPU profiles, also get the corresponding heap profile for context
if [ "$PROFILE_TYPE" = "cpu" ]; then
    HEAP_OUTPUT="${EVIDENCE_DIR}/profile_${OUTPUT_NAME}_${TIMESTAMP}_heap.prof"
    echo "Collecting associated heap profile..."
    curl -s -o "$HEAP_OUTPUT" "http://localhost:${PORT}/debug/pprof/heap?seconds=1"
    HEAP_SIZE=$(stat -c%s "$HEAP_OUTPUT" 2>/dev/null || stat -f%z "$HEAP_OUTPUT" 2>/dev/null)
    echo "Heap profile collected: ${HEAP_SIZE} bytes"
fi

echo ""
echo "=== Collection Complete ==="
echo "Files saved:"
ls -lh "$OUTPUT_FILE" "${HEAP_OUTPUT:-/dev/null}" 2>/dev/null || true
