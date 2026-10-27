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

func TestAgentCardHandler(t *testing.T) {
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
	server := httptest.NewServer(NewAgentCardHandler(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
		return nil, fmt.Errorf("failed")
	})))
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("http.Get() status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestStaticAgentCardHandler_PanicWithMalformedCard(t *testing.T) {
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
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	servers := map[string]http.Handler{
		"static": NewStaticAgentCardHandler(card),
		"dynamic": NewAgentCardHandler(AgentCardProducerFn(func(context.Context) (*a2a.AgentCard, error) {
			return card, nil
		})),
	}
	for name, h := range servers {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			client := &http.Client{}

			req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
			if err != nil {
				t.Fatalf("http.NewRequestWithContext() error = %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("client.Do() error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			etag := resp.Header.Get("ETag")
			if !strings.HasPrefix(etag, `W/"`) || !strings.HasSuffix(etag, `"`) {
				t.Fatalf("ETag = %q, want weak ETag format W/\"...\"", etag)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("first GET status = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			condReq, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
			if err != nil {
				t.Fatalf("http.NewRequestWithContext() error = %v", err)
			}
			condReq.Header.Set("If-None-Match", etag)
			condResp, err := client.Do(condReq)
			if err != nil {
				t.Fatalf("client.Do() conditional error = %v", err)
			}
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

func TestAgentCardHandler_IfNoneMatchForms(t *testing.T) {
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	srv := httptest.NewServer(NewStaticAgentCardHandler(card))
	defer srv.Close()
	client := &http.Client{}

	req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
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
			r, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
			if err != nil {
				t.Fatalf("http.NewRequestWithContext() error = %v", err)
			}
			r.Header.Set("If-None-Match", tc.ifNoneMatch)
			got, err := client.Do(r)
			if err != nil {
				t.Fatalf("client.Do() error = %v", err)
			}
			defer func() { _ = got.Body.Close() }()
			if got.StatusCode != tc.wantStatus {
				t.Errorf("If-None-Match=%q status = %d, want %d", tc.ifNoneMatch, got.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestAgentCardHandler_CacheControl(t *testing.T) {
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}
	client := &http.Client{}

	t.Run("absent by default", func(t *testing.T) {
		srv := httptest.NewServer(NewStaticAgentCardHandler(card))
		defer srv.Close()
		req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext() error = %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do() error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get("Cache-Control"); got != "" {
			t.Errorf("Cache-Control = %q, want empty by default", got)
		}
	})

	t.Run("passthrough when configured", func(t *testing.T) {
		srv := httptest.NewServer(NewStaticAgentCardHandler(card, WithCacheControl("public, max-age=60")))
		defer srv.Close()
		req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext() error = %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do() error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if got, want := resp.Header.Get("Cache-Control"), "public, max-age=60"; got != want {
			t.Fatalf("Cache-Control = %q, want %q", got, want)
		}

		condReq, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext() error = %v", err)
		}
		condReq.Header.Set("If-None-Match", resp.Header.Get("ETag"))
		condResp, err := client.Do(condReq)
		if err != nil {
			t.Fatalf("client.Do() error = %v", err)
		}
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
	n := 0
	srv := httptest.NewServer(NewAgentCardHandler(changingSignatureProducer(func() int {
		n++
		return n
	})))
	defer srv.Close()
	client := &http.Client{}

	get := func(ifNoneMatch string) (*http.Response, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
		if err != nil {
			t.Fatalf("http.NewRequestWithContext() error = %v", err)
		}
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do() error = %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp, resp.Header.Get("ETag")
	}

	first, etag1 := get("")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.StatusCode)
	}
	cond, _ := get(etag1)
	if cond.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304 despite re-signing", cond.StatusCode)
	}
	_, etag3 := get("")
	if etag3 != etag1 {
		t.Errorf("ETag changed across re-signings: %q != %q", etag3, etag1)
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
	srv := httptest.NewServer(NewAgentCardHandler(&rawJSONProducer{}))
	defer srv.Close()
	client := &http.Client{}

	req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	etag := resp.Header.Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) {
		t.Fatalf("ETag = %q, want weak ETag", etag)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	condReq, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	condReq.Header.Set("If-None-Match", etag)
	condResp, err := client.Do(condReq)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer func() { _ = condResp.Body.Close() }()
	if condResp.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304 despite changing raw signatures", condResp.StatusCode)
	}
}
