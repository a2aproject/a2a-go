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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestAgentCardHandler(t *testing.T) {
	card := &a2a.AgentCard{Name: "Agent", Description: "Test"}

	testCases := []struct {
		method      string
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
			t.Run(tc.method+" "+srvType, func(t *testing.T) {
				t.Parallel()
				req, err := http.NewRequestWithContext(t.Context(), tc.method, url, nil)
				if err != nil {
					t.Errorf("http.NewRequestWithContext() error = %v", err)
					return
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
						t.Errorf("wrong card (+got,-want) diff = %s", diff)
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
