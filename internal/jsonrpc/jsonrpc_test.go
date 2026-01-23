// Copyright 2025 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jsonrpc

import (
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
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

func TestToJSONRPCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want *Error
	}{
		{
			name: "JSONRPCError passthrough",
			err:  &Error{Code: -32001, Message: "Custom error", Data: map[string]any{"extra": "data"}},
			want: &Error{Code: -32001, Message: "Custom error", Data: map[string]any{"extra": "data"}},
		},
		{
			name: "Known a2a error",
			err:  a2a.ErrTaskNotFound,
			want: &Error{Code: -32001, Message: a2a.ErrTaskNotFound.Error(), Data: map[string]any{"error": a2a.ErrTaskNotFound.Error()}},
		},
		{
			name: "Known a2a error wrapped",
			err:  errors.Join(errors.New("context info"), a2a.ErrInvalidParams),
			want: &Error{Code: -32602, Message: a2a.ErrInvalidParams.Error(), Data: map[string]any{"error": "context info\ninvalid params"}},
		},
		{
			name: "Unknown error - internal error with details preserved",
			err:  errors.New("database connection failed"),
			want: &Error{Code: -32603, Message: a2a.ErrInternalError.Error(), Data: map[string]any{"error": "database connection failed"}},
		},
		{
			name: "Unknown error wrapped - internal error with details preserved",
			err:  errors.New("identity service error: user not authenticated"),
			want: &Error{Code: -32603, Message: a2a.ErrInternalError.Error(), Data: map[string]any{"error": "identity service error: user not authenticated"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ToJSONRPCError(tt.err)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ToJSONRPCError() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
