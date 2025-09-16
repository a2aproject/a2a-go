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
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
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
	url := mustServe(t, defaultAgentCardPath, mustMarshal(t, want), nil)
	resolver := Resolver{BaseURL: url}

	got, err := resolver.Resolve(t.Context())
	if err != nil {
		t.Fatalf("Resolve() failed with: %v", err)
	}

	if want != got {
		t.Errorf("Resolve() card returned %v, want %v", got, want)
	}
}

func TestResolver_CustomPath(t *testing.T) {
	path := "/custom/agent.json"
	want := &a2a.AgentCard{Name: "TestResolver_DefaultPath"}
	url := mustServe(t, path, mustMarshal(t, want), nil)

	resolver := Resolver{BaseURL: url}
	got, err := resolver.Resolve(t.Context())
	var httpErr *ErrStatusNotOK
	if err == nil || !errors.As(err, &httpErr) {
		t.Fatalf("expected Resolve() to fail with ErrStatusNotOK, got %v, %v", got, err)
	}
	if httpErr.StatusCode != 404 {
		t.Fatalf("expected Resolve() to fail with 404, got %v", httpErr)
	}

	got, err = resolver.Resolve(t.Context(), WithPath(path))
	if err != nil {
		t.Fatalf("Resolve() failed with %v", err)
	}

	if want != got {
		t.Errorf("Resolve() card returned %v, want %v", got, want)
	}
}

func TestResolver_CustomHeader(t *testing.T) {
	h, hval := "X-Header-Test", "TestResolver_CustomHeader"

	capturedHeader := []string{}
	card := &a2a.AgentCard{Name: "TestResolver_CustomHeader"}
	url := mustServe(t, defaultAgentCardPath, mustMarshal(t, card), func(req *http.Request) {
		capturedHeader = req.Header[h]
	})

	resolver := Resolver{BaseURL: url}
	_, err := resolver.Resolve(t.Context(), WithRequestHeader(h, hval))
	if err != nil {
		t.Fatalf("Resolve() failed with: %v", err)
	}

	if len(capturedHeader) != 1 || capturedHeader[0] != hval {
		t.Errorf("expected request %s to be %s, got %v", h, hval, capturedHeader)
	}
}

func TestResolver_MalformedJSON(t *testing.T) {
	url := mustServe(t, defaultAgentCardPath, []byte(`}{`), nil)

	resolver := Resolver{BaseURL: url}
	got, err := resolver.Resolve(t.Context())
	if err == nil {
		t.Fatalf("expected Resolve() to fail on malformed response, got: %v", got)
	}
}
