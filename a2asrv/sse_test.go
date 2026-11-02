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

package a2asrv

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"go.uber.org/goleak"
)

func TestSSE_PanicAfterClientDisconnectDoesNotLeak(t *testing.T) {
	testCases := []struct {
		name          string
		createHandler func(RequestHandler) http.Handler
	}{
		{
			name: "jsonrpc",
			createHandler: func(rh RequestHandler) http.Handler {
				return NewJSONRPCHandler(rh)
			},
		},
		{
			name: "rest",
			createHandler: func(rh RequestHandler) http.Handler {
				return NewRESTHandler(rh)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// IgnoreCurrent snapshots goroutines already running (e.g. leaked by
			// other tests in the package) so this check only flags goroutines
			// leaked by the SSE handler under test.
			defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

			release := make(chan struct{})
			mock := &mockRequestHandler{
				subscribeToTaskFunc: func(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
					return func(yield func(a2a.Event, error) bool) {
						<-release // stay parked until the client has "disconnected"
						panic("boom")
					}
				},
			}
			handler := tc.createHandler(mock)
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest(http.MethodGet, "/tasks/task-1:subscribe", nil).WithContext(ctx)
			served := make(chan struct{})
			go func() {
				handler.ServeHTTP(httptest.NewRecorder(), req)
				close(served)
			}()
			cancel()       // client disconnect -> main select loop returns
			<-served       // the panicChan reader is now gone
			close(release) // the streaming goroutine panics into an unread panicChan
		})
	}
}
