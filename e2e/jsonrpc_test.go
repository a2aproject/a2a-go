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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
)

func TestJSONRPC_Streaming(t *testing.T) {
	ctx := t.Context()

	executor := testexecutor.FromEventGenerator(func(reqCtx *a2asrv.ExecutorContext) []a2a.Event {
		task := &a2a.Task{ID: reqCtx.TaskID, ContextID: reqCtx.ContextID}
		artifact := a2a.NewArtifactEvent(task, a2a.TextPart{Text: "Hello"})
		finalUpdate := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Done!"}))
		finalUpdate.Final = true
		return []a2a.Event{
			a2a.NewStatusUpdateEvent(task, a2a.TaskStateSubmitted, nil),
			a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil),
			artifact,
			a2a.NewArtifactUpdateEvent(task, artifact.Artifact.ID, a2a.TextPart{Text: ", world!"}),
			finalUpdate,
		}
	})
	reqHandler := a2asrv.NewHandler(executor)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	card := newAgentCard(fmt.Sprintf("%s/invoke", server.URL))

	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(reqHandler))

	card, err := agentcard.DefaultResolver.Resolve(ctx, server.URL)
	if err != nil {
		t.Fatalf("resolver.Resolve() error = %v", err)
	}

	client := mustCreateClient(t, card)

	var received []a2a.Event
	msg := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"})}
	for event, err := range client.SendStreamingMessage(ctx, msg) {
		if err != nil {
			t.Fatalf("client.SendStreamingMessage() error = %v", err)
		}
		received = append(received, event)
	}

	if diff := cmp.Diff(executor.Emitted, received); diff != "" {
		t.Fatalf("client.SendStreamingMessage() wrong events (+got,-want) diff = %s", diff)
	}
}

func TestJSONRPC_ExecutionScopeStreamingPanic(t *testing.T) {
	ctx := t.Context()

	reqHandler := a2asrv.NewHandler(
		testexecutor.FromEventGenerator(func(reqCtx *a2asrv.ExecutorContext) []a2a.Event {
			panic("oh no")
		}),
		a2asrv.WithExecutionPanicHandler(func(r any) error {
			return a2a.ErrInvalidRequest
		}),
	)

	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler))
	client := mustCreateClient(t, newAgentCard(server.URL))

	var gotErr error
	msg := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"})}
	for _, err := range client.SendStreamingMessage(ctx, msg) {
		gotErr = err
	}
	if !errors.Is(gotErr, a2a.ErrInvalidRequest) {
		t.Fatalf("client.SendStreamingMessage() error = %v, want %v", gotErr, a2a.ErrInvalidRequest)
	}
}

func TestJSONRPC_RequestScopeStreamingPanic(t *testing.T) {
	ctx := t.Context()

	reqHandler := a2asrv.NewHandler(
		&testexecutor.TestAgentExecutor{},
		a2asrv.WithCallInterceptors(&fnInterceptor{
			beforeFn: func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
				panic("oh no")
			},
		}),
	)

	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(reqHandler, a2asrv.WithPanicHandler(func(r any) error {
		return a2a.ErrInternalError
	})))
	client := mustCreateClient(t, newAgentCard(server.URL))

	var gotErr error
	msg := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Work"})}
	for _, err := range client.SendStreamingMessage(ctx, msg) {
		gotErr = err
	}
	if !errors.Is(gotErr, a2a.ErrInternalError) {
		t.Fatalf("client.SendStreamingMessage() error = %v, want %v", gotErr, a2a.ErrInternalError)
	}
}

func mustCreateClient(t *testing.T, card *a2a.AgentCard) *a2aclient.Client {
	t.Helper()
	client, err := a2aclient.NewFromCard(t.Context(), card)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	return client
}

func newAgentCard(url string) *a2a.AgentCard {
	return &a2a.AgentCard{
		URL:                url,
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}
}

// TODO: replace with public utility
type fnInterceptor struct {
	a2asrv.PassthroughCallInterceptor
	beforeFn func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error)
}

func (ci *fnInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	if ci.beforeFn != nil {
		return ci.beforeFn(ctx, callCtx, req)
	}
	return ctx, nil, nil
}
