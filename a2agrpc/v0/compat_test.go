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
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/a2apb"
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestCompat_WithGRPCMetadata(t *testing.T) {
	modernKey := strings.ToLower(a2a.SvcParamExtensions)
	legacyKey := "x-" + modernKey

	ctx := withGRPCMetadata(context.Background(), a2aclient.ServiceParams{
		modernKey: []string{"uri1"},
		"other":   []string{"val"},
	})
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected metadata in outgoing context")
	}

	if !reflect.DeepEqual(md[legacyKey], []string{"uri1"}) {
		t.Errorf("expected %s: %v, got %v", legacyKey, []string{"uri1"}, md[legacyKey])
	}
	if !reflect.DeepEqual(md["other"], []string{"val"}) {
		t.Errorf("expected other: %v, got %v", []string{"val"}, md["other"])
	}
}

func TestCompat_WithGRPCMetadataCallerMetadata(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		callerMD []string
		params   a2aclient.ServiceParams
		want     metadata.MD
	}{
		{
			name:     "caller metadata preserved",
			callerMD: []string{"authorization", "Bearer token"},
			params:   a2aclient.ServiceParams{a2a.SvcParamExtensions: {"uri1"}},
			want: metadata.MD{
				"authorization":    {"Bearer token"},
				"x-a2a-extensions": {"uri1"},
			},
		},
		{
			name:     "service params override conflicting caller keys",
			callerMD: []string{"a2a-version", "stale", "x-a2a-extensions", "stale"},
			params:   a2aclient.ServiceParams{a2a.SvcParamVersion: {"0.3"}, a2a.SvcParamExtensions: {"uri1"}},
			want: metadata.MD{
				"a2a-version":      {"0.3"},
				"x-a2a-extensions": {"uri1"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := metadata.AppendToOutgoingContext(context.Background(), tc.callerMD...)

			got, _ := metadata.FromOutgoingContext(withGRPCMetadata(ctx, tc.params))
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("withGRPCMetadata() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestCompat_ToTrailer(t *testing.T) {
	legacyKey := "x-" + strings.ToLower(a2a.SvcParamExtensions)

	ctx := context.Background()
	svcParams := a2asrv.NewServiceParams(map[string][]string{
		strings.ToLower(a2a.SvcParamExtensions): {"uri1"},
	})
	_, callCtx := a2asrv.NewCallContext(ctx, svcParams)

	// Manually activate an extension to see it in trailers
	callCtx.Extensions().Activate(&a2a.AgentExtension{URI: "uri1"})

	got := toTrailer(callCtx)
	if !reflect.DeepEqual(got[legacyKey], []string{"uri1"}) {
		t.Errorf("expected trailer %s: %v, got %v", legacyKey, []string{"uri1"}, got[legacyKey])
	}
}

// errorMetadataServer fails every GetTask and SendStreamingMessage call with codes.Unauthenticated
// after setting response header and trailer metadata.
type errorMetadataServer struct {
	a2apb.UnimplementedA2AServiceServer
}

func (errorMetadataServer) GetTask(ctx context.Context, req *a2apb.GetTaskRequest) (*a2apb.Task, error) {
	if err := grpc.SetHeader(ctx, metadata.Pairs("www-authenticate", `Bearer resource_metadata="x"`)); err != nil {
		return nil, err
	}
	if err := grpc.SetTrailer(ctx, metadata.Pairs("x-trailer", "t")); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unauthenticated, "token expired")
}

func (errorMetadataServer) SendStreamingMessage(req *a2apb.SendMessageRequest, stream grpc.ServerStreamingServer[a2apb.StreamResponse]) error {
	if err := stream.SetHeader(metadata.Pairs("www-authenticate", `Bearer resource_metadata="x"`)); err != nil {
		return err
	}
	stream.SetTrailer(metadata.Pairs("x-trailer", "t"))
	return status.Error(codes.Unauthenticated, "token expired")
}

func TestGRPCTransport_ErrorServiceParams(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	a2apb.RegisterA2AServiceServer(s, errorMetadataServer{})
	go func() { _ = s.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		s.Stop()
		_ = conn.Close()
	})
	transport := NewGRPCTransport(conn)

	checkErr := func(op string, err error) {
		t.Helper()
		if !errors.Is(err, a2a.ErrUnauthenticated) {
			t.Fatalf("transport.%s() error = %v, want %v", op, err, a2a.ErrUnauthenticated)
		}
		var a2aErr *a2a.Error
		if !errors.As(err, &a2aErr) {
			t.Fatalf("transport.%s() error = %T, want *a2a.Error", op, err)
		}
		if diff := cmp.Diff([]string{`Bearer resource_metadata="x"`}, a2aErr.ServiceParams["www-authenticate"]); diff != "" {
			t.Errorf("transport.%s() ServiceParams[www-authenticate] wrong (-want +got):\n%s", op, diff)
		}
		if diff := cmp.Diff([]string{"t"}, a2aErr.ServiceParams["x-trailer"]); diff != "" {
			t.Errorf("transport.%s() ServiceParams[x-trailer] wrong (-want +got):\n%s", op, diff)
		}
	}

	_, err = transport.GetTask(t.Context(), a2aclient.ServiceParams{}, &a2a.GetTaskRequest{ID: "task-123"})
	checkErr("GetTask", err)

	var streamErr error
	for _, err := range transport.SendStreamingMessage(t.Context(), a2aclient.ServiceParams{}, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("test")),
	}) {
		if err != nil {
			streamErr = err
			break
		}
	}
	checkErr("SendStreamingMessage", streamErr)
}
