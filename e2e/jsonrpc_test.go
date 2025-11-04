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

	executor := testexecutor.FromEventGenerator(func(reqCtx *a2asrv.RequestContext) []a2a.Event {
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

	card := &a2a.AgentCard{
		URL:                fmt.Sprintf("%s/invoke", server.URL),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}

	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(reqHandler))

	card, err := agentcard.DefaultResolver.Resolve(ctx, server.URL)
	if err != nil {
		t.Fatalf("resolver.Resolve() error = %v", err)
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}

	var received []a2a.Event
	msg := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser)}
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
