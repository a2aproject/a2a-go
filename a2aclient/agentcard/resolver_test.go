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

package agentcard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

func mustMarshal(t *testing.T, data any) []byte {
	t.Helper()
	res, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("AgentCard marshaling failed: %v", err)
	}
	return res
}

func mustServe(t *testing.T, path string, body []byte, callback func(r *http.Request)) (addr string) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if callback != nil {
			callback(r)
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("failed to server %s: %v", path, err)
		}
	})
	srv := httptest.NewServer(mux)

	t.Cleanup(func() {
		srv.Close()
	})

	return srv.URL
}

func TestResolver_DefaultPath(t *testing.T) {
	want := &a2a.AgentCard{Name: "TestResolver_DefaultPath"}
	cardURL := mustServe(t, defaultAgentCardPath, mustMarshal(t, want), nil)
	resolver := Resolver{}

	for _, u := range []string{cardURL, cardURL + "/"} {
		got, err := resolver.Resolve(t.Context(), u)
		if err != nil {
			t.Fatalf("Resolve(%s) failed with: %v", u, err)
		}

		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("AgentCards are different for %s:\ngot %v\nwant %v\ndiff(-want +got):\n%v", u, got, want, diff)
		}
	}
}

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
		wantErr bool
	}{
		{
			name:    "https://example.com/",
			baseURL: "https://example.com/",
			want:    "https://example.com/.well-known/agent-card.json",
		},
		{
			name:    "https://example.com",
			baseURL: "https://example.com",
			want:    "https://example.com/.well-known/agent-card.json",
		},
		{
			name:    "https://example.com/ with custom path",
			baseURL: "https://example.com/",
			path:    "custom/agent.json",
			want:    "https://example.com/custom/agent.json",
		},
		{
			name:    "https://example.com with custom path",
			baseURL: "https://example.com",
			path:    "custom/agent.json",
			want:    "https://example.com/custom/agent.json",
		},
		{
			name:    "https://example.com with leading slash custom path",
			baseURL: "https://example.com",
			path:    "/custom/agent.json",
			want:    "https://example.com/custom/agent.json",
		},
		{
			name:    "complete card URL",
			baseURL: "https://example.com/custom/agent.json",
			want:    "https://example.com/custom/agent.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.baseURL, tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("buildURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolver_CustomPath(t *testing.T) {
	ctx := t.Context()
	path := "/custom/agent.json"
	want := &a2a.AgentCard{Name: "TestResolver_DefaultPath"}
	cardURL := mustServe(t, path, mustMarshal(t, want), nil)

	resolver := Resolver{}
	got, err := resolver.Resolve(ctx, cardURL)
	var httpErr *ErrStatusNotOK
	if err == nil || !errors.As(err, &httpErr) {
		t.Fatalf("expected Resolve() to fail with ErrStatusNotOK, got %v, %v", got, err)
	}
	if httpErr.StatusCode != 404 {
		t.Fatalf("expected Resolve() to fail with 404, got %v", httpErr)
	}

	for _, p := range []string{path, strings.TrimPrefix(path, "/")} {
		got, err = resolver.Resolve(ctx, cardURL, WithPath(p))
		if err != nil {
			t.Fatalf("Resolve(%s) failed with %v", p, err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("AgentCards are different:\ngot %v\nwant %v\ndiff(-want +got):\n%v", got, want, diff)
		}
	}
}

func TestResolver_CustomHeader(t *testing.T) {
	h, hval := "X-Header-Test", "TestResolver_CustomHeader"

	capturedHeader := []string{}
	card := &a2a.AgentCard{Name: "TestResolver_CustomHeader"}
	cardURL := mustServe(t, defaultAgentCardPath, mustMarshal(t, card), func(req *http.Request) {
		capturedHeader = req.Header[h]
	})

	resolver := NewResolver(nil)
	_, err := resolver.Resolve(t.Context(), cardURL, WithRequestHeader(h, hval))
	if err != nil {
		t.Fatalf("Resolve() failed with: %v", err)
	}

	if len(capturedHeader) != 1 || capturedHeader[0] != hval {
		t.Errorf("expected request %s to be %s, got %v", h, hval, capturedHeader)
	}
}

func TestResolver_MalformedJSON(t *testing.T) {
	cardURL := mustServe(t, defaultAgentCardPath, []byte(`}{`), nil)

	resolver := NewResolver(nil)
	got, err := resolver.Resolve(t.Context(), cardURL)
	if err == nil {
		t.Fatalf("expected Resolve() to fail on malformed response, got: %v", got)
	}
}

func TestResolver_FileURL(t *testing.T) {
	t.Parallel()
	want := &a2a.AgentCard{Name: "TestResolver_FileURL"}
	dir := t.TempDir()
	cardPath := filepath.Join(dir, "agent-card.json")
	if err := os.WriteFile(cardPath, mustMarshal(t, want), 0o600); err != nil {
		t.Fatalf("failed to write card file: %v", err)
	}

	tests := []struct {
		name      string
		url       *url.URL
		opts      []ResolveOption
		wantErr   bool
		wantErrIs error
	}{
		{
			name:      "missing file",
			url:       &url.URL{Scheme: "file", Path: filepath.Join(dir, "missing.json")},
			wantErr:   true,
			wantErrIs: os.ErrNotExist,
		},
		{
			name:    "unsupported host",
			url:     &url.URL{Scheme: "file", Host: "remotehost", Path: "/agent-card.json"},
			wantErr: true,
		},
		{
			name: "empty host",
			url:  &url.URL{Scheme: "file", Path: cardPath},
		},
		{
			name: "localhost",
			url:  &url.URL{Scheme: "file", Host: "localhost", Path: cardPath},
		},
		{
			name: "path as option",
			url:  &url.URL{Scheme: "file", Host: "localhost", Path: dir},
			opts: []ResolveOption{WithPath("agent-card.json")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DefaultResolver.Resolve(t.Context(), tt.url.String(), tt.opts...)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Resolve(%s) = %v, want error", tt.url, got)
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("Resolve(%s) error = %v, want error matching %v", tt.url, err, tt.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%s) error = %v, want nil", tt.url, err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Resolve(%s) wrong result (-want +got) diff = %s", tt.url, diff)
			}
		})
	}
}

func TestResolver_CompleteCardURL(t *testing.T) {
	want := &a2a.AgentCard{Name: "TestResolver_CompleteCardURL"}
	path := "/custom/agent-card.json"
	baseURL := mustServe(t, path, mustMarshal(t, want), nil)
	resolver := Resolver{}

	got, err := resolver.Resolve(t.Context(), baseURL+path)
	if err != nil {
		t.Fatalf("Resolve() failed with: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("AgentCards are different:\ngot %v\nwant %v\ndiff(-want +got):\n%v", got, want, diff)
	}
}
