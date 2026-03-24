#!/bin/bash
set -ex

# Initialize default exit code
RESULT=1

# 1. Pull a2a-samples and checkout revision
: "${A2A_SAMPLES_REVISION:?A2A_SAMPLES_REVISION environment variable must be set}"

# Cleanup function to be called on exit
cleanup() {
  set +x
  echo "Cleaning up artifacts..."
  docker stop itk-service > /dev/null 2>&1 || true
  docker rm itk-service > /dev/null 2>&1 || true
  docker rmi itk_service > /dev/null 2>&1 || true
  rm -rf a2a-samples > /dev/null 2>&1 || true
  rm -rf pb > /dev/null 2>&1 || true
  rm -f instruction.proto > /dev/null 2>&1 || true
  echo "Done. Final exit code: $RESULT"
}

# Register cleanup function to run on script exit
trap cleanup EXIT

if [ ! -d "a2a-samples" ]; then
  git clone https://github.com/a2aproject/a2a-samples.git a2a-samples
fi
cd a2a-samples
git fetch origin
git checkout "$A2A_SAMPLES_REVISION"

# Only pull if it's a branch (not a detached HEAD)
if git symbolic-ref -q HEAD > /dev/null; then
  git pull origin "$A2A_SAMPLES_REVISION"
fi
cd ..

# 2. Copy instruction.proto from a2a-samples
cp a2a-samples/itk/protos/instruction.proto ./instruction.proto

# 3. Build go pb library
# Ensure protoc-gen-go and protoc-gen-go-grpc are installed
export GOBIN=$HOME/go/bin
export PATH=$PATH:$GOBIN
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

mkdir -p pb
protoc -I. \
    --go_out=pb --go_opt=Minstruction.proto=github.com/a2aproject/a2a-go/itk/pb --go_opt=paths=source_relative \
    --go-grpc_out=pb --go-grpc_opt=Minstruction.proto=github.com/a2aproject/a2a-go/itk/pb --go-grpc_opt=paths=source_relative \
    instruction.proto

# 4. Synchronize go.mod
# We need to run go mod tidy because we might have added new dependencies or changed imports
go mod tidy

# 5. Build jit itk_service docker image from root of a2a-samples/itk
docker build -t itk_service a2a-samples/itk

# 6. Start docker service
A2A_GO_ROOT=$(cd .. && pwd)
ITK_DIR=$(pwd)

# Stop existing container if any
docker rm -f itk-service || true

# Run container with protobuf registration conflict set to 'warn'
# This is necessary because the SDK v2 depends on its predecessor v0.x,
# causing global proto registration conflicts.
docker run -d --name itk-service \
  -e GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn \
  -v "$A2A_GO_ROOT:/app/agents/repo" \
  -v "$ITK_DIR:/app/agents/repo/itk" \
  -p 8000:8000 \
  itk_service

# 6.1. Fix dubious ownership for git
docker exec itk-service git config --system --add safe.directory /app/agents/repo
docker exec itk-service git config --system --add safe.directory /app/agents/repo/itk
docker exec itk-service git config --system core.multiPackIndex false

# 7. Verify service is up and send post request
MAX_RETRIES=30
echo "Waiting for ITK service to start on 127.0.0.1:8000..."
set +e
for i in $(seq 1 $MAX_RETRIES); do
  if curl -s http://127.0.0.1:8000/ > /dev/null; then
    echo "Service is up!"
    break
  fi
  echo "Still waiting... ($i/$MAX_RETRIES)"
  sleep 2
done

if ! curl -s http://127.0.0.1:8000/ > /dev/null; then
  echo "Error: ITK service failed to start on port 8000"
  docker logs itk-service
  docker rm -f itk-service
  exit 1
fi

echo "ITK Service is up! Sending compatibility test request..."
RESPONSE=$(curl -s -X POST http://127.0.0.1:8000/run \
  -H "Content-Type: application/json" \
  -d '{
    "tests": [
      {
        "name": "Star Topology (Full) - JSONRPC",
        "sdks": ["current", "python_v10", "python_v03", "go_v10", "go_v03"],
        "traversal": "euler",
        "edges": ["0->1", "0->2", "0->3", "0->4", "1->0", "2->0", "3->0", "4->0"],
        "protocols": ["jsonrpc"]
      },
      {
        "name": "Star Topology (No Backwards Compatibility) - GRPC & HTTP_JSON",
        "sdks": ["current", "python_v10", "go_v10"],
        "traversal": "euler",
        "edges": ["0->1", "0->2", "1->0", "2->0"],
        "protocols": ["grpc", "http_json"]
      },
      {
        "name": "Star Topology (Full) - JSONRPC (Streaming)",
        "sdks": ["current", "python_v10", "python_v03", "go_v10", "go_v03"],
        "traversal": "euler",
        "edges": ["0->1", "0->2", "0->3", "0->4", "1->0", "2->0", "3->0", "4->0"],
        "protocols": ["jsonrpc"],
        "streaming": true
      },
      {
        "name": "Star Topology (No Backwards Compatibility) - GRPC & HTTP_JSON (Streaming)",
        "sdks": ["current", "python_v10", "go_v10"],
        "traversal": "euler",
        "edges": ["0->1", "0->2", "1->0", "2->0"],
        "protocols": ["grpc", "http_json"],
        "streaming": true
      }
    ]
  }')

echo "--------------------------------------------------------"
echo "ITK TEST RESULTS:"
echo "--------------------------------------------------------"
echo "$RESPONSE" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    all_passed = data.get('all_passed', False)
    results = data.get('results', {})
    for test, passed in results.items():
        status = 'PASSED' if passed else 'FAILED'
        print(f'{test}: {status}')
    print('--------------------------------------------------------')
    print(f'OVERALL STATUS: {\"PASSED\" if all_passed else \"FAILED\"}')
    if not all_passed:
        sys.exit(1)
except Exception as e:
    print(f'Error parsing results: {e}')
    print(f'Raw response: {data if \"data\" in locals() else \"no data\"}')
    sys.exit(1)
"
RESULT=$?
set -e

if [ $RESULT -ne 0 ]; then
  echo "Tests failed. Container logs:"
  docker logs itk-service
fi
echo "--------------------------------------------------------"

exit $RESULT
