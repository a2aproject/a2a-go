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

package push

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/utils"
	"github.com/google/uuid"
)

// ErrPushConfigNotFound indicates that a push config with the provided ID was not found.
var ErrPushConfigNotFound = errors.New("push config not found")

// InMemoryPushConfigStore implements a2asrv.PushConfigStore.
// Authorization (cross-tenant isolation) is enforced at the handler level
// via taskStore.Get; this store is a plain in-memory CRUD store.
type InMemoryPushConfigStore struct {
	mu      sync.RWMutex
	configs map[a2a.TaskID]map[string]*a2a.PushConfig
}

// NewInMemoryStore creates an empty store.
func NewInMemoryStore() *InMemoryPushConfigStore {
	return &InMemoryPushConfigStore{
		configs: make(map[a2a.TaskID]map[string]*a2a.PushConfig),
	}
}

// newID creates a time-based random ID.
func newID() string {
	return uuid.Must(uuid.NewV7()).String()
}

func validateConfig(config *a2a.PushConfig) error {
	if config == nil {
		return errors.New("push config cannot be nil")
	}
	if config.URL == "" {
		return errors.New("push config endpoint cannot be empty")
	}
	if _, err := url.ParseRequestURI(config.URL); err != nil {
		return fmt.Errorf("invalid push config endpoint URL: %w", err)
	}
	return nil
}

// Save adds a copy of push config to the store.
func (s *InMemoryPushConfigStore) Save(ctx context.Context, taskID a2a.TaskID, config *a2a.PushConfig) (*a2a.PushConfig, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("%w: %w", a2a.ErrInvalidParams, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	toSave, err := utils.DeepCopy(config)
	if err != nil {
		return nil, err
	}

	if toSave.ID == "" {
		toSave.ID = newID()
	}
	toSave.TaskID = taskID

	if _, ok := s.configs[taskID]; !ok {
		s.configs[taskID] = make(map[string]*a2a.PushConfig)
	}
	s.configs[taskID][toSave.ID] = toSave

	savedCopy, err := utils.DeepCopy(toSave)
	if err != nil {
		return nil, err
	}

	return savedCopy, nil
}

// Get returns a copy of stored config.
func (s *InMemoryPushConfigStore) Get(ctx context.Context, taskID a2a.TaskID, configID string) (*a2a.PushConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if configs, ok := s.configs[taskID]; ok {
		if config, ok := configs[configID]; ok {
			return utils.DeepCopy(config)
		}
	}

	return nil, ErrPushConfigNotFound
}

// List returns a copy of stored configs for a task, applying offset-based
// pagination when pageSize > 0. Configs are ordered by ID so pages are
// deterministic. The next page token is empty when no further pages exist.
func (s *InMemoryPushConfigStore) List(ctx context.Context, taskID a2a.TaskID, pageSize int, pageToken string) ([]*a2a.PushConfig, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs, ok := s.configs[taskID]
	if !ok {
		return []*a2a.PushConfig{}, "", nil
	}

	// Sort IDs for a deterministic ordering across pages.
	ids := make([]string, 0, len(configs))
	for id := range configs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	offset := 0
	if pageToken != "" {
		decodedOffset, err := decodePushConfigPageToken(pageToken)
		if err != nil {
			return nil, "", err
		}
		offset = decodedOffset
	}
	if offset < 0 || offset >= len(ids) {
		// Token is past the end: an empty page with no further pages.
		return []*a2a.PushConfig{}, "", nil
	}

	end := len(ids)
	if pageSize > 0 && offset+pageSize < end {
		end = offset + pageSize
	}

	result := make([]*a2a.PushConfig, 0, end-offset)
	for _, id := range ids[offset:end] {
		cp, err := utils.DeepCopy(configs[id])
		if err != nil {
			return nil, "", err
		}
		result = append(result, cp)
	}

	nextPageToken := ""
	if pageSize > 0 && end < len(ids) {
		nextPageToken = encodePushConfigPageToken(end)
	}
	return result, nextPageToken, nil
}

// encodePushConfigPageToken encodes a page offset as an opaque, URL-safe token
// (base64 of the decimal offset), matching the opaque-token style used by task
// pagination.
func encodePushConfigPageToken(offset int) string {
	return base64.URLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// decodePushConfigPageToken decodes an offset-based page token. Invalid tokens
// yield [a2a.ErrParseError].
func decodePushConfigPageToken(token string) (int, error) {
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, a2a.ErrParseError
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, a2a.ErrParseError
	}
	return offset, nil
}

// Delete removes a single config from a task.
func (s *InMemoryPushConfigStore) Delete(ctx context.Context, taskID a2a.TaskID, configID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if configs, ok := s.configs[taskID]; ok {
		delete(configs, configID)
	}
	return nil
}

// DeleteAll removes all stored configs for a task.
func (s *InMemoryPushConfigStore) DeleteAll(ctx context.Context, taskID a2a.TaskID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.configs, taskID)
	return nil
}
