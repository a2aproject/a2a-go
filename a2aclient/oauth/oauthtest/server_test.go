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

package oauthtest_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2aclient/oauth/oauthtest"
)

func postForm(t *testing.T, client *http.Client, u string, form url.Values) map[string]any {
	t.Helper()
	resp, err := client.PostForm(u, form)
	if err != nil {
		t.Fatalf("POST %s error = %v", u, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("closing the response of %s error = %v", u, err)
		}
	}()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding the response of %s error = %v", u, err)
	}
	return out
}

func TestDeviceGrant(t *testing.T) {
	t.Parallel()
	const resource = "https://agent.example.com"
	tests := []struct {
		name      string
		tls       bool
		resource  string
		wantValid bool
	}{
		{name: "http", resource: resource, wantValid: true},
		{name: "https", tls: true, resource: resource, wantValid: true},
		{name: "no resource indicator", resource: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			as := oauthtest.NewAuthServer(t, oauthtest.ServerOptions{TLS: tc.tls})
			client := http.DefaultClient
			if tc.tls {
				pool := x509.NewCertPool()
				pool.AddCert(as.Certificate())
				client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
			}
			form := url.Values{"client_id": {"c"}}
			if tc.resource != "" {
				form.Set("resource", tc.resource)
			}
			da := postForm(t, client, as.URL+"/device", form)
			uri, _ := da["verification_uri_complete"].(string)
			oauthtest.Browser(t)(uri)
			tok := postForm(t, client, as.URL+"/token", url.Values{
				"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
				"device_code": {fmt.Sprint(da["device_code"])},
				"client_id":   {"c"},
			})
			access, _ := tok["access_token"].(string)
			if access == "" {
				t.Fatalf("token response = %v, want an access token", tok)
			}
			if got := as.Valid(access, resource, nil); got != tc.wantValid {
				t.Fatalf("AuthServer.Valid() = %v, want %v", got, tc.wantValid)
			}
		})
	}
}
