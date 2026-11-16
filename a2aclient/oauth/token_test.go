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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestToken(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		requests <- r
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_in":"3600","refresh_token":"rt"}`))
	}))
	t.Cleanup(srv.Close)

	form := tokenForm(grantTypeAuthorizationCode, "cli", "https://agent.example.com")
	form.Set("code", "c0de")
	got, err := testFetcher().token(t.Context(), srv.URL, form)
	if err != nil {
		t.Fatalf("token() error = %v", err)
	}
	want := &tokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "rt"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("token() wrong result (-want +got) diff = %s", diff)
	}
	r := <-requests
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want a form", ct)
	}
	wantForm := url.Values{
		"grant_type": {"authorization_code"},
		"client_id":  {"cli"},
		"resource":   {"https://agent.example.com"},
		"code":       {"c0de"},
	}
	if diff := cmp.Diff(wantForm, r.PostForm); diff != "" {
		t.Fatalf("token request form wrong (-want +got) diff = %s", diff)
	}
}

func TestTokenErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		body      string
		wantErr   string
		wantOAuth string
	}{
		{name: "oauth error", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"expired"}`, wantErr: "invalid_grant: expired", wantOAuth: "invalid_grant"},
		{name: "oauth error with status 200", status: http.StatusOK, body: `{"error":"authorization_pending"}`, wantErr: "authorization_pending", wantOAuth: "authorization_pending"},
		{name: "server error", status: http.StatusInternalServerError, body: `oops`, wantErr: "HTTP 500"},
		{name: "empty response", status: http.StatusOK, wantErr: "empty response"},
		{name: "no access token", status: http.StatusOK, body: `{"token_type":"Bearer"}`, wantErr: "no access_token"},
		{name: "unsupported token type", status: http.StatusOK, body: `{"access_token":"at","token_type":"mac"}`, wantErr: `unsupported token type "mac"`},
		{name: "bad expires_in", status: http.StatusOK, body: `{"access_token":"at","expires_in":"soon"}`, wantErr: "parsing response"},
		{
			name:    "response too large",
			status:  http.StatusOK,
			body:    `{"access_token":"at","id_token":"` + strings.Repeat("x", maxResponseBytes) + `"}`,
			wantErr: "is larger than 1048576 bytes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			_, err := testFetcher().token(t.Context(), srv.URL, tokenForm(grantTypeRefreshToken, "cli", ""))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("token() error = %v, want it to contain %q", err, tc.wantErr)
			}
			if got := isOAuthError(err, tc.wantOAuth); got != (tc.wantOAuth != "") {
				t.Fatalf("isOAuthError(%v, %q) = %v", err, tc.wantOAuth, got)
			}
		})
	}
}

func TestPostFormWithoutResult(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	if err := testFetcher().postForm(t.Context(), srv.URL, url.Values{"token": {"t"}}, nil); err != nil {
		t.Fatalf("postForm() with an empty 200 response error = %v, want nil", err)
	}
	if err := testFetcher().postForm(t.Context(), "http://as.example.com/revoke", nil, nil); err == nil {
		t.Fatal("postForm() to a non-loopback http URL error = nil, want it refused")
	}
}

func TestTokenForm(t *testing.T) {
	t.Parallel()
	got := tokenForm(grantTypeDeviceCode, "cli", "")
	want := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {"cli"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("tokenForm() without a resource wrong result (-want +got) diff = %s", diff)
	}
}

func TestFlexInt(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]flexInt{`3600`: 3600, `"3600"`: 3600, `null`: 0, `""`: 0} {
		var got flexInt
		if err := json.Unmarshal([]byte(in), &got); err != nil || got != want {
			t.Errorf("Unmarshal(%s) = %d, %v, want %d", in, got, err, want)
		}
	}
	var n flexInt
	if err := json.Unmarshal([]byte(`"1h"`), &n); err == nil {
		t.Error(`Unmarshal("1h") error = nil, want an error`)
	}
}
