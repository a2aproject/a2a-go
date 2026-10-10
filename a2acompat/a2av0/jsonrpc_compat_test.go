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

package a2av0

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
)

func TestJSONRPC_ClientHeaderCompat(t *testing.T) {
	modernKey := strings.ToLower(a2a.SvcParamExtensions)
	legacyKey := "x-" + modernKey

	transport := &jsonrpcTransport{url: "http://localhost", httpClient: http.DefaultClient}

	req, err := transport.newHTTPRequest(context.Background(), "test", a2aclient.ServiceParams{
		"other":   {"val"},
		legacyKey: {"uri1"},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	if req.Header.Get(legacyKey) != "uri1" {
		t.Errorf("expected header %s: uri1, got %s", legacyKey, req.Header.Get(legacyKey))
	}
	if req.Header.Get("other") != "val" {
		t.Errorf("expected header other: val, got %s", req.Header.Get("other"))
	}
}

func TestJSONRPC_ServerExtensionsFrom(t *testing.T) {
	mock := &mockExtensionHandler{}
	handler := NewJSONRPCHandler(mock)
	server := httptest.NewServer(handler)
	defer server.Close()

	legacyKey := "x-" + strings.ToLower(a2a.SvcParamExtensions)
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"hello"}]}}}`
	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(body))
	req.Header.Set(legacyKey, "uri1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if len(mock.lastRequestedURIs) != 1 || mock.lastRequestedURIs[0] != "uri1" {
		t.Errorf("expected RequestedURIs [uri1], got %v", mock.lastRequestedURIs)
	}
}

type mockExtensionHandler struct {
	a2asrv.RequestHandler
	lastRequestedURIs []string
}

func (h *mockExtensionHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	if ext, ok := a2asrv.ExtensionsFrom(ctx); ok {
		h.lastRequestedURIs = ext.RequestedURIs()
	}
	return &a2a.Message{ID: "resp-1", Role: a2a.MessageRoleAgent, Parts: a2a.ContentParts{a2a.NewTextPart("ok")}}, nil
}

func TestJSONRPC_ClientResponseTrailingData(t *testing.T) {
	t.Parallel()

	result := json.RawMessage(`{"kind":"task","id":"task-123","contextId":"ctx-123","status":{"state":"completed"}}`)

	testCases := []struct {
		name    string
		body    func(resp []byte) []byte
		wantErr error
	}{
		{
			name: "trailing whitespace is accepted",
			body: func(resp []byte) []byte { return slices.Concat(resp, []byte("\n\t ")) },
		},
		{
			name:    "trailing garbage",
			body:    func(resp []byte) []byte { return slices.Concat(resp, []byte("TRAILING_GARBAGE")) },
			wantErr: utils.ErrTrailingData,
		},
		{
			name:    "concatenated responses",
			body:    func(resp []byte) []byte { return slices.Concat(resp, resp) },
			wantErr: utils.ErrTrailingData,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				var rpcReq jsonrpc.ClientRequest
				if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
					t.Errorf("failed to decode request: %v", err)
					return
				}
				resp, err := json.Marshal(jsonrpc.ClientResponse{JSONRPC: jsonrpc.Version, ID: rpcReq.ID, Result: result})
				if err != nil {
					t.Errorf("failed to marshal response: %v", err)
					return
				}
				rw.Header().Set("Content-Type", "application/json")
				_, _ = rw.Write(tc.body(resp))
			}))
			defer server.Close()

			transport := NewJSONRPCTransport(JSONRPCTransportConfig{URL: server.URL})

			task, err := transport.GetTask(context.Background(), a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("GetTask() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetTask() error = %v, want nil", err)
			}
			if task.ID != "task-123" {
				t.Fatalf("Task.ID = %q, want %q", task.ID, a2a.TaskID("task-123"))
			}
			if task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("Task.Status.State = %q, want %q", task.Status.State, a2a.TaskStateCompleted)
			}
		})
	}
}
