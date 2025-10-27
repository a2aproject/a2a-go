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
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/go-cmp/cmp"
)

func TestInMemoryPushConfigStore_Save(t *testing.T) {
	ctx := t.Context()
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
			wantErr: fmt.Errorf("%w: push config cannot be nil", a2a.ErrInvalidParams),
		},
		{
			name:    "empty URL",
			config:  &a2a.PushConfig{},
			wantErr: fmt.Errorf("%w: push config endpoint cannot be empty", a2a.ErrInvalidParams),
		},
		{
			name:    "invalid URL",
			config:  &a2a.PushConfig{URL: "not a url"},
			wantErr: fmt.Errorf("%w: invalid push config endpoint URL: parse \"not a url\": invalid URI for request", a2a.ErrInvalidParams),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			saved, err := store.Save(ctx, taskID, tc.config)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Save() failed: %v", err)
				}
				if tc.config.ID == "" {
					if saved.ID == "" {
						t.Fatalf("Saved config ID is empty")
					}
					saved.ID = ""
				}
				if diff := cmp.Diff(tc.config, saved); diff != "" {
					t.Fatalf("Stored config mismatch (-want +got):\n%s", diff)
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
	ctx := t.Context()
	taskID := a2a.TaskID("test-task")

	t.Run("modify original config after save", func(t *testing.T) {
		store := NewInMemoryStore()
		originalConfig := &a2a.PushConfig{ID: newID(), URL: "https://original.com"}
		saved, err := store.Save(ctx, taskID, originalConfig)
		if err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
		savedID := saved.ID
		savedURL := saved.URL

		originalConfig.URL = "https://modified-original.com"
		originalConfig.ID = "new-id-for-original"

		got, err := store.Get(ctx, taskID, savedID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		wantConfig := &a2a.PushConfig{ID: savedID, URL: savedURL}
		if diff := cmp.Diff(wantConfig, got); diff != "" {
			t.Errorf("Retrieved config mismatch after modifying original (-want +got):\n%s", diff)
		}
	})

	t.Run("modify returned saved config", func(t *testing.T) {
		store := NewInMemoryStore()
		originalConfig := &a2a.PushConfig{ID: newID(), URL: "https://original.com"}
		saved, err := store.Save(ctx, taskID, originalConfig)
		if err != nil {
			t.Fatalf("Save() failed: %v", err)
		}
		savedID := saved.ID
		savedURL := saved.URL

		saved.URL = "https://modified-original.com"
		saved.ID = "new-id-for-original"

		got, err := store.Get(ctx, taskID, savedID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		wantConfig := &a2a.PushConfig{ID: savedID, URL: savedURL}
		if diff := cmp.Diff(wantConfig, got); diff != "" {
			t.Errorf("Retrieved config mismatch after modifying original (-want +got):\n%s", diff)
		}
	})

	t.Run("modify retrieved config after get", func(t *testing.T) {
		store := NewInMemoryStore()
		initialConfig := &a2a.PushConfig{ID: newID(), URL: "https://initial-get.com"}
		saved, err := store.Save(ctx, taskID, initialConfig)
		if err != nil {
			t.Fatalf("Save() failed: %v", err)
		}

		retrieved, err := store.Get(ctx, taskID, saved.ID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		retrieved.URL = "https://modified-retrieved.com"
		retrieved.ID = "new-id-for-retrieved"
		secondRetrieved, err := store.Get(ctx, taskID, saved.ID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		if diff := cmp.Diff(initialConfig, secondRetrieved); diff != "" {
			t.Errorf("Second retrieved config mismatch after modifying first retrieved (-want +got):\n%s", diff)
		}
	})
}

func TestInMemoryPushConfigStore_Get(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("task")
	config := &a2a.PushConfig{ID: newID(), URL: "https://example.com/push1"}

	store := NewInMemoryStore()
	config, _ = store.Save(ctx, taskID, config)

	testCases := []struct {
		name    string
		taskID  a2a.TaskID
		want    *a2a.PushConfig
		wantErr error
	}{
		{
			name:   "existing task",
			taskID: taskID,
			want:   config,
		},
		{
			name:    "non-existent task",
			taskID:  a2a.TaskID("non-existent"),
			wantErr: ErrPushConfigNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.Get(ctx, tc.taskID, config.ID)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Get() failed: %v", err)
				}
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Fatalf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, tc.want, diff)
				}
			} else {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("Save() error = %v, want %v", err, tc.wantErr)
				}
			}
		})
	}
}

func TestInMemoryPushConfigStore_List(t *testing.T) {
	ctx := t.Context()
	taskID := a2a.TaskID("task")
	config := &a2a.PushConfig{ID: newID(), URL: "https://example.com/push1"}

	store := NewInMemoryStore()
	config, _ = store.Save(ctx, taskID, config)

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
			want:   []*a2a.PushConfig{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configs, err := store.List(ctx, tc.taskID)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}
			want := sortConfigList(tc.want)
			got := sortConfigList(configs)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, want, diff)
			}
		})
	}
}

func TestInMemoryPushConfigStore_Delete(t *testing.T) {
	ctx := t.Context()
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
			want:   []*a2a.PushConfig{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			for _, config := range configs {
				_, _ = store.Save(ctx, taskID, config)
			}

			err := store.Delete(ctx, tc.taskID, tc.configID)
			if err != nil {
				t.Fatalf("Delete() failed: %v", err)
			}
			got, err := store.List(ctx, tc.taskID)
			if err != nil {
				t.Fatalf("Get() failed: %v", err)
			}
			want := sortConfigList(tc.want)
			got = sortConfigList(got)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, want, diff)
			}
		})
	}
}

func TestInMemoryPushConfigStore_DeleteAll(t *testing.T) {
	ctx := t.Context()
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
				_, _ = store.Save(ctx, taskID, config)
			}
			for _, config := range anotherConfigs {
				_, _ = store.Save(ctx, anotherTaskID, config)
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
			saved, err := store.Save(ctx, taskID, config)
			if err != nil {
				t.Errorf("concurrent Save() failed: %v", err)
			}
			created <- saved
		}(i)
	}

	wg.Wait()
	close(created)

	configs, err := store.List(ctx, taskID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	if len(configs) != numGoroutines {
		t.Fatalf("Expected %d configs to be created, but got %d", numGoroutines, len(configs))
	}

	for c := range created {
		got, err := store.Get(ctx, taskID, c.ID)
		if err != nil {
			t.Fatalf("Get() failed: %v", err)
		}
		if diff := cmp.Diff(c, got); diff != "" {
			t.Errorf("Get() (+got,-want):\ngot = %v\nwant %v\ndiff = %s", got, c, diff)
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
