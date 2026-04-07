#!/bin/bash
# Clones the published A2A gRPC spec, renames the proto file to a2av1.proto,
# and generates Go types in ./a2apb/v1
#
# The rename is intentional: the protobuf file descriptor registers as
# "a2av1.proto" instead of "a2a.proto", avoiding a name clash with other
# protocol versions (e.g. a2a-go 0.3).
#
# Ensure $GOBIN is in path and dependencies are installed:
# > go install github.com/bufbuild/buf/cmd/buf@latest
# > go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
# > go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#
# Then run:
# > ./tools/generate_types.sh

set -euo pipefail

RENAME_TO="a2av1"
REPO_URL="https://github.com/a2aproject/A2A.git"
COMMIT="7df7685561b19c841c5a1349653df347db8d3314"
PROTO_SUBDIR="specification"
OUTPUT_DIR="./a2apb/v1"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Cloning A2A spec at ${COMMIT:0:12}..."
git clone --no-checkout --quiet "$REPO_URL" "$TMPDIR/A2A"
git -C "$TMPDIR/A2A" checkout --quiet "$COMMIT" -- "$PROTO_SUBDIR"

echo "Renaming a2a.proto -> $RENAME_TO.proto..."
mv "$TMPDIR/A2A/$PROTO_SUBDIR/a2a.proto" "$TMPDIR/A2A/$PROTO_SUBDIR/$RENAME_TO.proto"

cat > "$TMPDIR/buf.gen.yaml" <<EOF
version: v2
inputs:
  - directory: $TMPDIR/A2A/$PROTO_SUBDIR

managed:
  enabled: true
  override:
    - file_option: go_package
      path: $RENAME_TO.proto
      value: github.com/a2aproject/a2a-go/a2apb

plugins:
  - remote: buf.build/protocolbuffers/go
    out: $OUTPUT_DIR
    opt:
      - paths=source_relative

  - remote: buf.build/grpc/go
    out: $OUTPUT_DIR
    opt:
      - paths=source_relative
EOF

echo "Generating Go code..."
buf generate --template "$TMPDIR/buf.gen.yaml"

echo "Done: ${OUTPUT_DIR}/${RENAME_TO}.pb.go, ${OUTPUT_DIR}/${RENAME_TO}_grpc.pb.go"