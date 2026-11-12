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

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// inlineHost is the host of the authorization server inline cards name. It is
// not a loopback host, so agents on other hosts may use it.
const inlineHost = "inline-as.example.com"

// testTransport hides the CloseIdleConnections method of a transport, and
// serves https://inlineHost from inlineHandler.
type testTransport struct{ base http.RoundTripper }

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != inlineHost {
		return t.base.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	inlineHandler.ServeHTTP(rec, r)
	resp := rec.Result()
	resp.Request = r
	return resp, nil
}

// TestMain keeps httptest.Server.Close, which closes the idle connections of
// http.DefaultTransport, from breaking requests that parallel tests are making
// over it at the same time.
func TestMain(m *testing.M) {
	http.DefaultTransport = testTransport{base: http.DefaultTransport}
	os.Exit(m.Run())
}
