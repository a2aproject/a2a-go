# A2A Protocol Buffer Definitions

This directory contains the official Protocol Buffer definitions for the Agent-to-Agent (A2A) Protocol, automatically synchronized with the [A2A specification repository](https://github.com/a2aproject/A2A).

In turn the protocol definitions are used to generate the Go SDK code.

- **gRPC Service Interface**: Complete A2A service with all protocol methods
- **Core Data Types**: Task, Message, AgentCard, and all supporting structures
- **Authentication**: Security schemes and authentication configurations  
- **Streaming Support**: Real-time task updates via gRPC streaming
- **Google API Annotations**: REST API compatibility through HTTP/JSON mapping

## Automated Updates

### Update Mechanism
The proto definitions are automatically kept current through:

1. **Script-based Updates**: `../scripts/update-proto.sh` fetches the latest definitions
2. **Make Targets**: 
   - `make update-proto` - Check and update proto definitions (includes code generation)
   - `make proto` - Generate Go code from existing proto files
3. **Dependency Management**: `buf dep update` ensures Google API dependencies are current

### CI/CD Integration

#### Scheduled Updates
A GitHub Actions workflow (`proto-update-check`) runs on a schedule to:

- **Check for Updates**: Compares local proto with official repository
- **Automatic PRs**: Creates pull requests when updates are available
- **Change Tracking**: Commits only include proto definition changes
- **Review Process**: Updates go through standard PR review workflow

### Manual Operations

#### Update Proto Definitions
```bash
# Check and update proto definitions from official A2A repository (includes code generation)
make update-proto

# Check if local definitions are current (CI mode)
./scripts/update-proto.sh --check

# Show metadata about current proto file
make proto-info
```

#### Generate Code
```bash
# Generate Go code from current proto files
make proto

# Clean and regenerate everything
make clean && make proto
```

## Dependencies

The proto definitions depend on:
- **Google APIs**: For HTTP annotations and field behavior
- **Protocol Buffers**: Core protobuf types (Struct, Timestamp, Empty)

Dependencies are managed through `buf.yaml` and automatically resolved via `buf dep update`.

## Proto File Metadata

Each fetched proto file includes automatically generated metadata headers:

```proto
// This file was automatically downloaded from the official A2A specification repository
// Source: https://github.com/a2aproject/A2A/blob/main/specification/grpc/a2a.proto
// Fetched: 2025-06-30T11:59:10Z
// Official commit: abc123def456...
```

This metadata provides:
- **Source Traceability**: Direct link to the canonical proto file
- **Fetch Timestamp**: When the file was last updated from the official source
- **Commit Hash**: Exact version from the official repository (when available)
- **Transparency**: Clear visibility into the proto file's provenance

Use `make proto-info` to quickly view the current metadata without opening the file.
