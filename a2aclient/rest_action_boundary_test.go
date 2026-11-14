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

package a2aclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestRESTGetTaskEscapesActionDelimiter(t *testing.T) {
	t.Parallel()
	paths := make(chan string, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"foo:subscribe","status":{"state":"TASK_STATE_WORKING"}}`))
		if err != nil {
			t.Errorf("Write() error = %v", err)
		}
	}))
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewRESTTransport(u, s.Client()).GetTask(context.Background(), nil, &a2a.GetTaskRequest{ID: "foo:subscribe"})
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got := <-paths; got != "/tasks/foo%3Asubscribe" {
		t.Fatalf("GetTask() path = %q, want %q", got, "/tasks/foo%3Asubscribe")
	}
}
