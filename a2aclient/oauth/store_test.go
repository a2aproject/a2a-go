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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestNewGrantKey(t *testing.T) {
	t.Parallel()
	got := NewGrantKey("https://login.example.com/", "c", "https://Agent.example.com/", []string{"b", "a", "b"})
	want := GrantKey{Issuer: "https://login.example.com", ClientID: "c", Resource: "https://agent.example.com", Scopes: []string{"a", "b"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("NewGrantKey() wrong result (-want +got) diff = %s", diff)
	}
	if got.ID() != NewGrantKey("https://login.example.com", "c", "https://agent.example.com", []string{"a", "b"}).ID() {
		t.Fatalf("GrantKey.ID() differs for equal keys")
	}
}

func TestMemoryStore(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]*MemoryStore{"NewMemoryStore": NewMemoryStore(), "zero value": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			k1 := NewGrantKey("https://login.example.com", "c", "https://a.example.com", []string{"x"})
			k2 := NewGrantKey("https://login.example.com", "c", "https://b.example.com", nil)
			expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

			if _, err := s.Get(ctx, k1); !errors.Is(err, ErrGrantNotFound) {
				t.Fatalf("MemoryStore.Get() on an empty store error = %v, want %v", err, ErrGrantNotFound)
			}
			for _, g := range []Grant{
				{Key: k1, AccessToken: "a1", RefreshToken: "r1", ExpiresAt: expires},
				{Key: k2, AccessToken: "b1"},
				{Key: k1, AccessToken: "a2", RefreshToken: "r2", ExpiresAt: expires},
			} {
				if err := s.Put(ctx, g); err != nil {
					t.Fatalf("MemoryStore.Put() error = %v", err)
				}
			}
			got, err := s.Get(ctx, k1)
			if err != nil {
				t.Fatalf("MemoryStore.Get() error = %v", err)
			}
			if diff := cmp.Diff(&Grant{Key: k1, AccessToken: "a2", RefreshToken: "r2", ExpiresAt: expires}, got); diff != "" {
				t.Fatalf("MemoryStore.Get() wrong result (-want +got) diff = %s", diff)
			}
			if err := s.Delete(ctx, k1); err != nil {
				t.Fatalf("MemoryStore.Delete() error = %v", err)
			}
			all, err := s.List(ctx)
			if err != nil {
				t.Fatalf("MemoryStore.List() error = %v", err)
			}
			if diff := cmp.Diff([]Grant{{Key: k2, AccessToken: "b1"}}, all); diff != "" {
				t.Fatalf("MemoryStore.List() wrong result (-want +got) diff = %s", diff)
			}
			if _, err := s.Get(ctx, k1); !errors.Is(err, ErrGrantNotFound) {
				t.Fatalf("MemoryStore.Get() of a deleted grant error = %v, want %v", err, ErrGrantNotFound)
			}
		})
	}
}

func TestMemoryStoreCopies(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := NewMemoryStore()
	newKey := func() GrantKey {
		return NewGrantKey("https://login.example.com", "c", "https://a.example.com", []string{"x", "y"})
	}
	key := newKey()
	g := Grant{Key: newKey(), GrantedScopes: []string{"x"}, AccessToken: "a1"}
	if err := s.Put(ctx, g); err != nil {
		t.Fatalf("MemoryStore.Put() error = %v", err)
	}
	g.Key.Scopes[0] = "changed"
	g.GrantedScopes[0] = "changed"

	got, err := s.Get(ctx, key)
	if err != nil || got == nil {
		t.Fatalf("MemoryStore.Get() = %v, %v, want the grant", got, err)
	}
	got.AccessToken = "changed"
	got.Key.Scopes[1] = "changed"
	got.GrantedScopes[0] = "changed"
	all, err := s.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("MemoryStore.List() = %v, %v, want one grant", all, err)
	}
	all[0].Key.Scopes[0] = "changed"

	got, err = s.Get(ctx, key)
	if err != nil {
		t.Fatalf("MemoryStore.Get() error = %v", err)
	}
	if diff := cmp.Diff(&Grant{Key: newKey(), GrantedScopes: []string{"x"}, AccessToken: "a1"}, got); diff != "" {
		t.Fatalf("MemoryStore.Get() after changing returned grants (-want +got) diff = %s", diff)
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := NewMemoryStore()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			key := NewGrantKey("https://login.example.com", "c", "https://a.example.com", []string{string(rune('a' + i))})
			if err := s.Put(ctx, Grant{Key: key}); err != nil {
				t.Errorf("MemoryStore.Put() error = %v", err)
			}
			if _, err := s.List(ctx); err != nil {
				t.Errorf("MemoryStore.List() error = %v", err)
			}
		})
	}
	wg.Wait()
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("MemoryStore.List() error = %v", err)
	}
	if len(all) != 20 {
		t.Fatalf("MemoryStore.List() = %d grants, want all 20", len(all))
	}
}

func TestMemoryStoreLock(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]*MemoryStore{"NewMemoryStore": NewMemoryStore(), "zero value": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			unlock, err := s.Lock(t.Context())
			if err != nil {
				t.Fatalf("MemoryStore.Lock() error = %v", err)
			}
			if err := s.Put(t.Context(), Grant{Key: NewGrantKey("https://login.example.com", "c", "", nil)}); err != nil {
				t.Fatalf("MemoryStore.Put() with the lock held error = %v", err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if _, err := s.Lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("MemoryStore.Lock() while held error = %v, want %v", err, context.DeadlineExceeded)
			}

			locked := make(chan func())
			go func() {
				unlock, err := s.Lock(t.Context())
				if err != nil {
					t.Errorf("MemoryStore.Lock() error = %v", err)
					unlock = func() {}
				}
				locked <- unlock
			}()
			select {
			case <-locked:
				t.Fatal("MemoryStore.Lock() returned while the lock was held")
			case <-time.After(20 * time.Millisecond):
			}
			unlock()
			select {
			case unlock := <-locked:
				unlock()
			case <-time.After(5 * time.Second):
				t.Fatal("MemoryStore.Lock() still waiting after the lock was released")
			}
		})
	}
}
