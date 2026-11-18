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

package a2asrv

import (
	"net/http"
	"os"
	"testing"
)

// hiddenCloseIdleTransport hides the CloseIdleConnections method of a transport.
type hiddenCloseIdleTransport struct{ base http.RoundTripper }

func (t hiddenCloseIdleTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(r)
}

// TestMain keeps httptest.Server.Close, which closes the idle connections of
// http.DefaultTransport, from breaking requests that parallel tests are making
// over it at the same time, like push notifications of push.HTTPPushSender
// with AllowPrivateNetworks.
func TestMain(m *testing.M) {
	http.DefaultTransport = hiddenCloseIdleTransport{base: http.DefaultTransport}
	os.Exit(m.Run())
}
