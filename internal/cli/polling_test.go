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

package cli

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
)

func TestHandlePolling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		sendResult   a2a.SendMessageResult
		sendErr      error
		getResponses []getTaskResponse
		wantEvents   []a2a.Event
		wantErr      string
	}{
		{
			name:       "direct message reply stops without polling",
			sendResult: &a2a.Message{},
			wantEvents: []a2a.Event{&a2a.Message{}},
		},
		{
			name:    "send failure yields error",
			sendErr: errors.New("network down"),
			wantErr: "failed to send a message",
		},
		{
			name:       "terminal task on send stops without polling",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			wantEvents: []a2a.Event{&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
		},
		{
			name:       "input-required task on send stops without polling",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
			wantEvents: []a2a.Event{&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}},
		},
		{
			name:       "polls until completion recovering intermediate updates",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			getResponses: []getTaskResponse{
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
		},
		{
			name:       "polls until input-required",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			getResponses: []getTaskResponse{
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}}},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
			},
		},
		{
			name:       "recovers artifact events during transition",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			getResponses: []getTaskResponse{
				{task: &a2a.Task{
					Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
					Artifacts: []*a2a.Artifact{
						{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("foo")}},
					},
				}},
				{task: &a2a.Task{
					Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
					Artifacts: []*a2a.Artifact{
						{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("foo"), a2a.NewTextPart("bar")}},
					},
				}},
				{task: &a2a.Task{
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("foo"), a2a.NewTextPart("bar")}},
					},
				}},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskArtifactUpdateEvent{Artifact: &a2a.Artifact{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("foo")}}},
				&a2a.TaskArtifactUpdateEvent{
					Append:   true,
					Artifact: &a2a.Artifact{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("bar")}},
				},
				&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{
					Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
					Artifacts: []*a2a.Artifact{
						{ID: "a-1", Parts: a2a.ContentParts{a2a.NewTextPart("foo"), a2a.NewTextPart("bar")}},
					},
				},
			},
		},
		{
			name:       "transient failures below threshold recover",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			getResponses: []getTaskResponse{
				{err: errors.New("temporary 1")},
				{err: errors.New("temporary 2")},
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}},
				{err: errors.New("temporary 3")},
				{task: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}},
			},
			wantEvents: []a2a.Event{
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
				&a2a.TaskStatusUpdateEvent{Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
				&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}},
			},
		},
		{
			name:       "successive failures exceed threshold",
			sendResult: &a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}},
			getResponses: []getTaskResponse{
				{err: errors.New("temporary 1")},
				{err: errors.New("temporary 2")},
				{err: errors.New("temporary 3")},
			},
			wantEvents: []a2a.Event{&a2a.Task{Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}},
			wantErr:    "successive polling failure threshold exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &fakePollingTransport{
				sendResult:   tt.sendResult,
				sendErr:      tt.sendErr,
				getResponses: tt.getResponses,
			}
			client := newPollingClient(t, transport)
			req := &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("go"))}

			gotEvents, err := drainAll(handlePolling(t.Context(), client, req, 0))

			if tt.wantErr == "" && err != nil {
				t.Fatalf("handlePolling() error = %v, want nil", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("handlePolling() error = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("handlePolling() error = %v, want error containing %q", err, tt.wantErr)
				}
			}

			if diff := cmp.Diff(tt.wantEvents, gotEvents); diff != "" {
				t.Fatalf("handlePolling() events wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestHandlePolling_SetsReturnImmediately(t *testing.T) {
	t.Parallel()

	for _, config := range []*a2a.SendMessageConfig{nil, {}} {
		transport := &fakePollingTransport{sendResult: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))}
		client := newPollingClient(t, transport)
		req := &a2a.SendMessageRequest{
			Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("go")),
			Config:  config,
		}

		if _, err := drainAll(handlePolling(t.Context(), client, req, 0)); err != nil {
			t.Fatalf("handlePolling() error = %v, want nil", err)
		}

		if config == nil && req.Config != nil {
			t.Error("handlePolling() changed input request")
		}
		if config != nil && config.ReturnImmediately {
			t.Error("handlePolling() changed input config")
		}
		if transport.sendRequest.Config == nil || !transport.sendRequest.Config.ReturnImmediately {
			t.Error("handlePolling() send request without ReturnImmediately = true")
		}
	}
}

type getTaskResponse struct {
	task *a2a.Task
	err  error
}

type fakePollingTransport struct {
	a2aclient.Transport

	sendResult a2a.SendMessageResult
	sendErr    error

	getResponses []getTaskResponse
	getCalls     int

	sendRequest *a2a.SendMessageRequest
}

func (f *fakePollingTransport) SendMessage(ctx context.Context, c a2aclient.ServiceParams, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	f.sendRequest = req
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return f.sendResult, nil
}

func (f *fakePollingTransport) GetTask(context.Context, a2aclient.ServiceParams, *a2a.GetTaskRequest) (*a2a.Task, error) {
	i := f.getCalls
	f.getCalls++
	if i >= len(f.getResponses) {
		return nil, fmt.Errorf("ran out of getResponses")
	}
	r := f.getResponses[i]
	return r.task, r.err
}

func newPollingClient(t *testing.T, transport a2aclient.Transport) *a2aclient.Client {
	t.Helper()
	iface := a2a.NewAgentInterface("https://agent.test", a2a.TransportProtocolJSONRPC)
	client, err := a2aclient.NewFromEndpoints(t.Context(),
		[]*a2a.AgentInterface{iface},
		a2aclient.WithTransport(a2a.TransportProtocolJSONRPC,
			a2aclient.TransportFactoryFn(func(context.Context, *a2a.AgentCard, *a2a.AgentInterface) (a2aclient.Transport, error) {
				return transport, nil
			})),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}
	return client
}

func drainAll(seq iter.Seq2[a2a.Event, error]) ([]a2a.Event, error) {
	var events []a2a.Event
	var gotErr error
	for e, err := range seq {
		if err != nil {
			gotErr = err
			break
		}
		events = append(events, e)
	}
	return events, gotErr
}
