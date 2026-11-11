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
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"google.golang.org/grpc"
)

// ErrConnCacheClosed is returned when a client is created with a [ConnCache] after [ConnCache.Close].
var ErrConnCacheClosed = errors.New("a2agrpc: connection cache is closed")

// ConnCache shares one [grpc.ClientConn] per agent URL between clients created with [WithCachedGRPCTransport].
// A connection is created on first use and stays open until [ConnCache.Close] is called. Destroying a client
// does not close the connection it used.
//
// A ConnCache is safe for concurrent use.
type ConnCache struct {
	opts []grpc.DialOption

	mu     sync.Mutex
	conns  map[string]*grpc.ClientConn
	closed bool
}

// NewConnCache creates a [ConnCache] which uses the provided [grpc.DialOption]s for every connection it creates.
func NewConnCache(opts ...grpc.DialOption) *ConnCache {
	return &ConnCache{opts: opts, conns: make(map[string]*grpc.ClientConn)}
}

// Close closes all cached connections, including the ones used by clients which were not destroyed yet.
// Creating a client with the cache after Close fails with [ErrConnCacheClosed].
func (c *ConnCache) Close() error {
	c.mu.Lock()
	conns := c.conns
	c.conns = nil
	c.closed = true
	c.mu.Unlock()

	var errs []error
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *ConnCache) get(url string) (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrConnCacheClosed
	}
	if conn, ok := c.conns[url]; ok {
		return conn, nil
	}
	// grpc.NewClient does not perform I/O, so it is cheap enough to call while holding the lock.
	conn, err := grpc.NewClient(url, c.opts...)
	if err != nil {
		return nil, err
	}
	c.conns[url] = conn
	return conn, nil
}

// WithCachedGRPCTransport creates a gRPC transport implementation which uses connections from the provided [ConnCache].
// Unlike [WithGRPCTransport], it does not open a new connection for every client, and destroying a client leaves
// the connection open for reuse.
func WithCachedGRPCTransport(cache *ConnCache) a2aclient.FactoryOption {
	return a2aclient.WithTransport(
		a2a.TransportProtocolGRPC,
		a2aclient.TransportFactoryFn(func(ctx context.Context, card *a2a.AgentCard, iface *a2a.AgentInterface) (a2aclient.Transport, error) {
			conn, err := cache.get(iface.URL)
			if err != nil {
				return nil, err
			}
			return NewGRPCTransportFromClient(a2apb.NewA2AServiceClient(conn)), nil
		}),
	)
}
