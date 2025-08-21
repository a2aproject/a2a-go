package a2aclient

import (
	"context"
	"errors"
	"sync"
)

// ErrCredentialNotFound is returned by CredentialsService if a credential for the provided
// (sessionId, scheme) pair was not found.
var ErrCredentialNotFound = errors.New("credential not found")

// SessionID is a client-generated identifier used for scoping auth credentials.
type SessionID string

// AuthCredential represents a security-scheme specific credential (eg. a JWT token).
type AuthCredential string

// CredentialsService is used by auth interceptor for resolving credentials.
type CredentialsService interface {
	Get(ctx context.Context, sid SessionID, scheme string) (AuthCredential, error)
}

// InMemoryCredentialsStore implements CredentialsService.
type InMemoryCredentialsStore struct {
	mu          sync.RWMutex
	credentials map[SessionID]map[string]AuthCredential
}

// AuthInterceptor implements CallInterceptor. It uses SessionID attached to the context.Context
// using a2aclient.WithSessionID to lookup and attach credentials according to the security
// scheme described in a2a.AgentCard.
// Credentials fetching is delegated to CredentialsService.
type AuthInterceptor struct {
	PassthroughCallInterceptor
	Service CredentialsService
}

// NewInMemoryCredentialsStore initializes an InMemoryCredentialsStore.
func NewInMemoryCredentialsStore() InMemoryCredentialsStore {
	return InMemoryCredentialsStore{
		credentials: make(map[SessionID]map[string]AuthCredential),
	}
}

func (s *InMemoryCredentialsStore) Get(ctx context.Context, sid SessionID, scheme string) (AuthCredential, error) {
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

func (s *InMemoryCredentialsStore) Set(sid SessionID, scheme string, credential AuthCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.credentials[sid]; !ok {
		s.credentials[sid] = make(map[string]AuthCredential)
	}
	s.credentials[sid][scheme] = credential
}
