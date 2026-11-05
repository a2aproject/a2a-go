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

package utils

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMergeQueryParams(t *testing.T) {
	t.Parallel()

	dst := url.Values{"a": []string{"1"}}
	MergeQueryParams(dst, url.Values{"a": []string{"2"}, "b": []string{"3"}})
	MergeQueryParams(dst, nil)

	want := url.Values{"a": []string{"1", "2"}, "b": []string{"3"}}
	if diff := cmp.Diff(want, dst); diff != "" {
		t.Errorf("wrong merged params (-want +got) diff = %s", diff)
	}
}

func TestRedactURLError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "query stripped",
			err:  &url.Error{Op: "Post", URL: "https://agent.example.com/a2a?api_key=secret", Err: errors.New("dial failed")},
			want: "Post \"https://agent.example.com/a2a\": dial failed",
		},
		{
			name: "no query unchanged",
			err:  &url.Error{Op: "Post", URL: "https://agent.example.com/a2a", Err: errors.New("dial failed")},
			want: "Post \"https://agent.example.com/a2a\": dial failed",
		},
		{
			name: "other error unchanged",
			err:  errors.New("some other error"),
			want: "some other error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := RedactURLError(tc.err)
			if got.Error() != tc.want {
				t.Errorf("RedactURLError() = %q, want %q", got.Error(), tc.want)
			}
			// Wrapping a redacted *url.Error must keep it matchable with errors.As.
			var origURLErr *url.Error
			if errors.As(tc.err, &origURLErr) {
				wrapped := fmt.Errorf("failed to send HTTP request: %w", got)
				var urlErr *url.Error
				if !errors.As(wrapped, &urlErr) {
					t.Errorf("errors.As(wrapped, *url.Error) = false, want true")
				}
			}
		})
	}
}
