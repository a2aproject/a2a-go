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

package oauth

import (
	"context"
	"fmt"
	"sync"
)

// Store keeps grants, tokens included. Implementations must be safe for
// concurrent use and must not return memory that a later call modifies.
//
// Keys are compared with [GrantKey.Equal], or equivalently by [GrantKey.ID]. Put
// must copy what it keeps, Key.Scopes and GrantedScopes included, since the
// caller may reuse the grant. When the store also implements [Locker], or is
// used with a separate one, Get, Put and Delete may be called with that lock
// held, as when a grant is read, refreshed and written under it, so they must
// not take it themselves.
// Writes may receive a context that is detached from the caller's cancellation,
// so that a grant obtained just before the caller gave up is not lost.
type Store interface {
	// List returns every grant, in no particular order. It may leave
	// AccessToken and RefreshToken empty, so a store that keeps tokens in a
	// keychain need not read them all; callers that need the tokens call Get.
	List(ctx context.Context) ([]Grant, error)
	// Get returns the grant for key with its tokens, or an error wrapping
	// [ErrGrantNotFound] when there is none.
	Get(ctx context.Context, key GrantKey) (*Grant, error)
	// Put adds or replaces the grant with the same key.
	Put(ctx context.Context, g Grant) error
	// Delete removes the grant for key, if any.
	Delete(ctx context.Context, key GrantKey) error
}

// Locker is implemented by a [Store] that serializes the refreshes of its
// grants, such as a file in the user's home directory shared between processes.
// Its users hold the lock around reading, refreshing and writing a grant, so two
// of them don't both spend a refresh token the server rotates on use. Users of a
// Store that does not implement Locker can only lock within themselves, so two
// users sharing such a store may refresh the same grant at once.
type Locker interface {
	// Lock takes the lock, waiting until ctx is done, and returns the function
	// that releases it.
	Lock(ctx context.Context) (unlock func(), err error)
}

// MemoryStore is a [Store] and [Locker] that keeps grants in memory, for clients
// that obtain grants anew in every process. Its List returns the tokens too. The
// zero value is an empty store.
type MemoryStore struct {
	mu     sync.Mutex
	grants map[string]Grant
	sem    chan struct{}
}

var (
	_ Store  = (*MemoryStore)(nil)
	_ Locker = (*MemoryStore)(nil)
)

// NewMemoryStore returns an empty [MemoryStore].
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// List implements [Store].
func (s *MemoryStore) List(context.Context) ([]Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grants := make([]Grant, 0, len(s.grants))
	for _, g := range s.grants {
		grants = append(grants, *g.clone())
	}
	return grants, nil
}

// Get implements [Store].
func (s *MemoryStore) Get(_ context.Context, key GrantKey) (*Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[key.ID()]
	if !ok {
		return nil, ErrGrantNotFound
	}
	return g.clone(), nil
}

// Put implements [Store].
func (s *MemoryStore) Put(_ context.Context, g Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grants == nil {
		s.grants = map[string]Grant{}
	}
	s.grants[g.Key.ID()] = *g.clone()
	return nil
}

// Delete implements [Store].
func (s *MemoryStore) Delete(_ context.Context, key GrantKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.grants, key.ID())
	return nil
}

// Lock implements [Locker]. The lock is separate from the one that guards the
// grants, so the other methods can be called while it is held.
func (s *MemoryStore) Lock(ctx context.Context) (func(), error) {
	s.mu.Lock()
	if s.sem == nil {
		s.sem = make(chan struct{}, 1)
	}
	sem := s.sem
	s.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("locking grants: %w", ctx.Err())
	}
}
