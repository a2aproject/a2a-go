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
	"maps"
	"net"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/testutil/testexecutor"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// dialRecorder serves every address from one in-memory A2A server and counts dials per address.
type dialRecorder struct {
	lis *bufconn.Listener

	mu    sync.Mutex
	dials map[string]int
}

func startDialRecorder(t *testing.T) *dialRecorder {
	t.Helper()
	executor := testexecutor.FromEventGenerator(func(*a2asrv.ExecutorContext) []a2a.Event {
		return []a2a.Event{a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("pong"))}
	})
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	NewHandler(a2asrv.NewHandler(executor)).RegisterWith(s)
	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("Server exited with error: %v", err)
		}
	}()
	t.Cleanup(s.Stop)
	return &dialRecorder{lis: lis, dials: make(map[string]int)}
}

func (r *dialRecorder) newCache(t *testing.T) *ConnCache {
	t.Helper()
	cache := NewConnCache(
		grpc.WithContextDialer(func(_ context.Context, addr string) (net.Conn, error) {
			r.mu.Lock()
			r.dials[addr]++
			r.mu.Unlock()
			return r.lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Errorf("cache.Close() error = %v, want nil", err)
		}
	})
	return cache
}

func (r *dialRecorder) dialCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.dials)
}

func newCard(addr string) *a2a.AgentCard {
	return &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface("passthrough:///"+addr, a2a.TransportProtocolGRPC),
		},
	}
}

func sendPing(ctx context.Context, client *a2aclient.Client) error {
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping"))
	_, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	return err
}

func TestWithCachedGRPCTransport(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		clientFor []string
		wantDials map[string]int
	}{
		{
			name:      "clients of one agent share a connection",
			clientFor: []string{"agent-a", "agent-a", "agent-a"},
			wantDials: map[string]int{"agent-a": 1},
		},
		{
			name:      "every agent gets its own connection",
			clientFor: []string{"agent-a", "agent-b", "agent-a", "agent-b"},
			wantDials: map[string]int{"agent-a": 1, "agent-b": 1},
		},
		{
			name:      "concurrent clients of one agent share a connection",
			clientFor: []string{"agent-a", "agent-a", "agent-a", "agent-a", "agent-a", "agent-a", "agent-a", "agent-a"},
			wantDials: map[string]int{"agent-a": 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			recorder := startDialRecorder(t)
			cache := recorder.newCache(t)

			var group errgroup.Group
			for _, addr := range tc.clientFor {
				group.Go(func() error {
					client, err := a2aclient.NewFromCard(ctx, newCard(addr), WithCachedGRPCTransport(cache))
					if err != nil {
						return err
					}
					if err := sendPing(ctx, client); err != nil {
						return err
					}
					return client.Destroy()
				})
			}
			if err := group.Wait(); err != nil {
				t.Fatalf("client.SendMessage() error = %v, want nil", err)
			}

			if diff := cmp.Diff(tc.wantDials, recorder.dialCounts()); diff != "" {
				t.Fatalf("dial counts wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestWithCachedGRPCTransport_DestroyKeepsConnectionOpen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	recorder := startDialRecorder(t)
	cache := recorder.newCache(t)
	card := newCard("agent-a")

	destroyed, err := a2aclient.NewFromCard(ctx, card, WithCachedGRPCTransport(cache))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
	}
	remaining, err := a2aclient.NewFromCard(ctx, card, WithCachedGRPCTransport(cache))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
	}
	if err := sendPing(ctx, destroyed); err != nil {
		t.Fatalf("client.SendMessage() error = %v, want nil", err)
	}
	if err := destroyed.Destroy(); err != nil {
		t.Fatalf("client.Destroy() error = %v, want nil", err)
	}
	if err := destroyed.Destroy(); err != nil {
		t.Fatalf("client.Destroy() error = %v, want nil", err)
	}

	if err := sendPing(ctx, remaining); err != nil {
		t.Fatalf("client.SendMessage() error = %v, want nil", err)
	}
	if diff := cmp.Diff(map[string]int{"agent-a": 1}, recorder.dialCounts()); diff != "" {
		t.Fatalf("dial counts wrong result (-want +got) diff = %s", diff)
	}
}

func TestConnCache_Close(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	recorder := startDialRecorder(t)
	cache := recorder.newCache(t)
	card := newCard("agent-a")

	client, err := a2aclient.NewFromCard(ctx, card, WithCachedGRPCTransport(cache))
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v, want nil", err)
	}
	if err := sendPing(ctx, client); err != nil {
		t.Fatalf("client.SendMessage() error = %v, want nil", err)
	}

	if err := cache.Close(); err != nil {
		t.Fatalf("cache.Close() error = %v, want nil", err)
	}

	if err := sendPing(ctx, client); err == nil {
		t.Fatalf("client.SendMessage() error = nil, want error after cache.Close()")
	}
	if _, err := a2aclient.NewFromCard(ctx, card, WithCachedGRPCTransport(cache)); !errors.Is(err, ErrConnCacheClosed) {
		t.Fatalf("a2aclient.NewFromCard() error = %v, want %v", err, ErrConnCacheClosed)
	}
}
