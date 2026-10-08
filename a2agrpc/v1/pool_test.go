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
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func newBufconnServer(t *testing.T) (*grpc.Server, *bufconn.Listener, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go func() { _ = srv.Serve(lis) }()
	return srv, lis, func() { srv.Stop() }
}

func bufconnDialer(lis *bufconn.Listener) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
}

func bufconnOpts(lis *bufconn.Listener) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithContextDialer(bufconnDialer(lis)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
}

// newTestPool starts a bufconn server and returns a pool that dials it.
func newTestPool(t *testing.T, ttl time.Duration) *DefaultGRPCConnectionPool {
	t.Helper()
	_, lis, stop := newBufconnServer(t)
	t.Cleanup(stop)
	pool := NewDefaultGRPCConnectionPool(ttl, bufconnOpts(lis)...)
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func TestDefaultPool_AcquireCreatesConnection(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)

	conn, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}

	if n := pool.size(); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

func TestDefaultPool_ReusesConnection(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)

	conn1, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	_ = pool.Release(conn1)

	conn2, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	if conn1 != conn2 {
		t.Fatal("expected same connection to be reused")
	}

	if n := pool.size(); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

func TestDefaultPool_DifferentURLs(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)

	conn1, err := pool.Acquire(context.Background(), "agent-a")
	if err != nil {
		t.Fatalf("Acquire agent-a failed: %v", err)
	}
	defer func() { _ = pool.Release(conn1) }()

	conn2, err := pool.Acquire(context.Background(), "agent-b")
	if err != nil {
		t.Fatalf("Acquire agent-b failed: %v", err)
	}
	defer func() { _ = pool.Release(conn2) }()

	if conn1 == conn2 {
		t.Fatal("different URLs should not reuse the same connection")
	}

	if n := pool.size(); n != 2 {
		t.Fatalf("expected 2 pooled connections, got %d", n)
	}
}

func TestDefaultPool_TTLEviction(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 50*time.Millisecond)

	conn1, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	_ = pool.Release(conn1)

	if n := pool.size(); n != 1 {
		t.Fatalf("expected 1 pooled connection before TTL, got %d", n)
	}

	time.Sleep(100 * time.Millisecond)

	conn2, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire after TTL failed: %v", err)
	}

	if conn1 == conn2 {
		t.Fatal("expired connection should have been evicted")
	}

	if n := pool.size(); n != 1 {
		t.Fatalf("expected 1 pooled connection after eviction, got %d", n)
	}
}

func TestDefaultPool_ZeroTTLNeverEvicts(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 0)

	conn1, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	_ = pool.Release(conn1)

	time.Sleep(50 * time.Millisecond)

	conn2, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	if conn1 != conn2 {
		t.Fatal("zero TTL should never evict, expected same connection")
	}
}

func TestDefaultPool_Release(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)

	conn, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if err := pool.Release(conn); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}

func TestDefaultPool_ReleaseUnknownConnection(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)
	other := newTestPool(t, 1*time.Minute)

	conn, err := other.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if err := pool.Release(conn); err != nil {
		t.Fatalf("Release of a connection from another pool should be a no-op, got: %v", err)
	}
}

func TestDefaultPool_DoesNotEvictReferencedConnection(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 50*time.Millisecond)

	conn1, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	conn2, err := pool.Acquire(context.Background(), "bufnet")
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	if conn1 != conn2 {
		t.Fatal("a connection held by a caller must not be evicted, expected the same connection")
	}

	if n := pool.size(); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

func TestDefaultPool_ReferenceCount(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 50*time.Millisecond)
	ctx := context.Background()

	conn1, err := pool.Acquire(ctx, "bufnet")
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	conn2, err := pool.Acquire(ctx, "bufnet")
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}
	if conn1 != conn2 {
		t.Fatal("expected both callers to share one connection")
	}

	// One caller is still holding the connection, so it must outlive the TTL.
	_ = pool.Release(conn1)
	time.Sleep(100 * time.Millisecond)

	conn3, err := pool.Acquire(ctx, "bufnet")
	if err != nil {
		t.Fatalf("Acquire 3 failed: %v", err)
	}
	if conn1 != conn3 {
		t.Fatal("a connection with one outstanding reference must not be evicted")
	}

	// Once the last reference is dropped the TTL applies and it is evicted.
	_ = pool.Release(conn2)
	_ = pool.Release(conn3)
	time.Sleep(100 * time.Millisecond)

	conn4, err := pool.Acquire(ctx, "bufnet")
	if err != nil {
		t.Fatalf("Acquire 4 failed: %v", err)
	}
	if conn4 == conn1 {
		t.Fatal("an unreferenced connection idle past the TTL should have been evicted")
	}
}

func TestDefaultPool_Close(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t, 1*time.Minute)

	if _, err := pool.Acquire(context.Background(), "bufnet"); err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if err := pool.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if n := pool.size(); n != 0 {
		t.Fatalf("expected the pool to be empty after Close, got %d", n)
	}
}
