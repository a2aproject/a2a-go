// Copyright 2026 The A2A Authors
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

package oauth

import "testing"

func TestError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{name: "code", err: &Error{Code: "invalid_grant"}, want: "invalid_grant"},
		{name: "code and description", err: &Error{Code: "invalid_grant", Description: "expired"}, want: "invalid_grant: expired"},
		{name: "zero value", err: &Error{}, want: "OAuth error response without an error code"},
		{name: "description only", err: &Error{Description: "oops"}, want: "OAuth error response without an error code: oops"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error.Error() = %q, want %q", got, tc.want)
			}
		})
	}
}
