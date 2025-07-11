package jsonrpc

// Response represents a JSON-RPC response object.
// Either Result or Error MUST be included, but not both.
type Response struct {
	Message
	// Result is REQUIRED on success.
	// This member MUST NOT exist if there was an error invoking the method.
	// The value of this member is determined by the method invoked on the Server.
	// It's stored as an interface{} and often requires type assertion or
	// further unmarshalling based on the expected method result.
	Result interface{} `json:"result,omitempty"`
	// Error is REQUIRED on error.
	// This member MUST NOT exist if there was no error triggered during invocation.
	// The value for this member MUST be an Object as defined in section 5.1.
	Error *Error `json:"error,omitempty"`
}

// NewResponse creates a new JSON-RPC response with a result.
func NewResponse(id interface{}, result interface{}) *Response {
	return &Response{
		Message: Message{JSONRPC: Version, ID: id},
		Result:  result,
	}
}

// NewErrorResponse creates a new JSON-RPC response with an error.
func NewErrorResponse(id interface{}, err *Error) *Response {
	return &Response{
		Message: Message{JSONRPC: Version, ID: id},
		Error:   err,
	}
}

// NewNotificationResponse creates a JSON-RPC notification response with an optional ID.
// When ID is nil, it creates a proper notification (no ID field).
// When ID is provided, it creates a response that includes the ID (used for SSE events).
// This is useful for event streams like SSE where messages may need to be correlated
// with the original request.
func NewNotificationResponse(id interface{}, result interface{}) *Response {
	return &Response{
		Message: Message{JSONRPC: Version, ID: id},
		Result:  result,
	}
}
