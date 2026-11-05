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

package a2asrv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

// get performs a GET against url, setting If-None-Match when non-empty, and
// returns the response. Callers close the body.
func get(t *testing.T, url, ifNoneMatch string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), "GET", url, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	return resp
}

func TestAgentCardHandler(t *testing.T) {
	t.Parallel()
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}

	slog.SetDefault(slog.New(slog.DiscardHandler))

	testCases := []struct {
		name        string
		method      string
		reqHeaders  map[string]string
		wantCard    bool
		wantHeaders map[string]string
		wantStatus  int
	}{
		{
			method:     "GET",
			wantCard:   true,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Access-Control-Allow-Origin": "*",
				"Content-Type":                "application/json",
				"Vary":                        "Origin",
			},
		},
		{
			method: "GET",
			reqHeaders: map[string]string{
				"Origin": "https://example.com",
			},
			wantCard:   true,
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Access-Control-Allow-Origin":      "https://example.com",
				"Access-Control-Allow-Credentials": "true",
				"Content-Type":                     "application/json",
				"Vary":                             "Origin",
			},
		},
		{
			method:     "OPTIONS",
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, OPTIONS",
				"Access-Control-Allow-Headers": "Content-Type",
				"Access-Control-Max-Age":       "86400",
				"Vary":                         "Origin",
			},
		},
		{
			method: "OPTIONS",
			reqHeaders: map[string]string{
				"Origin": "https://example.com",
			},
			wantStatus: http.StatusOK,
			wantHeaders: map[string]string{
				"Access-Control-Allow-Origin":      "https://example.com",
				"Access-Control-Allow-Credentials": "true",
				"Access-Control-Allow-Methods":     "GET, OPTIONS",
				"Access-Control-Allow-Headers":     "Content-Type",
				"Access-Control-Max-Age":           "86400",
				"Vary":                             "Origin",
			},
		},
		{
			method:     "POST",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			method:     "DELETE",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			method:     "PATCH",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			method:     "PUT",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	staticServer := httptest.NewServer(NewStaticAgentCardHandler(card))

	server := httptest.NewServer(NewAgentCardHandler(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
		return card, nil
	})))
	for _, tc := range testCases {
		for srvType, url := range map[string]string{"dynamic": server.URL, "static": staticServer.URL} {
			name := tc.method
			if len(tc.reqHeaders) > 0 {
				name = tc.method + " with origin"
			}
			t.Run(name+" "+srvType, func(t *testing.T) {
				t.Parallel()
				req, err := http.NewRequestWithContext(t.Context(), tc.method, url, nil)
				if err != nil {
					t.Errorf("http.NewRequestWithContext() error = %v", err)
					return
				}
				for k, v := range tc.reqHeaders {
					req.Header.Set(k, v)
				}
				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					t.Errorf("client.Do(req) error = %v", err)
					return
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != tc.wantStatus {
					t.Errorf("client.Do(req) status = %d, want %d", resp.StatusCode, tc.wantStatus)
				}
				for header, want := range tc.wantHeaders {
					if resp.Header.Get(header) != want {
						t.Errorf("resp.Header(%q) = %v, want %s", header, resp.Header.Get(header), want)
					}
				}
				if tc.wantCard {
					var gotCard a2a.AgentCard
					if err := json.NewDecoder(resp.Body).Decode(&gotCard); err != nil {
						t.Errorf("json.Decode() error = %v", err)
					}
					if diff := cmp.Diff(card, &gotCard); diff != "" {
						t.Errorf("wrong card (-want +got) diff = %s", diff)
					}
				}
			})
		}
	}
}

func TestAgentCardHandler_ServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(NewAgentCardHandler(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
		return nil, fmt.Errorf("failed")
	})))
	defer server.Close()
	resp := get(t, server.URL, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("GET status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestStaticAgentCardHandler_PanicWithMalformedCard(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected to panic for non-serializable card")
		}
	}()
	NewStaticAgentCardHandler(&a2a.AgentCard{
		Capabilities: a2a.AgentCapabilities{
			Extensions: []a2a.AgentExtension{{Params: map[string]any{"malformed": func() {}}}},
		},
	})
}

func TestAgentCardHandler_ETagAnd304(t *testing.T) {
	t.Parallel()

	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	servers := map[string]http.Handler{
		"static": NewStaticAgentCardHandler(card),
		"dynamic": NewAgentCardHandler(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
			return card, nil
		})),
	}
	for name, h := range servers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(h)
			defer srv.Close()

			resp := get(t, srv.URL, "")
			defer func() { _ = resp.Body.Close() }()
			etag := resp.Header.Get("ETag")
			if !strings.HasPrefix(etag, `W/"`) || !strings.HasSuffix(etag, `"`) {
				t.Fatalf("ETag = %q, want weak ETag format W/\"...\"", etag)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("first GET status = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			condResp := get(t, srv.URL, etag)
			defer func() { _ = condResp.Body.Close() }()
			if condResp.StatusCode != http.StatusNotModified {
				t.Errorf("conditional GET status = %d, want %d", condResp.StatusCode, http.StatusNotModified)
			}
			if got := condResp.Header.Get("ETag"); got != etag {
				t.Errorf("304 ETag = %q, want %q", got, etag)
			}
			body, err := io.ReadAll(condResp.Body)
			if err != nil {
				t.Fatalf("io.ReadAll() error = %v", err)
			}
			if len(body) != 0 {
				t.Errorf("304 body = %q, want empty", body)
			}
		})
	}
}

func TestAgentCardHandler_VaryPreservesMiddlewareValues(t *testing.T) {
	t.Parallel()

	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	inner := NewStaticAgentCardHandler(card)
	// Simulate outer middleware (e.g. gzhttp) contributing its own Vary
	// value before the card handler runs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL, "")
	defer func() { _ = resp.Body.Close() }()

	want := []string{"Accept-Encoding", "Origin"}
	if diff := cmp.Diff(want, resp.Header.Values("Vary")); diff != "" {
		t.Errorf("wrong Vary header values (-want +got) diff = %s", diff)
	}
}

func TestAgentCardHandler_IfNoneMatchForms(t *testing.T) {
	t.Parallel()
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	srv := httptest.NewServer(NewStaticAgentCardHandler(card))
	t.Cleanup(srv.Close)

	resp := get(t, srv.URL, "")
	defer func() { _ = resp.Body.Close() }()
	etag := resp.Header.Get("ETag")

	tests := []struct {
		name        string
		ifNoneMatch string
		wantStatus  int
	}{
		{name: "exact weak tag", ifNoneMatch: etag, wantStatus: http.StatusNotModified},
		{name: "strong form of weak tag", ifNoneMatch: strings.TrimPrefix(etag, "W/"), wantStatus: http.StatusNotModified},
		{name: "wildcard", ifNoneMatch: "*", wantStatus: http.StatusNotModified},
		{name: "list containing tag", ifNoneMatch: `"other", ` + etag, wantStatus: http.StatusNotModified},
		{name: "mismatched tag", ifNoneMatch: `W/"deadbeef"`, wantStatus: http.StatusOK},
		{name: "list without tag", ifNoneMatch: `"other", W/"deadbeef"`, wantStatus: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := get(t, srv.URL, tc.ifNoneMatch)
			defer func() { _ = got.Body.Close() }()
			if got.StatusCode != tc.wantStatus {
				t.Errorf("If-None-Match=%q status = %d, want %d", tc.ifNoneMatch, got.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestAgentCardHandler_CacheControl(t *testing.T) {
	t.Parallel()
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}

	t.Run("absent by default", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(NewStaticAgentCardHandler(card))
		defer srv.Close()
		resp := get(t, srv.URL, "")
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q, want empty by default", got)
		}
	})

	t.Run("passthrough when configured", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(NewStaticAgentCardHandler(card, WithAgentCardCacheControl("public, max-age=60")))
		defer srv.Close()
		resp := get(t, srv.URL, "")
		if got, want := resp.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Fatalf("Cache-Control = %q, want %q", got, want)
		}
		etag := resp.Header.Get("ETag")
		_ = resp.Body.Close()

		condResp := get(t, srv.URL, etag)
		defer func() { _ = condResp.Body.Close() }()
		if condResp.StatusCode != http.StatusNotModified {
			t.Fatalf("conditional status = %d, want %d", condResp.StatusCode, http.StatusNotModified)
		}
		if got, want := condResp.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Errorf("304 Cache-Control = %q, want %q", got, want)
		}
	})
}

// changingSignatureProducer mimics NewSignedCardProducer: identical card
// content, but a different signature on every request.
func changingSignatureProducer(counter func() int) AgentCardProducer {
	return AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
		return &a2a.AgentCard{
			Name:        "Agent",
			Description: "Test",
			Signatures:  []a2a.AgentCardSignature{{Protected: "p", Signature: fmt.Sprintf("sig-%d", counter())}},
		}, nil
	})
}

func TestAgentCardHandler_SignedCardStableETag(t *testing.T) {
	t.Parallel()
	n := 0
	srv := httptest.NewServer(NewAgentCardHandler(changingSignatureProducer(func() int {
		n++
		return n
	})))
	defer srv.Close()

	first := get(t, srv.URL, "")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.StatusCode)
	}
	etag1 := first.Header.Get("ETag")
	_ = first.Body.Close()

	cond := get(t, srv.URL, etag1)
	_ = cond.Body.Close()
	if cond.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304 despite re-signing", cond.StatusCode)
	}

	third := get(t, srv.URL, "")
	_ = third.Body.Close()
	if etag3 := third.Header.Get("ETag"); etag3 != etag1 {
		t.Errorf("ETag changed across re-signings: %q != %q", etag3, etag1)
	}
}

// rotatingKeyProducer mimics NewSignedCardProducer across a key rotation:
// identical card content, but the signature's protected header (and value)
// changes once the rotation happens.
func rotatingKeyProducer(protected func() string) AgentCardProducer {
	return AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
		return &a2a.AgentCard{
			Name:        "Agent",
			Description: "Test",
			Signatures:  []a2a.AgentCardSignature{{Protected: protected(), Signature: "sig"}},
		}, nil
	})
}

func TestAgentCardHandler_KeyRotationInvalidatesETag(t *testing.T) {
	t.Parallel()
	kid := "kid-old"
	srv := httptest.NewServer(NewAgentCardHandler(rotatingKeyProducer(func() string { return kid })))
	defer srv.Close()

	first := get(t, srv.URL, "")
	etag := first.Header.Get("ETag")
	_ = first.Body.Close()
	if first.StatusCode != http.StatusOK || etag == "" {
		t.Fatalf("first status = %d, ETag = %q; want 200 with an ETag", first.StatusCode, etag)
	}
	cond := get(t, srv.URL, etag)
	_ = cond.Body.Close()
	if cond.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status before rotation = %d, want 304", cond.StatusCode)
	}

	kid = "kid-new"
	rotated := get(t, srv.URL, etag)
	_ = rotated.Body.Close()
	if rotated.StatusCode != http.StatusOK {
		t.Errorf("conditional status after rotation = %d, want 200 so clients refetch the new signature", rotated.StatusCode)
	}
	if got := rotated.Header.Get("ETag"); got == etag {
		t.Errorf("ETag unchanged after key rotation: %q", got)
	}
}

// rawJSONProducer implements both AgentCardProducer and AgentCardJSONProducer,
// returning raw card JSON whose signatures member changes on every call.
type rawJSONProducer struct {
	n int
}

func (p *rawJSONProducer) Card(context.Context) (*a2a.AgentCard, error) {
	return nil, fmt.Errorf("not used: the handler must prefer CardJSON")
}

func (p *rawJSONProducer) CardJSON(context.Context) ([]byte, error) {
	p.n++
	return fmt.Appendf(nil,
		`{"name":"Agent","description":"Test","signatures":[{"protected":"p","signature":"sig-%d"}]}`, p.n), nil
}

func TestAgentCardHandler_RawJSONProducer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(NewAgentCardHandler(&rawJSONProducer{}))
	defer srv.Close()

	resp := get(t, srv.URL, "")
	etag := resp.Header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Fatalf("ETag = %q, want weak ETag", etag)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	_ = resp.Body.Close()

	condResp := get(t, srv.URL, etag)
	defer func() { _ = condResp.Body.Close() }()
	if condResp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304 despite changing raw signatures", condResp.StatusCode)
	}
}
