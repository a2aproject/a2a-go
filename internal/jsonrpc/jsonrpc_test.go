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
