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

package structuredinput

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestRelayRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		received *Request
		want     *Request
		wantErr  bool
	}{
		{
			name:     "person required",
			received: &Request{ID: "deploy-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson},
			want:     &Request{ID: "relay-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson, AskedBy: "deploy"},
		},
		{
			name:     "no answer source required",
			received: &Request{ID: "deploy-1", Schema: newTestSchema()},
			want:     &Request{ID: "relay-1", Schema: newTestSchema(), AskedBy: "deploy"},
		},
		{
			name:     "already relayed request keeps the original asker",
			received: &Request{ID: "deploy-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson, AskedBy: "payments"},
			want:     &Request{ID: "relay-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson, AskedBy: "payments"},
		},
		{
			name:    "nil request",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			relayed, err := RelayRequest(tc.received, "relay-1", "deploy")
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("RelayRequest() error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
			event, err := NewRequestEvent(taskInfo, relayed, a2a.NewTextPart("approve the deployment"))
			if err != nil {
				t.Fatalf("NewRequestEvent() error = %v", err)
			}
			got, err := ParseRequest(jsonRoundTrip(t, event.Status.Message))
			if err != nil {
				t.Fatalf("ParseRequest() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(jsonschema.Schema{})); diff != "" {
				t.Fatalf("ParseRequest() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestRelayResponse(t *testing.T) {
	t.Parallel()

	taskInfo := a2a.TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}
	input := map[string]any{"status": "ok", "approver_id": "alice"}

	tests := []struct {
		name     string
		received *Request
		answer   *ProcessorResult
		want     PartMetadata
		wantErr  bool
	}{
		{
			name:     "answer from a person",
			received: &Request{ID: "deploy-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson},
			answer:   &ProcessorResult{Outcome: OutcomeAccepted, Input: input, AnsweredBy: AnswerSourcePerson},
			want:     PartMetadata{Type: PartTypeStructuredInput, ID: "deploy-1", AnsweredBy: AnswerSourcePerson},
		},
		{
			name:     "answer from a policy when no source is required",
			received: &Request{ID: "deploy-1", Schema: newTestSchema()},
			answer:   &ProcessorResult{Outcome: OutcomeAccepted, Input: input, AnsweredBy: AnswerSourcePolicy},
			want:     PartMetadata{Type: PartTypeStructuredInput, ID: "deploy-1", AnsweredBy: AnswerSourcePolicy},
		},
		{
			name:     "person required and answer from an agent",
			received: &Request{ID: "deploy-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson},
			answer:   &ProcessorResult{Outcome: OutcomeAccepted, Input: input, AnsweredBy: AnswerSourceAgent},
			wantErr:  true,
		},
		{
			name:     "person required and source not stated",
			received: &Request{ID: "deploy-1", Schema: newTestSchema(), AnswerFrom: AnswerSourcePerson},
			answer:   &ProcessorResult{Outcome: OutcomeAccepted, Input: input},
			wantErr:  true,
		},
		{
			name:     "answer not accepted",
			received: &Request{ID: "deploy-1", Schema: newTestSchema()},
			answer:   &ProcessorResult{Outcome: OutcomeInvalid},
			wantErr:  true,
		},
		{
			name:    "nil request",
			answer:  &ProcessorResult{Outcome: OutcomeAccepted, Input: input},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RelayResponse(taskInfo, tc.received, tc.answer)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("RelayResponse() error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			msg = jsonRoundTrip(t, msg)
			if len(msg.Parts) != 1 {
				t.Fatalf("RelayResponse() parts = %d, want 1", len(msg.Parts))
			}
			got, ok := GetPartMetadata(msg.Parts[0])
			if !ok {
				t.Fatalf("GetPartMetadata() ok = false, want extension metadata")
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("GetPartMetadata() wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(any(input), msg.Parts[0].Data()); diff != "" {
				t.Fatalf("RelayResponse() wrong input (-want +got) diff = %s", diff)
			}
		})
	}
}
