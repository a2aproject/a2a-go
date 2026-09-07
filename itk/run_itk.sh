#!/bin/bash
# ITK harness for a2a-go — a thin shim over a2a-itk's shared driver.
#
# Everything that used to live here (clone, image build, container start,
# readiness poll, POST /run, result reporting, nightly metrics) is now in
# a2a-itk/scripts/run_itk_shared.sh, which all five SDK repos share. Only the
# genuinely go-specific parts stay: generating the proto stubs, and the
# registration-conflict env the container needs.
#
# Scenarios come from the shared role-based set in a2a-itk rather than a
# scenarios.json in this repo — see a2a-itk/scenarios/traversal/.
set -e
cd "$(dirname "${BASH_SOURCE[0]}")"

ITK_SDK_NAME=go
ITK_SCENARIO_SET=shared

# The SDK's v2 module depends on its v0.x predecessor, so both register the
# same proto names in the global registry; downgrade the clash from fatal.
ITK_EXTRA_DOCKER_ARGS=(-e GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn)

itk_generate_protos() {
  export GOBIN="$HOME/go/bin"
  export PATH="$PATH:$GOBIN"
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

  mkdir -p pb
  protoc -I. \
      --go_out=pb --go_opt=Minstruction.proto=github.com/a2aproject/a2a-go/itk/pb --go_opt=paths=source_relative \
      --go-grpc_out=pb --go-grpc_opt=Minstruction.proto=github.com/a2aproject/a2a-go/itk/pb --go-grpc_opt=paths=source_relative \
      instruction.proto

  # go.sum is committed so the agent builds reproducibly; -diff fails instead
  # of silently rewriting it the way `go mod tidy` would.
  go mod tidy -diff
}

itk_extra_cleanup() {
  rm -rf pb
}

# --- bootstrap -------------------------------------------------------------
# The shared driver lives in a2a-itk, so the checkout has to exist before it
# can be sourced. CI has already placed it here via actions/checkout; locally
# we clone it from a2aproject/a2a-itk.
: "${A2A_ITK_REVISION:?A2A_ITK_REVISION environment variable must be set}"
if [ ! -d a2a-itk ]; then
  git clone https://github.com/a2aproject/a2a-itk.git a2a-itk
  git -C a2a-itk checkout "$A2A_ITK_REVISION"
fi

source a2a-itk/scripts/run_itk_shared.sh
