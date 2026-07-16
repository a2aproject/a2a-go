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

package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/log"
)

// maxPushRedirects bounds the redirect chain a push notification may follow.
const maxPushRedirects = 10

// errBlockedPushTarget is returned when a push notification URL resolves to a
// non-public address range (SSRF protection, CWE-918).
var errBlockedPushTarget = errors.New("push notification target resolves to a blocked address range")

var tokenHeader = http.CanonicalHeaderKey("A2A-Notification-Token")

// HTTPPushSender sends A2A events to a push notification endpoint over HTTP.
type HTTPPushSender struct {
	client      *http.Client
	failOnError bool
}

// HTTPSenderConfig allows to configure [HTTPPushSender].
type HTTPSenderConfig struct {
	// Timeout is used to configure internal [http.Client].
	Timeout time.Duration
	// FailOnError can be set to true to make push sending errors trigger execution cancelation.
	FailOnError bool
	// AllowPrivateNetworks disables the built-in SSRF guard that rejects
	// webhook URLs resolving to loopback, private, link-local or unspecified
	// addresses. It defaults to false (guard enabled) per the A2A spec 13.2
	// requirement that agents validate webhook URLs against SSRF. Set it to
	// true only for trusted internal deployments (for example a webhook on the
	// same host or private network).
	AllowPrivateNetworks bool
}

// NewHTTPPushSender creates a new HTTPPushSender. It uses a default client
// with a 30-second timeout. An optional config can be provided to customize it.
//
// By default the client is SSRF-hardened: it validates the resolved IP of
// every connection (including redirect hops) and refuses to POST to loopback,
// private, link-local or unspecified addresses. Set
// [HTTPSenderConfig.AllowPrivateNetworks] to opt out for trusted internal use.
func NewHTTPPushSender(config *HTTPSenderConfig) *HTTPPushSender {
	t := 30 * time.Second
	allowPrivate := false
	if config != nil {
		if config.Timeout > 0 {
			t = config.Timeout
		}
		allowPrivate = config.AllowPrivateNetworks
	}
	client := &http.Client{Timeout: t}
	if !allowPrivate {
		client.Transport = ssrfGuardedTransport()
		client.CheckRedirect = limitPushRedirects
	}
	return &HTTPPushSender{
		client:      client,
		failOnError: config != nil && config.FailOnError,
	}
}

// ssrfGuardedTransport clones the default transport and validates the resolved
// IP of every connection before the TCP handshake. Because the check runs at
// dial time it also covers DNS-rebinding targets and redirect hops (each hop
// re-dials), which a URL-string check alone cannot catch.
func ssrfGuardedTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("invalid dial address %q: %w", address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%w: unresolved host %q", errBlockedPushTarget, host)
		}
		if isBlockedIP(ip) {
			return fmt.Errorf("%w: %s", errBlockedPushTarget, ip)
		}
		return nil
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	return tr
}

// isBlockedIP reports whether ip is in a range a push notification must not
// reach: loopback, RFC 1918 / RFC 4193 private, link-local, unspecified or
// multicast. This covers cloud metadata endpoints (169.254.169.254) and
// internal services (127.0.0.1, 10/8, 172.16/12, 192.168/16).
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// limitPushRedirects bounds the redirect chain; the per-dial IP guard already
// validates the resolved address of every hop.
func limitPushRedirects(_ *http.Request, via []*http.Request) error {
	if len(via) >= maxPushRedirects {
		return fmt.Errorf("stopped after %d redirects", maxPushRedirects)
	}
	return nil
}

// SendPush serializes the task to JSON and sends it as an HTTP POST request
// to the URL specified in the push configuration.
func (s *HTTPPushSender) SendPush(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	jsonData, err := json.Marshal(a2a.StreamResponse{Event: event})
	if err != nil {
		return s.handleError(ctx, fmt.Errorf("failed to serialize event to JSON: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return s.handleError(ctx, fmt.Errorf("failed to create HTTP request: %w", err))
	}

	req.Header.Set("Content-Type", "application/json")
	if config.Token != "" {
		req.Header.Set(tokenHeader, config.Token)
	}
	if config.Auth != nil && config.Auth.Credentials != "" {
		switch strings.ToLower(config.Auth.Scheme) {
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+config.Auth.Credentials)
		case "basic":
			req.Header.Set("Authorization", "Basic "+config.Auth.Credentials)
		}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return s.handleError(ctx, fmt.Errorf("failed to send push notification: %w", err))
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error(ctx, "push response body close failed", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.handleError(ctx, fmt.Errorf("push notification endpoint returned non-success status: %s", resp.Status))
	}

	return nil
}

func (s *HTTPPushSender) handleError(ctx context.Context, err error) error {
	if s.failOnError {
		return err
	}
	log.Error(ctx, "push sending failed", err)
	return nil
}
