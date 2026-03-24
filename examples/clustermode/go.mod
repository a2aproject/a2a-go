module github.com/a2aproject/a2a-go/v2/examples/clustermode

go 1.24.4

require (
	github.com/a2aproject/a2a-go/v2 v2.0.1
	github.com/go-sql-driver/mysql v1.9.3
	github.com/google/uuid v1.6.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

replace github.com/a2aproject/a2a-go/v2 => ../../
