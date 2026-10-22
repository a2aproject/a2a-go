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

// SessionStore maps A2A contexts to ACP sessions, so that every task in a
// context continues the same agent conversation. A persistent store lets
// contexts survive a restart when the agent supports session/load.
type SessionStore interface {
	// Get returns the ACP session of the A2A context, or false if there is none.
	Get(ctx context.Context, contextID string) (sessionID string, ok bool, err error)
	// Put records the ACP session of the A2A context.
	Put(ctx context.Context, contextID, sessionID string) error
}

// memorySessions is a [SessionStore] which forgets everything on restart.
type memorySessions struct {
	mu sync.Mutex
	m  map[string]string
}

var _ SessionStore = (*memorySessions)(nil)

func newMemorySessions() *memorySessions {
	return &memorySessions{m: map[string]string{}}
}

func (s *memorySessions) Get(_ context.Context, contextID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.m[contextID]
	return id, ok, nil
}

func (s *memorySessions) Put(_ context.Context, contextID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[contextID] = sessionID
	return nil
}
