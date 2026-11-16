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

// Store keeps grants, tokens included, by [Grant.ID]. Implementations must be
// safe for concurrent use and must not return memory that a later call
// modifies.
//
// Put must copy what it keeps, Scopes and GrantedScopes included, since the
// caller may reuse the grant. When its users serialize their writes with a
// [Locker], as a [Manager] given one does, Get, Put and Delete may be called
// with that lock held, as when a grant is read, refreshed and written under
// it, so they must not take it themselves. Writes may receive a context that
// is detached from the caller's cancellation, so that a grant obtained just
// before the caller gave up is not lost.
type Store interface {
	// List returns every grant, in no particular order. It may leave
	// AccessToken and RefreshToken empty, so a store that keeps tokens in a
	// keychain need not read them all; callers that need the tokens call Get.
	List(ctx context.Context) ([]Grant, error)
	// Get returns the grant with the given ID, tokens included, or an error
	// wrapping [ErrGrantNotFound] when there is none.
	Get(ctx context.Context, id string) (*Grant, error)
	// Put adds the grant, or replaces the one with the same ID.
	Put(ctx context.Context, g Grant) error
	// Delete removes the grant with the given ID, if any.
	Delete(ctx context.Context, id string) error
}

// Locker serializes the refreshes and writes of grants between the users of a
// shared [Store], such as processes sharing a file in the user's home directory.
// They hold the lock around reading, refreshing and writing a grant, so two of
// them don't both spend a refresh token the server rotates on use. Give it to a
// [Manager] as Manager.Locker; without one, a Manager only locks within itself.
type Locker interface {
	// Lock takes the lock, waiting until ctx is done, and returns the function
	// that releases it.
	Lock(ctx context.Context) (unlock func(), err error)
}

// MemoryStore is a [Store] that keeps grants in memory, for clients that obtain
// grants anew in every process. Its List returns the tokens too. It is also a
// [Locker], for its users in one process, such as several Managers sharing it.
// The zero value is an empty store.
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
func (s *MemoryStore) Get(_ context.Context, id string) (*Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[id]
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
	s.grants[g.ID()] = *g.clone()
	return nil
}

// Delete implements [Store].
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.grants, id)
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
