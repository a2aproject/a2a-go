// Copyright 2025 The A2A Authors
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

package a2aclient

import (
	"context"
	"errors"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
)

// ErrCredentialNotFound is returned by CredentialsService if a credential for the provided
// (sessionId, scheme) pair was not found.
var ErrCredentialNotFound = errors.New("credential not found")

// SessionID is a client-generated identifier used for scoping auth credentials.
type SessionID string

// AuthCredential represents a security-scheme specific credential (eg. a JWT token).
type AuthCredential string

// AuthInterceptor implements CallInterceptor.
// It uses SessionID provided using a2aclient.WithSessionID to lookup credentials according
// and attach them to the according to the security scheme described in a2a.AgentCard.
// Credentials fetching is delegated to CredentialsService.
type AuthInterceptor struct {
	PassthroughInterceptor
	Service CredentialsService
}

// CredentialsService is used by auth interceptor for resolving credentials.
type CredentialsService interface {
	Get(ctx context.Context, sid SessionID, scheme string) (AuthCredential, error)
}

type SessionCredentials map[a2a.SecuritySchemeName]AuthCredential

// InMemoryCredentialsStore implements CredentialsService.
type InMemoryCredentialsStore struct {
	mu          sync.RWMutex
	credentials map[SessionID]SessionCredentials
}

// NewInMemoryCredentialsStore initializes an InMemoryCredentialsStore.
func NewInMemoryCredentialsStore() InMemoryCredentialsStore {
	return InMemoryCredentialsStore{
		credentials: make(map[SessionID]SessionCredentials),
	}
}

func (s *InMemoryCredentialsStore) Get(ctx context.Context, sid SessionID, scheme a2a.SecuritySchemeName) (AuthCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	forSession, ok := s.credentials[sid]
	if !ok {
		return AuthCredential(""), ErrCredentialNotFound
	}

	credential, ok := forSession[scheme]
	if !ok {
		return AuthCredential(""), ErrCredentialNotFound
	}

	return credential, nil
}

func (s *InMemoryCredentialsStore) Set(sid SessionID, scheme a2a.SecuritySchemeName, credential AuthCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[sid]; !ok {
		s.credentials[sid] = make(map[a2a.SecuritySchemeName]AuthCredential)
	}
	s.credentials[sid][scheme] = credential
}
