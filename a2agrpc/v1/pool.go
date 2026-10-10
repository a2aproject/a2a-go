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
	"log/slog"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/log"
	"google.golang.org/grpc"
)

// ConnectionPool manages reusable gRPC connections keyed by URL.
// gRPC ClientConns are long-lived and expensive to recreate — a pool
// avoids the hidden performance cost of creating a new connection on
// every client creation.
type ConnectionPool interface {
	// Acquire returns a connection for url, creating it when it is not pooled
	// yet, and takes a reference on it. Every successful Acquire must be
	// paired with a Release.
	Acquire(ctx context.Context, url string) (*grpc.ClientConn, error)

	// Release drops the reference taken by Acquire. The caller must not
	// close the connection: it stays pooled until it has been unreferenced
	// for longer than the pool's TTL.
	Release(*grpc.ClientConn) error
}

// DefaultGRPCConnectionPool is a simple in-memory pool that reuses
// gRPC connections by URL. Connections are reference counted and only become
// evictable once their last reference is released; an unreferenced connection
// idle for longer than ttl is closed and removed on the next Acquire. A zero
// or negative ttl disables eviction (connections live forever).
type DefaultGRPCConnectionPool struct {
	mu       sync.Mutex
	conns    map[string]*pooledConn
	ttl      time.Duration
	dialOpts []grpc.DialOption
}

type pooledConn struct {
	conn *grpc.ClientConn
	// refs is the number of callers currently using the connection. It is
	// never lower than 0, and idleSince is only set when it reaches 0.
	refs int
	// idleSince is the moment the last reference was released. It is the
	// zero time while the connection is still referenced.
	idleSince time.Time
}

// NewDefaultGRPCConnectionPool creates a pool with the given idle TTL, dialing
// every connection it creates with the provided options.
//
// The options belong to the pool rather than to Acquire because they are not
// part of the pool key: a URL is pooled once, so two callers asking for the same
// URL with different options would silently share whichever connection was
// created first.
func NewDefaultGRPCConnectionPool(ttl time.Duration, opts ...grpc.DialOption) *DefaultGRPCConnectionPool {
	return &DefaultGRPCConnectionPool{
		conns:    make(map[string]*pooledConn),
		ttl:      ttl,
		dialOpts: opts,
	}
}

// Acquire returns the pooled connection for url, dialing one when the URL is not
// pooled yet, and takes a reference on the result. Unreferenced connections idle
// for longer than the TTL are evicted first.
func (p *DefaultGRPCConnectionPool) Acquire(ctx context.Context, url string) (*grpc.ClientConn, error) {
	p.evictIdle(ctx)

	if pc := p.reference(url); pc != nil {
		return pc.conn, nil
	}

	// Dialing is deliberately outside the critical section: it can take
	// arbitrarily long and must not block callers of other URLs.
	conn, err := grpc.NewClient(url, p.dialOpts...)
	if err != nil {
		return nil, err
	}
	return p.pool(ctx, url, conn), nil
}

// Release drops the reference Acquire took. The connection is not closed here;
// dropping the last reference only arms its TTL, and the pool closes it once it
// has been idle for longer than the TTL.
func (p *DefaultGRPCConnectionPool) Release(conn *grpc.ClientConn) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.conns {
		if pc.conn != conn {
			continue
		}
		if pc.refs > 0 {
			pc.refs--
		}
		if pc.refs == 0 {
			pc.idleSince = time.Now()
		}
		return nil
	}
	return nil
}

// Close closes all pooled connections, including referenced ones. It is meant
// for shutdown; the pool is left empty and can be used again afterwards.
func (p *DefaultGRPCConnectionPool) Close() error {
	p.mu.Lock()
	conns := p.conns
	p.conns = make(map[string]*pooledConn)
	p.mu.Unlock()

	errs := make([]error, 0, len(conns))
	for _, pc := range conns {
		if err := pc.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *DefaultGRPCConnectionPool) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// reference takes a reference on the pooled connection for url, if there is one.
func (p *DefaultGRPCConnectionPool) reference(url string) *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()

	pc, ok := p.conns[url]
	if !ok {
		return nil
	}
	pc.refs++
	return pc
}

// pool stores a freshly dialed connection, or discards it in favor of one that
// another caller pooled for the same URL while this one was dialing.
func (p *DefaultGRPCConnectionPool) pool(ctx context.Context, url string, conn *grpc.ClientConn) *grpc.ClientConn {
	p.mu.Lock()
	if pc, ok := p.conns[url]; ok {
		pc.refs++
		p.mu.Unlock()

		if err := conn.Close(); err != nil {
			log.Error(ctx, "grpc connection pool: closing redundant connection", err, slog.String("url", url))
		}
		return pc.conn
	}
	p.conns[url] = &pooledConn{conn: conn, refs: 1}
	p.mu.Unlock()
	return conn
}

// evictIdle closes and removes the connections whose last reference was released
// more than the TTL ago. A connection that is still referenced is never evicted,
// however long its caller holds it.
func (p *DefaultGRPCConnectionPool) evictIdle(ctx context.Context) {
	if p.ttl <= 0 {
		return
	}
	for _, pc := range p.takeIdle(time.Now()) {
		if err := pc.conn.Close(); err != nil {
			log.Error(ctx, "grpc connection pool: closing idle connection", err)
		}
	}
}

// takeIdle removes and returns the idle connections, leaving the close calls to
// the caller so that they happen outside the critical section.
func (p *DefaultGRPCConnectionPool) takeIdle(now time.Time) []*pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()

	var idle []*pooledConn
	for url, pc := range p.conns {
		if pc.refs > 0 || now.Sub(pc.idleSince) <= p.ttl {
			continue
		}
		idle = append(idle, pc)
		delete(p.conns, url)
	}
	return idle
}

// WithPooledGRPCTransport creates a gRPC transport backed by a connection pool.
// Each client creation acquires a connection from the pool, and releasing the
// transport returns the connection to the pool instead of closing it.
func WithPooledGRPCTransport(pool ConnectionPool) a2aclient.FactoryOption {
	return a2aclient.WithTransport(
		a2a.TransportProtocolGRPC,
		a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
			conn, err := pool.Acquire(ctx, iface.URL)
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
