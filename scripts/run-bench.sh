#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

EVIDENCE_DIR="${REPO_DIR}/.sisyphus/evidence"
mkdir -p "$EVIDENCE_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="${EVIDENCE_DIR}/task-5-bench-output-${TIMESTAMP}.txt"

echo "=== Running Benchmarks ==="
echo "Output will be saved to: $OUTPUT_FILE"

if go test ./... -bench=. -benchmem -count=1 2>&1 | tee "$OUTPUT_FILE"; then
    echo ""
    echo "=== Benchmarks Complete ==="
    echo "Evidence saved to: $OUTPUT_FILE"
else
    BENCH_EXIT_CODE=$?
    echo ""
    echo "=== Benchmarks Failed ==="
    echo "Exit code: $BENCH_EXIT_CODE"
    exit $BENCH_EXIT_CODE
fi
