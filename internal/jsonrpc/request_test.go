package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateNewRequest(t *testing.T) {
	// Test creating a new request
	req := NewRequest("test-method", "test-id")

	assert.Equal(t, Version, req.JSONRPC)
	assert.Equal(t, "test-id", req.ID)
	assert.Equal(t, "test-method", req.Method)
	assert.Nil(t, req.Params)
}

func TestRequestMarshalJSON(t *testing.T) {
	// Test with all fields populated
	params := json.RawMessage(`{"key":"value"}`)
	req := &Request{
		Message: Message{
			JSONRPC: Version,
			ID:      "test-id",
		},
		Method: "test-method",
		Params: params,
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var unmarshaled map[string]interface{}
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, Version, unmarshaled["jsonrpc"])
	assert.Equal(t, "test-id", unmarshaled["id"])
	assert.Equal(t, "test-method", unmarshaled["method"])

	paramsMap, ok := unmarshaled["params"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", paramsMap["key"])

	// Test with empty params
	req = &Request{
		Message: Message{
			JSONRPC: Version,
			ID:      "test-id",
		},
		Method: "test-method",
	}

	data, err = json.Marshal(req)
	require.NoError(t, err)

	unmarshaled = make(map[string]interface{})
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, Version, unmarshaled["jsonrpc"])
	assert.Equal(t, "test-id", unmarshaled["id"])
	assert.Equal(t, "test-method", unmarshaled["method"])

	// Check that params field is not present in the JSON output
	_, exists := unmarshaled["params"]
	assert.False(t, exists, "params field should not be present when empty")
}

func TestRequestUnmarshalJSON(t *testing.T) {
	// Test valid JSON-RPC request
	jsonData := `{
		"jsonrpc": "2.0",
		"id": "request-id",
		"method": "test.method",
		"params": {"param1": "value1", "param2": 42}
	}`

	var req Request
	err := json.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	assert.Equal(t, Version, req.JSONRPC)
	assert.Equal(t, "request-id", req.ID)
	assert.Equal(t, "test.method", req.Method)

	// Verify params
	var params map[string]interface{}
	err = json.Unmarshal(req.Params, &params)
	require.NoError(t, err)
	assert.Equal(t, "value1", params["param1"])
	assert.Equal(t, float64(42), params["param2"])

	// Test without params
	jsonData = `{
		"jsonrpc": "2.0",
		"id": "request-id",
		"method": "test.method"
	}`

	req = Request{}
	err = json.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	assert.Equal(t, Version, req.JSONRPC)
	assert.Equal(t, "request-id", req.ID)
	assert.Equal(t, "test.method", req.Method)
	assert.Len(t, req.Params, 0, "params should be empty")

	// Test with missing jsonrpc version
	jsonData = `{
		"id": "request-id",
		"method": "test.method"
	}`

	req = Request{}
	err = json.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err) // Will not fail because jsonrpc is not validated in unmarshal

	// Test with missing required method field
	jsonData = `{
		"jsonrpc": "2.0",
		"id": "request-id"
	}`

	req = Request{}
	err = json.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err) // Will not fail because method is not validated in unmarshal

	// Test with invalid JSON
	jsonData = `{
		"jsonrpc": "2.0",
		"id": "request-id",
		"method": "test.method",
		"params": {"invalid": 
	}`

	req = Request{}
	err = json.Unmarshal([]byte(jsonData), &req)
	assert.Error(t, err)
}
