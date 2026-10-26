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

package a2aacp

import (
	"context"
	"sync"
)

// Session is the ACP session of an A2A context.
type Session struct {
	// ID is the ACP session ID.
	ID string
	// AgentKey is the key of the agent process which has the session in its
	// storage, see [Config.AgentKey].
	AgentKey string
}

// SessionStore maps A2A contexts to ACP sessions, so that every task in a
// context continues the same agent conversation. A persistent store lets
// contexts survive a restart when the agent supports session/load. In a
// cluster, a shared store also tells which agent key, and so which node,
// serves a context.
type SessionStore interface {
	// Get returns the ACP session of the A2A context, or false if there is none.
	Get(ctx context.Context, contextID string) (Session, bool, error)
	// Put records the ACP session of the A2A context.
	Put(ctx context.Context, contextID string, s Session) error
}

// memorySessions is a [SessionStore] which forgets everything on restart.
type memorySessions struct {
	mu sync.Mutex
	m  map[string]Session
}

var _ SessionStore = (*memorySessions)(nil)

func newMemorySessions() *memorySessions {
	return &memorySessions{m: map[string]Session{}}
}

func (s *memorySessions) Get(_ context.Context, contextID string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[contextID]
	return sess, ok, nil
}

func (s *memorySessions) Put(_ context.Context, contextID string, sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[contextID] = sess
	return nil
}
