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

package taskstore

import (
	"context"
	"encoding/gob"
	"slices"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/utils"
)

type storedTask struct {
	user        string
	lastUpdated time.Time
	task        *a2a.Task
}

type Authenticate struct {
	getUserName func(context.Context) (string, bool)
}

type Option func(*Authenticate)

func WithAuthInfoProviderFn(getUserName func(context.Context) (string, bool)) Option {
	return func(a *Authenticate) {
		a.getUserName = getUserName
	}
}

// Mem stores deep-copied [a2a.Task]-s in memory.
type Mem struct {
	mu    sync.RWMutex
	tasks map[a2a.TaskID]*storedTask
	auth  *Authenticate
}

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// NewMem creates an empty [Mem] store.
func NewMem(opts ...Option) *Mem {
	auth := &Authenticate{
		getUserName: func(ctx context.Context) (string, bool) {
			return "", false
		},
	}

	for _, opt := range opts {
		opt(auth)
	}

	return &Mem{
		tasks: make(map[a2a.TaskID]*storedTask),
		auth:  auth,
	}
}

func (s *Mem) Save(ctx context.Context, task *a2a.Task) error {
	if err := validateTask(task); err != nil {
		return err
	}

	userName, ok := s.auth.getUserName(ctx)
	if !ok {
		userName = "anonymous"
	}
	copy, err := utils.DeepCopy(task)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.tasks[task.ID] = &storedTask{
		user:        userName,
		lastUpdated: time.Now(),
		task:        copy,
	}
	s.mu.Unlock()

	return nil
}

func (s *Mem) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	s.mu.RLock()
	storedTask, ok := s.tasks[taskID]
	s.mu.RUnlock()

	if !ok {
		return nil, a2a.ErrTaskNotFound
	}

	return utils.DeepCopy(storedTask.task)
}

func (s *Mem) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	userName, ok := s.auth.getUserName(ctx)

	// Only proceed if user name is available for authentication
	if !ok {
		return nil, a2a.ErrAuthFailed
	}

	// Filter tasks per request filters
	var filteredTasks []*storedTask
	for _, storedTask := range s.tasks {
		// Retrieve only tasks created by the user
		if storedTask.user != userName {
			continue
		}

		// Filter by context ID if it is set
		if req.ContextID != "" && storedTask.task.ContextID != req.ContextID {
			continue
		}
		// Filter by status if it is set
		if req.Status != a2a.TaskStateUnspecified && storedTask.task.Status.State != req.Status {
			continue
		}

		// Filter by LastUpdatedTime if it is set
		if req.LastUpdatedTime != nil && storedTask.lastUpdated.Before(*req.LastUpdatedTime) {
			continue
		}

		filteredTasks = append(filteredTasks, storedTask)
	}

	// Sort tasks by last updated time
	slices.SortFunc(filteredTasks, func(a, b *storedTask) int {
		return a.lastUpdated.Compare(b.lastUpdated)
	})

	// From sorted and filtered tasks of type []*storedTask, apply necessary filters and create []*a2a.Task
	var result []*a2a.Task
	for _, storedTask := range filteredTasks {
		// Copy the task to avoid modifying the original
		taskCopy, err := utils.DeepCopy(storedTask.task)
		if err != nil {
			return nil, err
		}

		// If HistoryLength is set, truncate the history, otherwise keep it as is
		if req.HistoryLength > 0 {
			lengthToShow := min(len(storedTask.task.History), req.HistoryLength)
			taskCopy.History = storedTask.task.History[:lengthToShow]
		}

		// If IncludeArtifacts is false, remove the artifacts, otherwise keep it as is
		if !req.IncludeArtifacts {
			taskCopy.Artifacts = nil
		}

		result = append(result, taskCopy)
	}
	return &a2a.ListTasksResponse{Tasks: result}, nil
}
