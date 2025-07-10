package jsonrpc

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONRPCRequest_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name       string
		input      Request
		expectJSON string // Expected JSON (might vary slightly based on map order)
	}{
		{
			name: "Request with structured params",
			input: Request{
				Message: Message{JSONRPC: "2.0", ID: "req-1"},
				Method:  "test/method",
				Params:  json.RawMessage(`{"key":"value","number":123}`),
			},
			expectJSON: `{"jsonrpc":"2.0","id":"req-1","method":"test/method","params":{"key":"value","number":123}}`,
		},
		{
			name: "Request with array params",
			input: Request{
				Message: Message{JSONRPC: "2.0", ID: float64(123)},
				Method:  "array/params",
				Params:  json.RawMessage(`[1, "two", null]`),
			},
			expectJSON: `{"jsonrpc":"2.0","id":123,"method":"array/params","params":[1,"two",null]}`,
		},
		{
			name: "Notification (no ID)",
			input: Request{
				Message: Message{JSONRPC: "2.0"}, // ID is nil
				Method:  "notify/update",
				Params:  json.RawMessage(`{}`),
			},
			expectJSON: `{"jsonrpc":"2.0","method":"notify/update","params":{}}`,
		},
		{
			name: "Request with no params",
			input: Request{
				Message: Message{JSONRPC: "2.0", ID: "req-noparams"},
				Method:  "get/status",
				// Params is nil
			},
			expectJSON: `{"jsonrpc":"2.0","id":"req-noparams","method":"get/status"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal
			jsonData, err := json.Marshal(tc.input)
			require.NoError(t, err, "Marshal should succeed")
			assert.JSONEq(t, tc.expectJSON, string(jsonData), "Marshalled JSON should match expected")

			// Unmarshal
			var output Request
			err = json.Unmarshal(jsonData, &output)
			require.NoError(t, err, "Unmarshal should succeed")

			// Assert specific fields.
			assert.Equal(t, tc.input.JSONRPC, output.JSONRPC)
			assert.Equal(t, tc.input.ID, output.ID)
			assert.Equal(t, tc.input.Method, output.Method)

			// Compare Params. Use JSONEq for non-nil, otherwise check for nil equality.
			if tc.input.Params == nil {
				assert.Nil(t, output.Params, "Unmarshalled Params should be nil")
			} else {
				assert.JSONEq(t, string(tc.input.Params), string(output.Params),
					"Unmarshalled Params should match original")
			}
		})
	}
}

func TestResponse_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name       string
		input      Response
		expectJSON string
	}{
		{
			name: "Success response with result",
			input: Response{
				Message: Message{JSONRPC: "2.0", ID: "resp-1"},
				Result:  json.RawMessage(`{"status":"ok","value":true}`),
			},
			expectJSON: `{"jsonrpc":"2.0","id":"resp-1","result":{"status":"ok","value":true}}`,
		},
		{
			name: "Error response",
			input: Response{
				Message: Message{JSONRPC: "2.0", ID: float64(99)},
				Error:   ErrInvalidParams("Missing required field 'name'"),
			},
			expectJSON: `{"jsonrpc":
			"2.0","id":99,"error":{"code":-32602,"message":"Invalid params","data":"Missing required field 'name'"}}`,
		},
		{
			name: "Error response with nil ID (parse error case)",
			input: Response{
				Message: Message{JSONRPC: "2.0", ID: nil},
				Error:   ErrParseError("Unexpected token '!'"),
			},
			expectJSON: `{"jsonrpc":
			"2.0","error":{"code":-32700,"message":"Parse error","data":"Unexpected token '!'"}}`,
		},
		{
			name: "Success response with null result",
			input: Response{
				Message: Message{JSONRPC: "2.0", ID: "resp-null"},
				Result:  json.RawMessage(`null`),
			},
			expectJSON: `{"jsonrpc":"2.0","id":"resp-null","result":null}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal
			jsonData, err := json.Marshal(tc.input)
			require.NoError(t, err, "Marshal should succeed")
			assert.JSONEq(t, tc.expectJSON, string(jsonData), "Marshalled JSON should match expected")

			// Unmarshal
			var output Response
			err = json.Unmarshal(jsonData, &output)
			require.NoError(t, err, "Unmarshal should succeed")

			// Assert specific fields.
			assert.Equal(t, tc.input.JSONRPC, output.JSONRPC)
			assert.Equal(t, tc.input.ID, output.ID)
			if tc.input.Error != nil {
				// Error case: Expect Error, No Result
				require.NotNil(t, output.Error, "Unmarshal(error response): Error should not be nil")
				assert.Equal(t, *tc.input.Error, *output.Error, "Unmarshal(error response): Error mismatch")
				assert.Nil(t, output.Result, "Unmarshal(error response): Result should be nil")
			} else {
				// Success case: Expect Result, No Error
				require.Nil(t, output.Error, "Unmarshal(success response): Error should be nil")
				// Compare Result by unmarshalling expected/actual into interface{} and comparing.
				var inputResult, outputResult interface{}
				// Unmarshal the expected raw JSON Result first
				if inputResultBytes, ok := tc.input.Result.(json.RawMessage); ok && len(inputResultBytes) > 0 {
					err = json.Unmarshal(inputResultBytes, &inputResult)
					require.NoError(t, err, "Failed to unmarshal input result for comparison")
				}
				// output.Result is already interface{} after unmarshalling the whole response
				outputResult = output.Result
				assert.Equal(t, inputResult, outputResult, "Unmarshal(success response): Result mismatch")
			}
		})
	}
}

func TestErrorCreation(t *testing.T) {
	tests := []struct {
		name       string
		createFunc func(data interface{}) *Error
		expectCode int
		expectMsg  string // Use expected message string directly
		data       interface{}
	}{
		{"ParseError", ErrParseError, CodeParseError,
			"Parse error", "Invalid character"},
		{"InvalidRequest", ErrInvalidRequest, CodeInvalidRequest,
			"Invalid Request", "Missing jsonrpc field"},
		{"MethodNotFound", ErrMethodNotFound, CodeMethodNotFound,
			"Method not found", "method xyz"},
		{"InvalidParams", ErrInvalidParams, CodeInvalidParams,
			"Invalid params", "Expected string, got number"},
		{"InternalError", ErrInternalError, CodeInternalError,
			"Internal error", "Database connection failed"},
		// Test custom error outside standard range.
		{"CustomError", func(d interface{}) *Error {
			return &Error{Code: -31000, Message: "CustomAppError", Data: d}
		}, -31000, "CustomAppError", map[string]int{"detailCode": 101}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.createFunc(tc.data)
			require.NotNil(t, err)
			assert.Equal(t, tc.expectCode, err.Code)
			assert.Equal(t, tc.expectMsg, err.Message)
			assert.Equal(t, tc.data, err.Data)

			// Check Error() method
			expectedErrorString := fmt.Sprintf("jsonrpc error %d: %s", tc.expectCode, tc.expectMsg)
			assert.Equal(t, expectedErrorString, err.Error())
		})
	}
}

func TestNewRequest(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		id         string
		expectJSON string
	}{
		{
			name:       "String ID",
			method:     "test/method",
			id:         "req-123",
			expectJSON: `{"jsonrpc":"2.0","id":"req-123","method":"test/method"}`,
		},
		{
			name:       "Integer ID",
			method:     "get/data",
			id:         "123",
			expectJSON: `{"jsonrpc":"2.0","id":"123","method":"get/data"}`,
		},
		{
			name:       "Auto-generated ID",
			method:     "notify/update",
			id:         "123",
			expectJSON: ``, // Will be checked separately since ID is auto-generated
		},
		{
			name:       "Complex Method Path",
			method:     "namespace/resource/action",
			id:         "complex-id",
			expectJSON: `{"jsonrpc":"2.0","id":"complex-id","method":"namespace/resource/action"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := NewRequest(tc.method, tc.id)

			// Verify individual fields
			assert.Equal(t, Version, req.JSONRPC, "JSONRPC version should be set correctly")
			assert.Equal(t, tc.method, req.Method, "Method should match input")

			// Special handling for auto-generated ID test case
			if tc.name == "Auto-generated ID" {
				assert.NotNil(t, req.ID, "ID should be auto-generated when nil is passed")
				assert.NotEmpty(t, req.ID, "Auto-generated ID should not be empty")
				// Skip JSON comparison for auto-generated ID since it's unpredictable
				return
			}

			assert.Equal(t, tc.id, req.ID, "ID should match input")
			assert.Nil(t, req.Params, "Params should be nil by default")

			// Verify JSON representation
			jsonData, err := json.Marshal(req)
			require.NoError(t, err, "Marshal should succeed")
			assert.JSONEq(t, tc.expectJSON, string(jsonData), "JSON should match expected")
		})
	}
}

func TestNewResponse(t *testing.T) {
	tests := []struct {
		name       string
		id         interface{}
		result     interface{}
		expectJSON string
	}{
		{
			name:       "String result",
			id:         "resp-1",
			result:     "success",
			expectJSON: `{"jsonrpc":"2.0","id":"resp-1","result":"success"}`,
		},
		{
			name:       "Integer result",
			id:         123,
			result:     42,
			expectJSON: `{"jsonrpc":"2.0","id":123,"result":42}`,
		},
		{
			name:       "Boolean result",
			id:         "bool-resp",
			result:     true,
			expectJSON: `{"jsonrpc":"2.0","id":"bool-resp","result":true}`,
		},
		{
			name:       "Null result",
			id:         "null-resp",
			result:     nil,
			expectJSON: `{"jsonrpc":"2.0","id":"null-resp"}`,
		},
		{
			name:       "Object result",
			id:         "obj-resp",
			result:     map[string]interface{}{"status": "ok", "count": 5},
			expectJSON: `{"jsonrpc":"2.0","id":"obj-resp","result":{"status":"ok","count":5}}`,
		},
		{
			name:       "Array result",
			id:         "arr-resp",
			result:     []interface{}{1, "two", true},
			expectJSON: `{"jsonrpc":"2.0","id":"arr-resp","result":[1,"two",true]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewResponse(tc.id, tc.result)

			// Verify individual fields
			assert.Equal(t, Version, resp.JSONRPC, "JSONRPC version should be set correctly")
			assert.Equal(t, tc.id, resp.ID, "ID should match input")
			assert.Equal(t, tc.result, resp.Result, "Result should match input")
			assert.Nil(t, resp.Error, "Error should be nil for success response")

			// Verify JSON representation
			jsonData, err := json.Marshal(resp)
			require.NoError(t, err, "Marshal should succeed")
			assert.JSONEq(t, tc.expectJSON, string(jsonData), "JSON should match expected")
		})
	}
}

func TestNewErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		id         interface{}
		err        *Error
		expectJSON string
	}{
		{
			name: "Parse error",
			id:   nil,
			err:  ErrParseError("Invalid JSON"),
			expectJSON: `{"jsonrpc":"2.0","error":
			{"code":-32700,"message":"Parse error","data":"Invalid JSON"}}`,
		},
		{
			name: "Method not found",
			id:   "err-1",
			err:  ErrMethodNotFound("Method 'test' not found"),
			expectJSON: `{"jsonrpc":"2.0","id":"err-1","error":
			{"code":-32601,"message":"Method not found","data":"Method 'test' not found"}}`,
		},
		{
			name: "Invalid params",
			id:   42,
			err:  ErrInvalidParams("Missing required parameter 'name'"),
			expectJSON: `{"jsonrpc":"2.0","id":42,"error":
			{"code":-32602,"message":"Invalid params","data":"Missing required parameter 'name'"}}`,
		},
		{
			name: "Internal error",
			id:   "internal-err",
			err:  ErrInternalError("Database connection failed"),
			expectJSON: `{"jsonrpc":"2.0","id":"internal-err","error":
			{"code":-32603,"message":"Internal error","data":"Database connection failed"}}`,
		},
		{
			name: "Custom error",
			id:   "custom-err",
			err: &Error{Code: -10001, Message: "Custom error",
				Data: map[string]interface{}{"details": "Something went wrong"}},
			expectJSON: `{"jsonrpc":"2.0","id":"custom-err","error":
			{"code":-10001,"message":"Custom error","data":{"details":"Something went wrong"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewErrorResponse(tc.id, tc.err)

			// Verify individual fields
			assert.Equal(t, Version, resp.JSONRPC, "JSONRPC version should be set correctly")
			assert.Equal(t, tc.id, resp.ID, "ID should match input")
			assert.Nil(t, resp.Result, "Result should be nil for error response")
			assert.Equal(t, tc.err, resp.Error, "Error should match input")

			// Verify JSON representation
			jsonData, err := json.Marshal(resp)
			require.NoError(t, err, "Marshal should succeed")
			assert.JSONEq(t, tc.expectJSON, string(jsonData), "JSON should match expected")
		})
	}
}

func TestErrorNilCase(t *testing.T) {
	// Test handling of nil Error when calling Error() method
	var err *Error
	assert.Equal(t, "<nil jsonrpc error>", err.Error(), "Nil error should have correct string representation")
}

func TestNewNotificationResponse(t *testing.T) {
	testCases := []struct {
		name  string
		id    interface{}
		data  interface{}
		hasID bool
	}{
		{
			name:  "Notification without ID",
			id:    nil,
			data:  map[string]string{"key": "value"},
			hasID: false,
		},
		{
			name:  "Response with string ID",
			id:    "test-id-123",
			data:  map[string]string{"key": "value"},
			hasID: true,
		},
		{
			name:  "Response with numeric ID",
			id:    123,
			data:  map[string]string{"key": "value"},
			hasID: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response := NewNotificationResponse(tc.id, tc.data)

			// Verify JSONRPC version
			assert.Equal(t, Version, response.JSONRPC, "JSONRPC version should be set correctly")

			// Verify ID handling
			if tc.hasID {
				assert.Equal(t, tc.id, response.ID, "ID should match the provided value")
			} else {
				assert.Nil(t, response.ID, "ID should be nil for notifications")
			}

			// Verify the result is the data directly
			assert.Equal(t, tc.data, response.Result, "Result should be the provided data directly")
		})
	}
}
