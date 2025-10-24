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

package pushconfig

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestInMemoryPushConfigStore_Save(t *testing.T) {
	ctx := context.Background()
	taskID := a2a.TaskID("task")

	testCases := []struct {
		name    string
		config  *a2a.PushConfig
		wantErr error
	}{
		{
			name: "valid config with no id",
			config: &a2a.PushConfig{
				URL: "https://example.com/push",
			},
			wantErr: nil,
		},
		{
			name: "valid config with id",
			config: &a2a.PushConfig{
				ID:  newID(),
				URL: "https://example.com/push",
			},
			wantErr: nil,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: errors.New("push config cannot be nil"),
		},
		{
			name:    "empty URL",
			config:  &a2a.PushConfig{},
			wantErr: errors.New("push config endpoint cannot be empty"),
		},
		{
			name:    "invalid URL",
			config:  &a2a.PushConfig{URL: "not a url"},
			wantErr: errors.New("invalid push config endpoint URL: parse \"not a url\": invalid URI for request"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			err := store.Save(ctx, taskID, tc.config)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Save() failed: %v", err)
				}
				storedConfigs, err := store.Get(ctx, taskID)
				if err != nil {
					t.Fatalf("Get() failed: %v", err)
				}
				if len(storedConfigs) != 1 {
					t.Fatalf("Get() returned %d configs, want 1 for task %s", len(storedConfigs), taskID)
				}
				if diff := cmp.Diff(tc.config, storedConfigs[0]); diff != "" {
					t.Errorf("Stored config mismatch (-want +got):\n%s", diff)
				}
			} else {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("Save() error = %v, want %v", err, tc.wantErr)
				}
			}
		})
	}
}

func TestInMemoryPushConfigStore_ModifiedConfig(t *testing.T) {
	ctx := context.Background()
	taskID := a2a.TaskID("test-task")

	t.Run("modify original config after save", func(t *testing.T) {
		store := NewInMemoryStore()
		originalConfig := &a2a.PushConfig{ID: newID(), URL: "https://original.com"}
		err := store.Save(ctx, taskID, originalConfig)
		if err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
		savedID := originalConfig.ID
		savedURL := originalConfig.URL

		originalConfig.URL = "https://modified-original.com"
		originalConfig.ID = "new-id-for-original"

		storedConfigs, err := store.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		if len(storedConfigs) != 1 {
			t.Fatalf("Expected 1 config, got %d", len(storedConfigs))
		}
		retrievedConfig := storedConfigs[0]
		wantConfig := &a2a.PushConfig{ID: savedID, URL: savedURL}
		if diff := cmp.Diff(wantConfig, retrievedConfig); diff != "" {
			t.Errorf("Retrieved config mismatch after modifying original (-want +got):\n%s", diff)
		}
	})

	t.Run("modify retrieved config after get", func(t *testing.T) {
		store := NewInMemoryStore()
		initialConfig := &a2a.PushConfig{ID: newID(), URL: "https://initial-get.com"}
		err := store.Save(ctx, taskID, initialConfig)
		if err != nil {
			t.Fatalf("Save() failed: %v", err)
		}

		retrievedConfigs, err := store.Get(ctx, taskID)
		if err != nil || len(retrievedConfigs) != 1 {
			t.Fatalf("First Get() failed or returned unexpected count: %v, count %d", err, len(retrievedConfigs))
		}
		firstRetrievedConfig := retrievedConfigs[0]
		firstRetrievedConfig.URL = "https://modified-retrieved.com"
		firstRetrievedConfig.ID = "new-id-for-retrieved"
		secondRetrievedConfigs, err := store.Get(ctx, taskID)
		if err != nil || len(secondRetrievedConfigs) != 1 {
			t.Fatalf("Second Get() failed or returned unexpected count: %v, count %d", err, len(secondRetrievedConfigs))
		}
		secondRetrievedConfig := secondRetrievedConfigs[0]
		if diff := cmp.Diff(initialConfig, secondRetrievedConfig); diff != "" {
			t.Errorf("Second retrieved config mismatch after modifying first retrieved (-want +got):\n%s", diff)
		}
	})
}

func TestInMemoryPushConfigStore_Get(t *testing.T) {
	ctx := context.Background()
	taskID := a2a.TaskID("task")
	config := &a2a.PushConfig{ID: newID(), URL: "https://example.com/push1"}
	anotherTaskID := a2a.TaskID("another-task")
	anotherConfig := &a2a.PushConfig{ID: newID(), URL: "https://example.com/push2"}

	store := NewInMemoryStore()
	_ = store.Save(ctx, taskID, config)
	_ = store.Save(ctx, anotherTaskID, anotherConfig)

	testCases := []struct {
		name   string
		taskID a2a.TaskID
		want   []*a2a.PushConfig
	}{
		{
			name:   "existing task",
			taskID: taskID,
			want:   []*a2a.PushConfig{config},
		},
		{
			name:   "non-existent task",
			taskID: a2a.TaskID("non-existent"),
			want:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configs, err := store.Get(ctx, tc.taskID)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}
			if diff := cmp.Diff(tc.want, configs); diff != "" {
				t.Errorf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", configs, tc.want, diff)
			}
		})
	}
}

func TestInMemoryPushConfigStore_Delete(t *testing.T) {
	ctx := context.Background()
	taskID := a2a.TaskID("task")
	configs := []*a2a.PushConfig{
		{ID: newID(), URL: "https://example.com/push1"},
		{ID: newID(), URL: "https://example.com/push2"},
	}

	testCases := []struct {
		name     string
		taskID   a2a.TaskID
		configID string
		want     []*a2a.PushConfig
	}{
		{
			name:     "existing config",
			taskID:   taskID,
			configID: configs[0].ID,
			want:     []*a2a.PushConfig{configs[1]},
		},
		{
			name:     "non-existent config",
			taskID:   taskID,
			configID: newID(),
			want:     configs,
		},
		{
			name:   "non-existent task",
			taskID: a2a.TaskID("non-existent"),
			want:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			for _, config := range configs {
				_ = store.Save(ctx, taskID, config)
			}

			err := store.Delete(ctx, tc.taskID, tc.configID)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}
			got, err := store.Get(ctx, tc.taskID)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, tc.want, diff)
			}
		})
	}
}

func TestInMemoryPushConfigStore_DeleteAll(t *testing.T) {
	ctx := context.Background()
	taskID := a2a.TaskID("task")
	configs := sortConfigList([]*a2a.PushConfig{
		{ID: newID(), URL: "https://example.com/push1"},
		{ID: newID(), URL: "https://example.com/push2"},
	})
	anotherTaskID := a2a.TaskID("another-task")
	anotherConfigs := sortConfigList([]*a2a.PushConfig{
		{ID: newID(), URL: "https://example.com/push3"},
		{ID: newID(), URL: "https://example.com/push4"},
	})

	testCases := []struct {
		name       string
		taskID     a2a.TaskID
		wantRemain map[a2a.TaskID][]*a2a.PushConfig
	}{
		{
			name:   "existing task",
			taskID: taskID,
			wantRemain: map[a2a.TaskID][]*a2a.PushConfig{
				anotherTaskID: anotherConfigs,
			},
		},
		{
			name:   "non-existent task",
			taskID: a2a.TaskID("non-existent"),
			wantRemain: map[a2a.TaskID][]*a2a.PushConfig{
				taskID:        configs,
				anotherTaskID: anotherConfigs,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			for _, config := range configs {
				_ = store.Save(ctx, taskID, config)
			}
			for _, config := range anotherConfigs {
				_ = store.Save(ctx, anotherTaskID, config)
			}

			err := store.DeleteAll(ctx, tc.taskID)
			if err != nil {
				t.Fatalf("DeleteAll() failed: %v", err)
			}

			got := toConfigList(store.configs)
			if diff := cmp.Diff(tc.wantRemain, got); diff != "" {
				t.Errorf("Get() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInMemoryPushConfigStore_ConcurrenctCreation(t *testing.T) {
	ctx := t.Context()
	var wg sync.WaitGroup
	store := NewInMemoryStore()
	taskID := a2a.TaskID("concurrent-task")
	numGoroutines := 100
	created := make(chan *a2a.PushConfig, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			config := &a2a.PushConfig{URL: fmt.Sprintf("https://example.com/push-%d", i)}
			if err := store.Save(ctx, taskID, config); err != nil {
				t.Errorf("concurrent Save() failed: %v", err)
			}
			created <- config
		}(i)
	}

	wg.Wait()
	close(created)

	configs, err := store.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if len(configs) != numGoroutines {
		t.Fatalf("Expected %d configs to be created, but got %d", numGoroutines, len(configs))
	}

	for got := range created {
		found := false
		for _, config := range configs {
			if config.URL == got.URL {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing config with URL %q", got.URL)
		}
	}
}

func sortConfigList(configs []*a2a.PushConfig) []*a2a.PushConfig {
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].ID < configs[j].ID
	})
	return configs
}

func toConfigList(storeConfigs map[a2a.TaskID]map[string]*a2a.PushConfig) map[a2a.TaskID][]*a2a.PushConfig {
	result := make(map[a2a.TaskID][]*a2a.PushConfig)
	for taskID, configsMap := range storeConfigs {
		var configs []*a2a.PushConfig
		for _, config := range configsMap {
			configs = append(configs, config)
		}
		result[taskID] = sortConfigList(configs)
	}
	return result
}
