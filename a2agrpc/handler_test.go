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

package a2agrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// mockRequestHandler is a mock of a2asrv.RequestHandler.
type mockRequestHandler struct {
	a2asrv.RequestHandler
	OnSendMessageFunc func(ctx context.Context, message a2a.MessageSendParams) (a2a.SendMessageResult, error)
}

func (m *mockRequestHandler) OnSendMessage(ctx context.Context, message a2a.MessageSendParams) (a2a.SendMessageResult, error) {
	if m.OnSendMessageFunc != nil {
		return m.OnSendMessageFunc(ctx, message)
	}
	return nil, errors.New("OnSendMessage not implemented")
}

func startTestServer(t *testing.T, handler a2asrv.RequestHandler) (a2apb.A2AServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	grpcHandler := NewHandler(handler)
	grpcHandler.RegisterWith(s)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("Server exited with error: %v", err)
		}
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	client := a2apb.NewA2AServiceClient(conn)

	stop := func() {
		s.Stop()
		_ = conn.Close()
	}

	return client, stop
}

func TestGrpcHandler_SendMessage(t *testing.T) {
	ctx := t.Context()
	taskID := "test-task-123"
	reqMessageID := "req-msg-123"
	respMessageID := "resp-msg-456"
	reqText := "Hello Agent"
	respText := "Hello User"

	mockHandler := &mockRequestHandler{
		OnSendMessageFunc: func(ctx context.Context, params a2a.MessageSendParams) (a2a.SendMessageResult, error) {
			// Check if the request was converted correctly
			if params.Message.ID != reqMessageID {
				return nil, fmt.Errorf("expected message ID %q, got %q", reqMessageID, params.Message.ID)
			}
			if params.Message.TaskID != a2a.TaskID(taskID) {
				return nil, fmt.Errorf("expected task ID %q, got %q", taskID, params.Message.TaskID)
			}
			if len(params.Message.Parts) != 1 {
				return nil, fmt.Errorf("expected 1 part, got %d", len(params.Message.Parts))
			}
			textPart, ok := params.Message.Parts[0].(a2a.TextPart)
			if !ok {
				return nil, fmt.Errorf("expected TextPart, got %T", params.Message.Parts[0])
			}
			if textPart.Text != reqText {
				return nil, fmt.Errorf("expected text %q, got %q", reqText, textPart.Text)
			}

			// Return a response message
			return &a2a.Message{
				ID:     respMessageID,
				TaskID: a2a.TaskID(taskID),
				Role:   a2a.MessageRoleAgent,
				Parts: []a2a.Part{
					a2a.TextPart{Text: respText},
				},
			}, nil
		},
	}

	client, stop := startTestServer(t, mockHandler)
	defer stop()

	req := &a2apb.SendMessageRequest{
		Request: &a2apb.Message{
			MessageId: reqMessageID,
			TaskId:    taskID,
			Role:      a2apb.Role_ROLE_USER,
			Content: []*a2apb.Part{
				{
					Part: &a2apb.Part_Text{Text: reqText},
				},
			},
		},
	}

	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Check response
	pld := resp.GetMsg()
	if pld == nil {
		t.Fatalf("expected message payload, got %T", resp.GetPayload())
	}
	if pld.GetMessageId() != respMessageID {
		t.Errorf("expected response message ID %q, got %q", respMessageID, pld.GetMessageId())
	}
	if pld.GetTaskId() != taskID {
		t.Errorf("expected response task ID %q, got %q", taskID, pld.GetTaskId())
	}
	if pld.GetRole() != a2apb.Role_ROLE_AGENT {
		t.Errorf("expected role AGENT, got %v", pld.GetRole())
	}
	if len(pld.GetContent()) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(pld.GetContent()))
	}
	textPart := pld.GetContent()[0].GetText()
	if textPart != respText {
		t.Errorf("expected response text %q, got %q", respText, textPart)
	}
}
