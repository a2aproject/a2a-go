package jsonrpc

import (
	"encoding/json"

	"github.com/a2aproject/a2a-go/protocol/jsonprotocol"
)

// Request represents a JSON-RPC request object.
type Request struct {
	Message
	// Method is a String containing the name of the method to be invoked.
	Method string `json:"method"`
	// Params is a Structured value that holds the parameter values to be used
	// during the invocation of the method. This member MAY be omitted.
	// It's stored as raw JSON to be decoded by the method handler.
	Params json.RawMessage `json:"params,omitempty"`
}

// NewRequest creates a new JSON-RPC request with the given method and ID.
// If id is nil, a new ID will be automatically generated since A2A protocol
// requires responses for all requests.
// Note: Currently all callers pass non-nil IDs, so auto-generation is not used in practice.
func NewRequest(method string, id string) *Request {
	if id == "" {
		id = jsonprotocol.GenerateRPCID()
	}
	return &Request{
		Message: Message{
			ID:      id,
			JSONRPC: Version,
		},
		Method: method,
	}
}
