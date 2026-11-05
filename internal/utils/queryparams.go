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
	"net/url"
	"strings"
)

// MergeQueryParams appends all key-value pairs from src to dst.
func MergeQueryParams(dst, src url.Values) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// RedactURLError strips the query part from [*url.Error] values so that
// credentials carried in URL query parameters do not leak into error
// messages and logs. Errors of other types are returned unchanged.
func RedactURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	i := strings.Index(urlErr.URL, "?")
	if i < 0 {
		return err
	}
	redacted := *urlErr
	redacted.URL = urlErr.URL[:i]
	return &redacted
}
