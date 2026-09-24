module github.com/a2aproject/a2a-go/itk

go 1.25.0

require (
	github.com/a2aproject/a2a-go v0.3.15
	github.com/a2aproject/a2a-go/v2 v2.0.0
	golang.org/x/sync v0.22.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/a2aproject/a2a-go/v2 => ../
