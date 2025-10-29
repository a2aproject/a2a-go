package jsonrpc

import (
	"testing"
)

func TestJSONRPCError(t *testing.T) {
	err := &Error{
		Code:    -32600,
		Message: "Invalid Request",
		Data:    map[string]any{"details": "extra info"},
	}

	errStr := err.Error()
	if errStr != "jsonrpc error -32600: Invalid Request (data: map[details:extra info])" {
		t.Errorf("Unexpected error string: %s", errStr)
	}

	err2 := &Error{Code: -32601, Message: "Method not found"}

	errStr2 := err2.Error()
	if errStr2 != "jsonrpc error -32601: Method not found" {
		t.Errorf("Unexpected error string: %s", errStr2)
	}
}
