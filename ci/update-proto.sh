#!/bin/bash

# Script to check and update A2A proto definitions from the official repository
# This ensures we're always using the canonical protocol definitions
#
# Usage:
#   ./update-proto.sh           # Check and update if needed
#   ./update-proto.sh --check   # Check only (CI mode)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUF_GEN_FILE="${PROJECT_ROOT}/buf.gen.yaml"

# Check if this is a check-only run
CHECK_ONLY=false
if [[ "${1:-}" == "--check" ]]; then
    CHECK_ONLY=true
fi

echo "Checking A2A proto definitions..."

# Get current version from buf.gen.yaml
CURRENT_REF=""
if [[ -f "${BUF_GEN_FILE}" ]]; then
    CURRENT_REF=$(grep "ref:" "${BUF_GEN_FILE}" | sed 's/.*ref: *//' | tr -d ' ' || echo "")
fi

# Get latest available tag from the official repo
LATEST_REF=$(curl -fsSL "https://api.github.com/repos/a2aproject/A2A/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' || echo "unknown")

# Compare versions
if [[ -n "$CURRENT_REF" && "$CURRENT_REF" == "$LATEST_REF" && "$LATEST_REF" != "unknown" ]]; then
    echo "✓ Proto definitions are up to date (version: ${CURRENT_REF})"

    # Check if generated files exist
    if [[ -d "${PROJECT_ROOT}/grpc/a2a/v1" ]] && [[ -f "${PROJECT_ROOT}/grpc/a2a/v1/a2a.pb.go" ]]; then
        echo "✓ Generated files are present"
        exit 0
    else
        echo "⚠ Generated files missing, regenerating..."
        # Continue to generation step
    fi
elif [[ "$CHECK_ONLY" == "true" ]]; then
    if [[ -z "$CURRENT_REF" ]]; then
        echo "✗ No buf.gen.yaml found or missing ref"
    else
        echo "✗ Proto definitions are outdated (current: ${CURRENT_REF}, latest: ${LATEST_REF})"
    fi
    echo "Run 'make update-proto' to update to the latest version"
    exit 1
else
    if [[ -z "$CURRENT_REF" ]]; then
        echo "No existing version found, updating to latest..."
    else
        echo "Proto definitions are outdated (current: ${CURRENT_REF}, latest: ${LATEST_REF})"
        echo "Updating to latest version..."
    fi
fi

# Update buf.gen.yaml with latest version
echo "Updating buf.gen.yaml to version ${LATEST_REF}..."
if command -v yq &> /dev/null; then
    yq e ".inputs[0].ref = \"${LATEST_REF}\"" -i "${BUF_GEN_FILE}"
    echo "Updated buf.gen.yaml using yq."
else
    # Fallback to sed if yq is not available
    sed -i.backup "s/ref: .*/ref: ${LATEST_REF}/" "${BUF_GEN_FILE}"
    echo "Updated buf.gen.yaml using sed (yq not found)."
fi

echo "Successfully updated A2A proto version reference"

# Generate Go code
echo ""
echo "Generating Go code from proto definitions..."
cd "${PROJECT_ROOT}"

# Ensure buf dependencies are available
"$(go env GOPATH)/bin/buf" dep update 2>/dev/null || true

# Generate the code
"$(go env GOPATH)/bin/buf" generate

echo ""
echo "✓ Proto update and code generation complete"
echo ""
echo "Summary:"
echo "========"
echo "Updated version: ${LATEST_REF}"
echo "Generated files: ${PROJECT_ROOT}/grpc/a2a/v1/"