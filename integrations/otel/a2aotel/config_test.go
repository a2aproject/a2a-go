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

package a2aotel

import (
	"context"
	"iter"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// The tests set environment variables, so they can't run in parallel.
func TestPlugins_EnabledFromEnv(t *testing.T) {
	testCases := []struct {
		value       string
		wantEnabled bool
	}{
		{value: "", wantEnabled: true},
		{value: "true", wantEnabled: true},
		{value: "TRUE", wantEnabled: true},
		{value: "not-a-bool", wantEnabled: true},
		{value: "false", wantEnabled: false},
		{value: " False ", wantEnabled: false},
	}
	for _, tc := range testCases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv(EnvEnabled, tc.value)
			telemetry := newTestTelemetry(t)
			clientPlugin := NewClientPlugin(telemetry.clientOptions()...)
			serverHandler := a2asrv.NewHandler(completingExecutor(), a2asrv.WithPlugins(NewServerPlugin(telemetry.serverOptions()...)))
			var sentParams a2aclient.ServiceParams
			next := func(_ context.Context, req a2aclient.Request) iter.Seq2[any, error] {
				sentParams = req.ServiceParams
				return newClientHandler(callResult{Payload: &a2a.Task{}})(t.Context(), req)
			}

			collect(clientPlugin.WrapCall(next)(t.Context(), a2aclient.Request{Method: "GetTask", ServiceParams: a2aclient.ServiceParams{}}))
			if _, err := serverHandler.GetTask(callContext(t.Context(), nil), &a2a.GetTaskRequest{ID: "task-404"}); err == nil {
				t.Fatal("handler.GetTask() error = nil, want an error")
			}

			wantSpans := 0
			if tc.wantEnabled {
				wantSpans = 2
			}
			if got := len(telemetry.recorder.Ended()); got != wantSpans {
				t.Fatalf("plugins recorded %d spans, want %d", got, wantSpans)
			}
			if got := len(sentParams.Get("traceparent")) > 0; got != tc.wantEnabled {
				t.Fatalf("client plugin propagated trace context = %v, want %v", got, tc.wantEnabled)
			}
		})
	}
}
