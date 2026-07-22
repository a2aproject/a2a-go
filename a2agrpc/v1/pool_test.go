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

func TestDefaultPool_AcquireCreatesConnection(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(1 * time.Minute)
	defer pool.Close()

	// Start a local gRPC server
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := pool.Acquire(context.Background(), "bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}

	if n := pool.Len(); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

func TestDefaultPool_ReusesConnection(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(1 * time.Minute)
	defer pool.Close()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	opts := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}

	conn1, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	pool.Release(conn1)

	conn2, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	// Same underlying connection should be reused
	if conn1 != conn2 {
		t.Fatal("expected same connection to be reused")
	}

	if n := pool.Len(); n != 1 {
		t.Fatalf("expected 1 pooled connection, got %d", n)
	}
}

func TestDefaultPool_DifferentURLs(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(1 * time.Minute)
	defer pool.Close()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	opts := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}

	conn1, err := pool.Acquire(context.Background(), "agent-a", opts...)
	if err != nil {
		t.Fatalf("Acquire agent-a failed: %v", err)
	}
	defer pool.Release(conn1)

	conn2, err := pool.Acquire(context.Background(), "agent-b", opts...)
	if err != nil {
		t.Fatalf("Acquire agent-b failed: %v", err)
	}
	defer pool.Release(conn2)

	if conn1 == conn2 {
		t.Fatal("different URLs should not reuse the same connection")
	}

	if n := pool.Len(); n != 2 {
		t.Fatalf("expected 2 pooled connections, got %d", n)
	}
}

func TestDefaultPool_TTLEviction(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(50 * time.Millisecond)
	defer pool.Close()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	opts := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}

	conn1, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	pool.Release(conn1)

	if n := pool.Len(); n != 1 {
		t.Fatalf("expected 1 pooled connection before TTL, got %d", n)
	}

	time.Sleep(100 * time.Millisecond)

	// Next Acquire should evict the expired connection and create a new one
	conn2, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire after TTL failed: %v", err)
	}

	if conn1 == conn2 {
		t.Fatal("expired connection should have been evicted")
	}

	if n := pool.Len(); n != 1 {
		t.Fatalf("expected 1 pooled connection after eviction, got %d", n)
	}
}

func TestDefaultPool_ZeroTTLNeverEvicts(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(0)
	defer pool.Close()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	opts := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}

	conn1, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	pool.Release(conn1)

	time.Sleep(50 * time.Millisecond)

	conn2, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	if conn1 != conn2 {
		t.Fatal("zero TTL should never evict, expected same connection")
	}
}

func TestDefaultPool_Release(t *testing.T) {
	pool := NewDefaultGRPCConnectionPool(1 * time.Minute)
	defer pool.Close()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	go srv.Serve(lis)
	defer srv.Stop()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	opts := []grpc.DialOption{grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials())}

	conn, err := pool.Acquire(context.Background(), "bufnet", opts...)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release should succeed even for a connection not in the map (e.g., externally provided)
	if err := pool.Release(conn); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}
