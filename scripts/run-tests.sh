#!/bin/bash
# Test runner with evidence capture
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

EVIDENCE_DIR="${REPO_DIR}/.sisyphus/evidence"
mkdir -p "$EVIDENCE_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="${EVIDENCE_DIR}/task-5-test-output-${TIMESTAMP}.txt"
ERROR_FILE="${EVIDENCE_DIR}/task-5-qa-commands-error.txt"

echo "=== Running Tests ==="
echo "Output will be saved to: $OUTPUT_FILE"

# Run tests and capture output
if go test ./... -count=1 -v 2>&1 | tee "$OUTPUT_FILE"; then
    echo ""
    echo "=== Tests PASSED ==="
    echo "Evidence saved to: $OUTPUT_FILE"
    
    # Also save a copy with pass indicator
    {
        echo "Test Run - ${TIMESTAMP}"
        echo "Status: PASSED"
    } >> "$OUTPUT_FILE"
else
    TEST_EXIT_CODE=$?
    echo ""
    echo "=== Tests FAILED ==="
    
    # Save error evidence
    {
        echo "Test Run - ${TIMESTAMP}"
        echo "Status: FAILED (exit code: $TEST_EXIT_CODE)"
        echo ""
        echo "Last 100 lines of test output:"
        tail -100 "$OUTPUT_FILE"
    } > "$ERROR_FILE"
    
    echo "Error evidence saved to: $ERROR_FILE"
    exit $TEST_EXIT_CODE
fi
