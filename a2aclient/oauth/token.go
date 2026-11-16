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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	grantTypeAuthorizationCode = "authorization_code"
	grantTypeRefreshToken      = "refresh_token"
	grantTypeDeviceCode        = "urn:ietf:params:oauth:grant-type:device_code"

	maxResponseBytes = 1 << 20
)

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    flexInt `json:"expires_in"`
	RefreshToken string  `json:"refresh_token"`
	Scope        string  `json:"scope"`
}

// flexInt accepts a JSON number or a numeric string; some servers send expires_in as a string.
type flexInt int64

func (n *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid number %s", b)
	}
	*n = flexInt(v)
	return nil
}

// seconds returns n seconds as a duration, capped at [maxLifetime].
func (n flexInt) seconds() time.Duration {
	if n > flexInt(maxLifetime/time.Second) {
		return maxLifetime
	}
	return time.Duration(n) * time.Second
}

// postForm posts form values to an authorization server endpoint and decodes a
// JSON response into out. OAuth error responses are returned as *Error.
// Redirects are not followed: a redirected POST would resend the form, which may
// hold a refresh token, to wherever the redirect points.
func (f fetcher) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	if err := checkSecureURL(endpoint); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := *f.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "failed to close http response body", err)
		}
	}()
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		return fmt.Errorf("POST %s: HTTP %d redirect not followed", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("reading response from %s: %w", endpoint, err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("response from %s is larger than %d bytes", endpoint, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var oe Error
		if json.Unmarshal(body, &oe) == nil && oe.Code != "" {
			return &oe
		}
		return &statusError{method: http.MethodPost, url: endpoint, status: resp.StatusCode}
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("POST %s: empty response", endpoint)
	}
	var oe Error
	if json.Unmarshal(body, &oe) == nil && oe.Code != "" {
		return &oe
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parsing response from %s: %w", endpoint, err)
	}
	return nil
}

// token calls the token endpoint and validates the response.
func (f fetcher) token(ctx context.Context, endpoint string, form url.Values) (*tokenResponse, error) {
	var t tokenResponse
	if err := f.postForm(ctx, endpoint, form, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint %s returned no access_token", endpoint)
	}
	if t.TokenType != "" && !strings.EqualFold(t.TokenType, "bearer") {
		return nil, fmt.Errorf("token endpoint %s returned unsupported token type %q", endpoint, t.TokenType)
	}
	return &t, nil
}

func tokenForm(grantType, clientID, resource string) url.Values {
	form := url.Values{"grant_type": {grantType}, "client_id": {clientID}}
	if resource != "" {
		form.Set("resource", resource)
	}
	return form
}
