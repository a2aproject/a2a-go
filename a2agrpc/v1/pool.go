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
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"google.golang.org/grpc"
)

// GRPCConnectionPool manages reusable gRPC connections keyed by URL.
// gRPC ClientConns are long-lived and expensive to recreate — a pool
// avoids the hidden performance cost of creating a new connection on
// every client creation.
type GRPCConnectionPool interface {
	// Acquire returns an existing or newly created connection for url.
	Acquire(ctx context.Context, url string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

	// Release returns a connection to the pool. The caller must not
	// close the connection after releasing it.
	Release(*grpc.ClientConn) error
}

// DefaultGRPCConnectionPool is a simple in-memory pool that reuses
// gRPC connections by URL. Connections idle longer than ttl are
// closed and removed on the next Acquire. A zero or negative ttl
// disables eviction (connections live forever).
type DefaultGRPCConnectionPool struct {
	mu    sync.Mutex
	conns map[string]*pooledConn
	ttl   time.Duration
}

type pooledConn struct {
	conn     *grpc.ClientConn
	lastUsed time.Time
}

// NewDefaultGRPCConnectionPool creates a pool with the given idle TTL.
func NewDefaultGRPCConnectionPool(ttl time.Duration) *DefaultGRPCConnectionPool {
	return &DefaultGRPCConnectionPool{
		conns: make(map[string]*pooledConn),
		ttl:   ttl,
	}
}

// Acquire returns a cached connection for url or creates one with the
// provided dial options. Expired connections are evicted before lookup.
func (p *DefaultGRPCConnectionPool) Acquire(ctx context.Context, url string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.evictExpired()

	if pc, ok := p.conns[url]; ok {
		pc.lastUsed = time.Now()
		return pc.conn, nil
	}

	conn, err := grpc.NewClient(url, opts...)
	if err != nil {
		return nil, err
	}

	p.conns[url] = &pooledConn{conn: conn, lastUsed: time.Now()}
	return conn, nil
}

// Release returns a connection to the pool. The connection is NOT closed.
func (p *DefaultGRPCConnectionPool) Release(conn *grpc.ClientConn) error {
	// Connections are kept in the pool until evicted by TTL.
	// On release we just update the timestamp so the TTL clock resets.
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.conns {
		if pc.conn == conn {
			pc.lastUsed = time.Now()
			return nil
		}
	}
	return nil
}

// Close closes all pooled connections.
func (p *DefaultGRPCConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for url, pc := range p.conns {
		_ = pc.conn.Close()
		delete(p.conns, url)
	}
	return nil
}

// Len returns the current number of pooled connections.
func (p *DefaultGRPCConnectionPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

func (p *DefaultGRPCConnectionPool) evictExpired() {
	if p.ttl <= 0 {
		return
	}
	now := time.Now()
	for url, pc := range p.conns {
		if now.Sub(pc.lastUsed) > p.ttl {
			_ = pc.conn.Close()
			delete(p.conns, url)
		}
	}
}

// WithPooledGRPCTransport creates a gRPC transport backed by a connection pool.
// Each client creation acquires a connection from the pool, and releasing the
// transport returns the connection to the pool instead of closing it.
func WithPooledGRPCTransport(pool GRPCConnectionPool, opts ...grpc.DialOption) a2aclient.FactoryOption {
	return a2aclient.WithTransport(
		a2a.TransportProtocolGRPC,
		a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
			conn, err := pool.Acquire(ctx, iface.URL, opts...)
			if err != nil {
				return nil, err
			}
			return &grpcTransport{
				client:      a2apb.NewA2AServiceClient(conn),
				closeConnFn: func() error { return pool.Release(conn) },
			}, nil
		}),
	)
}
