# a2a-go
Golang SDK for A2A Protocol

## Generating Code

### gRPC

#### Prerequisites
Before you can validate or generate code from these protobuf definitions, you need to install buf.

Follow the installation instructions on the official buf GitHub repository: https://github.com/bufbuild/buf/

#### Steps
Clone the [A2A Project](https://github.com/a2aproject/A2A/tree/main) in a separate directory.
```
git clone https://github.com/a2aproject/A2A.git
```
Navigate to the gRPC specification.
```
cd A2A/specification/grpc
```
Execute `buf generate` to generate the code.  You need to have the [buf tool](https://github.com/bufbuild/buf/) installed.
```
buf generate
```
Copy the Golang gRPC files to this project.
```
cp -rf src/go/google.golang.org/a2a/v1/a2a* ${WORKSPACE_DIR}/a2a-go/generated/grpc/v1
```