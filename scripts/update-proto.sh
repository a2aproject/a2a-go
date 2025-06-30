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
PROTO_DIR="${PROJECT_ROOT}/proto/a2a/v1"
OFFICIAL_REPO="https://raw.githubusercontent.com/a2aproject/A2A/main/specification/grpc"

# Check if this is a check-only run
CHECK_ONLY=false
if [[ "${1:-}" == "--check" ]]; then
    CHECK_ONLY=true
fi

echo "Checking A2A proto definitions..."

# Get the latest commit hash from the official repo
LATEST_COMMIT=$(curl -fsSL "https://api.github.com/repos/a2aproject/A2A/commits?path=specification/grpc/a2a.proto&per_page=1" 2>/dev/null | sed -n 's/.*"sha": *"\([^"]*\)".*/\1/p' | head -1 || echo "unknown")

# Check if we have an existing proto file with metadata
CURRENT_COMMIT=""
if [[ -f "${PROTO_DIR}/a2a.proto" ]]; then
    CURRENT_COMMIT=$(grep "^// Official commit:" "${PROTO_DIR}/a2a.proto" 2>/dev/null | cut -d' ' -f4 || echo "")
fi

# Compare commits
if [[ -n "$CURRENT_COMMIT" && "$CURRENT_COMMIT" == "$LATEST_COMMIT" && "$LATEST_COMMIT" != "unknown" ]]; then
    echo "✓ Proto definitions are up to date (commit: ${CURRENT_COMMIT:0:8})"
    
    # Check if generated files exist
    if [[ -d "${PROJECT_ROOT}/generated/a2a/v1" ]]; then
        echo "✓ Generated files are present"
        exit 0
    else
        echo "⚠ Generated files missing, regenerating..."
        # Continue to generation step
    fi
elif [[ "$CHECK_ONLY" == "true" ]]; then
    if [[ -z "$CURRENT_COMMIT" ]]; then
        echo "✗ No proto file found or missing metadata"
    else
        echo "✗ Proto definitions are outdated (current: ${CURRENT_COMMIT:0:8}, latest: ${LATEST_COMMIT:0:8})"
    fi
    echo "Run 'make update-proto' to update to the latest version"
    exit 1
else
    if [[ -z "$CURRENT_COMMIT" ]]; then
        echo "No existing proto file found, downloading latest..."
    else
        echo "Proto definitions are outdated (current: ${CURRENT_COMMIT:0:8}, latest: ${LATEST_COMMIT:0:8})"
        echo "Updating to latest version..."
    fi
fi

# Create proto directory if it doesn't exist
mkdir -p "${PROTO_DIR}"

# Download fresh proto file
echo "Downloading official A2A proto definition..."
TEMP_PROTO=$(mktemp)
curl -fsSL "${OFFICIAL_REPO}/a2a.proto" -o "${TEMP_PROTO}"

# Verify download
if [[ ! -f "${TEMP_PROTO}" ]] || [[ ! -s "${TEMP_PROTO}" ]]; then
    echo "Error: Failed to download proto file or file is empty"
    rm -f "${TEMP_PROTO}"
    exit 1
fi

# Backup existing files if they exist
if [[ -f "${PROTO_DIR}/a2a.proto" ]]; then
    BACKUP_TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
    echo "Backing up existing proto file..."
    cp "${PROTO_DIR}/a2a.proto" "${PROTO_DIR}/a2a.proto.backup.${BACKUP_TIMESTAMP}"
fi

# Get metadata
FETCH_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Add metadata header and update Go package option
{
    echo "// This file was automatically downloaded from the official A2A specification repository"
    echo "// Source: https://github.com/a2aproject/A2A/blob/main/specification/grpc/a2a.proto"
    echo "// Fetched: ${FETCH_TIMESTAMP}"
    echo "// Official commit: ${LATEST_COMMIT}"
    echo "//"
    sed 's|^option go_package.*|option go_package = "github.com/a2aproject/a2a-go/generated/a2a/v1;a2av1";|' "${TEMP_PROTO}"
} > "${PROTO_DIR}/a2a.proto"

rm -f "${TEMP_PROTO}"

echo "Successfully updated A2A proto definitions"
echo "Proto file location: ${PROTO_DIR}/a2a.proto"

# Generate Go code
echo ""
echo "Generating Go code from proto definitions..."
cd "${PROJECT_ROOT}/proto"

# Ensure buf dependencies are available
"$(go env GOPATH)/bin/buf" dep update 2>/dev/null || true

# Generate the code
"$(go env GOPATH)/bin/buf" generate

echo ""
echo "✓ Proto update and code generation complete"
echo ""
echo "Summary:"
echo "========"
echo "Fetched: ${FETCH_TIMESTAMP}"
echo "Official commit: ${LATEST_COMMIT:0:8}"
echo "Generated files: ${PROJECT_ROOT}/generated/a2a/v1/"