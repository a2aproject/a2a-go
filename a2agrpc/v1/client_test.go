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

package a2agrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// Embedding a generated oneof satisfies its private interface while simulating
// a future generated payload that the current converter does not support.
type futureStreamPayload struct {
	*a2apb.StreamResponse_Task
}

type mockStreamClient struct {
	a2apb.A2AServiceClient
	responses []*a2apb.StreamResponse
	err       error
}

func (c *mockStreamClient) SendStreamingMessage(ctx context.Context, _ *a2apb.SendMessageRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
	return &mockResponseStream{ctx: ctx, responses: c.responses, err: c.err}, nil
}

func (c *mockStreamClient) SubscribeToTask(ctx context.Context, _ *a2apb.SubscribeToTaskRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[a2apb.StreamResponse], error) {
	return &mockResponseStream{ctx: ctx, responses: c.responses, err: c.err}, nil
}

type mockResponseStream struct {
	grpc.ClientStream
	ctx       context.Context
	responses []*a2apb.StreamResponse
	err       error
}

func (s *mockResponseStream) Context() context.Context { return s.ctx }

func (s *mockResponseStream) Recv() (*a2apb.StreamResponse, error) {
	if len(s.responses) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func TestGRPCTransport_UnknownStreamResponses(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: "task-123", ContextID: "ctx-123", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	working := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	completed := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
	encode := func(event a2a.Event) *a2apb.StreamResponse {
		t.Helper()
		response, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			t.Fatalf("ToProtoStreamResponse() error = %v, want nil", err)
		}
		return response
	}
	pTask, pWorking, pCompleted := encode(task), encode(working), encode(completed)
	unknown := &a2apb.StreamResponse{}
	wire := protowire.AppendBytes(protowire.AppendTag(nil, 100, protowire.BytesType), []byte("private payload"))
	if err := proto.Unmarshal(wire, unknown); err != nil {
		t.Fatalf("proto.Unmarshal() error = %v, want nil", err)
	}
	knownWithExtra := proto.Clone(pWorking).(*a2apb.StreamResponse)
	knownWithExtra.ProtoReflect().SetUnknown(wire)
	knownWithExtra.GetStatusUpdate().ProtoReflect().SetUnknown(wire)
	future := &a2apb.StreamResponse{Payload: &futureStreamPayload{}}
	malformed := &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Task{Task: &a2apb.Task{Id: "task-123"}}}
	malformed.ProtoReflect().SetUnknown(wire)
	cases := []struct {
		name        string
		responses   []*a2apb.StreamResponse
		streamErr   error
		want        []a2a.Event
		wantErrText string
		wantErr     error
	}{
		{
			name:      "unknown between updates",
			responses: []*a2apb.StreamResponse{pTask, pWorking, unknown, pWorking, pCompleted},
			want:      []a2a.Event{task, working, working, completed},
		},
		{
			name:      "unknown first consecutive and last",
			responses: []*a2apb.StreamResponse{unknown, unknown, pTask, unknown, future, pCompleted, unknown},
			want:      []a2a.Event{task, completed},
		},
		{
			name:      "unknown only",
			responses: []*a2apb.StreamResponse{unknown, unknown},
		},
		{
			name:      "unsupported generated payload",
			responses: []*a2apb.StreamResponse{pTask, future, pCompleted},
			want:      []a2a.Event{task, completed},
		},
		{
			name:      "known event with unknown fields",
			responses: []*a2apb.StreamResponse{pTask, knownWithExtra, pCompleted},
			want:      []a2a.Event{task, working, completed},
		},
		{
			name:        "empty wrapper after unknown",
			responses:   []*a2apb.StreamResponse{pTask, unknown, {}, pCompleted},
			want:        []a2a.Event{task},
			wantErrText: "stream response has no payload",
		},
		{
			name: "malformed known after unknown",
			responses: []*a2apb.StreamResponse{
				pTask, unknown,
				{Payload: &a2apb.StreamResponse_StatusUpdate{StatusUpdate: &a2apb.TaskStatusUpdateEvent{}}},
				pCompleted,
			},
			want:        []a2a.Event{task},
			wantErrText: "invalid status",
		},
		{
			name:        "malformed known with unknown fields",
			responses:   []*a2apb.StreamResponse{pTask, malformed, pCompleted},
			want:        []a2a.Event{task},
			wantErrText: "invalid status",
		},
		{
			name:        "server error after unknown",
			responses:   []*a2apb.StreamResponse{pTask, unknown},
			streamErr:   status.Error(codes.Internal, "server failed"),
			want:        []a2a.Event{task},
			wantErrText: "server failed",
			wantErr:     a2a.ErrInternalError,
		},
		{
			name:        "transport error after unknown",
			responses:   []*a2apb.StreamResponse{pTask, unknown},
			streamErr:   status.Error(codes.Unavailable, "connection lost"),
			want:        []a2a.Event{task},
			wantErrText: "connection lost",
			wantErr:     a2a.ErrInternalError,
		},
	}
	for _, subscribe := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("subscribe=%t/%s", subscribe, tc.name), func(t *testing.T) {
				t.Parallel()
				transport := &grpcTransport{client: &mockStreamClient{responses: tc.responses, err: tc.streamErr}}
				stream := transport.SendStreamingMessage(t.Context(), nil, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
				if subscribe {
					stream = transport.SubscribeToTask(t.Context(), nil, &a2a.SubscribeToTaskRequest{ID: task.ID})
				}
				var got []a2a.Event
				var gotErr error
				for event, err := range stream {
					if err != nil {
						if event != nil || gotErr != nil {
							t.Fatalf("stream = (%v, %v), want a single error with no event", event, err)
						}
						gotErr = err
						continue
					}
					got = append(got, event)
				}
				if tc.wantErrText != "" {
					if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantErrText) {
						t.Fatalf("stream error = %v, want %q", gotErr, tc.wantErrText)
					}
				} else if gotErr != nil {
					t.Fatalf("stream error = %v, want nil", gotErr)
				}
				if tc.wantErr != nil && !errors.Is(gotErr, tc.wantErr) {
					t.Fatalf("stream error = %v, want %v", gotErr, tc.wantErr)
				}
				if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
					t.Fatalf("stream wrong result (-want +got) diff = %s", diff)
				}
			})
		}
	}
}

func TestGRPCTransport_StreamResponseWireTypes(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: "task-123", ContextID: "ctx-123", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
	working := a2a.NewStatusUpdateEvent(task, a2a.TaskStateWorking, nil)
	completed := a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil)
	encode := func(event a2a.Event) *a2apb.StreamResponse {
		t.Helper()
		response, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			t.Fatalf("ToProtoStreamResponse() error = %v, want nil", err)
		}
		return response
	}
	pTask, pWorking, pCompleted := encode(task), encode(working), encode(completed)
	futureWire := protowire.AppendBytes(protowire.AppendTag(nil, 100, protowire.BytesType), []byte("future payload"))
	future := &a2apb.StreamResponse{}
	future.ProtoReflect().SetUnknown(futureWire)
	fields := future.ProtoReflect().Descriptor().Oneofs().ByName("payload").Fields()
	taskField := fields.ByName("task")
	wrongTaskWire := protowire.AppendVarint(protowire.AppendTag(nil, taskField.Number(), protowire.VarintType), 1)
	unknownGroup := protowire.AppendTag(nil, 100, protowire.StartGroupType)
	unknownGroup = protowire.AppendVarint(protowire.AppendTag(unknownGroup, taskField.Number(), protowire.VarintType), 1)
	unknownGroup = protowire.AppendTag(unknownGroup, 100, protowire.EndGroupType)
	type testCase struct {
		name             string
		wire             []byte
		withKnownPayload bool
		want             []a2a.Event
		wantErrText      string
	}
	cases := []testCase{
		{
			name: "future field",
			wire: futureWire,
			want: []a2a.Event{task, completed},
		},
		{
			name: "unknown group with matching nested field number",
			wire: unknownGroup,
			want: []a2a.Event{task, completed},
		},
		{
			name:        "unknown field before malformed known field",
			wire:        append(append([]byte(nil), futureWire...), wrongTaskWire...),
			want:        []a2a.Event{task},
			wantErrText: `invalid wire type for stream response field "task"`,
		},
		{
			name:             "known payload retains existing wrong wire field behavior",
			wire:             wrongTaskWire,
			withKnownPayload: true,
			want:             []a2a.Event{task, working, completed},
		},
		{
			name:        "malformed embedded task",
			wire:        protowire.AppendBytes(protowire.AppendTag(nil, taskField.Number(), protowire.BytesType), []byte{0xff}),
			want:        []a2a.Event{task},
			wantErrText: "failed to unmarshal the received message",
		},
		{
			name:        "empty wrapper",
			want:        []a2a.Event{task},
			wantErrText: "stream response has no payload",
		},
	}
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		cases = append(cases, testCase{
			name:        fmt.Sprintf("wrong wire type for %s", field.Name()),
			wire:        protowire.AppendVarint(protowire.AppendTag(nil, field.Number(), protowire.VarintType), 1),
			want:        []a2a.Event{task},
			wantErrText: fmt.Sprintf("invalid wire type for stream response field %q", field.Name()),
		})
	}
	for _, subscribe := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("subscribe=%t/%s", subscribe, tc.name), func(t *testing.T) {
				t.Parallel()
				middle := &a2apb.StreamResponse{}
				if tc.withKnownPayload {
					middle = proto.Clone(pWorking).(*a2apb.StreamResponse)
				}
				// Preserve these bytes until gRPC's real protobuf decoder receives them.
				middle.ProtoReflect().SetUnknown(tc.wire)
				conn := newStreamResponseClientConn(t, []*a2apb.StreamResponse{pTask, future, middle, pCompleted})
				transport := NewGRPCTransport(conn)
				stream := transport.SendStreamingMessage(t.Context(), nil, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test"))})
				if subscribe {
					stream = transport.SubscribeToTask(t.Context(), nil, &a2a.SubscribeToTaskRequest{ID: task.ID})
				}
				var got []a2a.Event
				var gotErr error
				for event, err := range stream {
					if err != nil {
						if event != nil || gotErr != nil {
							t.Fatalf("stream = (%v, %v), want a single error with no event", event, err)
						}
						gotErr = err
						continue
					}
					got = append(got, event)
				}
				if tc.wantErrText != "" {
					if gotErr == nil || !strings.Contains(gotErr.Error(), tc.wantErrText) {
						t.Fatalf("stream error = %v, want %q", gotErr, tc.wantErrText)
					}
				} else if gotErr != nil {
					t.Fatalf("stream error = %v, want nil", gotErr)
				}
				if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty()); diff != "" {
					t.Fatalf("stream wrong result (-want +got) diff = %s", diff)
				}
			})
		}
	}
}

type streamResponseServer struct {
	a2apb.UnimplementedA2AServiceServer
	responses []*a2apb.StreamResponse
}

func (s *streamResponseServer) send(stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	for _, response := range s.responses {
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamResponseServer) SendStreamingMessage(_ *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	return s.send(stream)
}

func (s *streamResponseServer) SubscribeToTask(_ *a2apb.SubscribeToTaskRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	return s.send(stream)
}

func newStreamResponseClientConn(t *testing.T, responses []*a2apb.StreamResponse) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	a2apb.RegisterA2AServiceServer(server, &streamResponseServer{responses: responses})
	t.Cleanup(server.Stop)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("server.Serve() error = %v, want nil", err)
		}
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("conn.Close() error = %v, want nil", err)
		}
	})
	return conn
}
