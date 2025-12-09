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
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/utils"
)

type storedTask struct {
	user        UserName
	lastUpdated time.Time
	task        *a2a.Task
}

type UserName string

type Authenticator func(context.Context) (UserName, bool)

type TimeProvider func() time.Time

type Option func(*Mem)

func WithAuthenticator(a Authenticator) Option {
	return func(m *Mem) {
		m.authenticator = a
	}
}

func WithTimeProvider(tp TimeProvider) Option {
	return func(m *Mem) {
		m.timeProvider = tp
	}
}

// Mem stores deep-copied [a2a.Task]-s in memory.
type Mem struct {
	mu    sync.RWMutex
	tasks map[a2a.TaskID]*storedTask

	authenticator Authenticator
	timeProvider  TimeProvider
}

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// NewMem creates an empty [Mem] store.
func NewMem(opts ...Option) *Mem {
	m := &Mem{
		tasks: make(map[a2a.TaskID]*storedTask),
		authenticator: func(ctx context.Context) (UserName, bool) {
			return "", false
		},
		timeProvider: func() time.Time {
			return time.Now()
		},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (s *Mem) Save(ctx context.Context, task *a2a.Task) error {
	if err := validateTask(task); err != nil {
		return err
	}

	userName, ok := s.authenticator(ctx)
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
		lastUpdated: s.timeProvider(),
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
	userName, ok := s.authenticator(ctx)

	// Only proceed if user name is available for authentication
	if !ok {
		return nil, a2a.ErrAuthFailed
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	} else if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("must be between 1 and 100 inclusive, got %d", pageSize)
	}

	// Filter tasks per request filters before pagination
	var prefilteredTasks []*storedTask
	s.mu.RLock()
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
		if req.LastUpdatedAfter != nil && storedTask.lastUpdated.Before(*req.LastUpdatedAfter) {
			continue
		}

		prefilteredTasks = append(prefilteredTasks, storedTask)
	}
	s.mu.RUnlock()

	// Count total number of tasks before pagination and after all other filters are applied
	totalSize := len(prefilteredTasks)

	// Sort tasks by last updated time by descending order, if they are equal sort by task ID
	slices.SortFunc(prefilteredTasks, func(a, b *storedTask) int {
		if timeCmp := b.lastUpdated.Compare(a.lastUpdated); timeCmp != 0 {
			return timeCmp
		}
		return strings.Compare(string(b.task.ID), string(a.task.ID))
	})

	var cursorTime time.Time
	var cursorTaskID a2a.TaskID
	var err error
	if req.PageToken != "" {
		cursorTime, cursorTaskID, err = s.base64Decode(req.PageToken)
		if err != nil {
			return nil, err
		}
	}

	// Filter tasks after pagination
	var filteredTasks []*storedTask
	if req.PageToken != "" {
		for _, storedTask := range prefilteredTasks {
			timeCmp := storedTask.lastUpdated.Compare(cursorTime)
			if timeCmp < 0 {
				filteredTasks = append(filteredTasks, storedTask)
			} else if timeCmp == 0 {
				taskIDCmp := strings.Compare(string(storedTask.task.ID), string(cursorTaskID))
				if taskIDCmp < 0 {
					filteredTasks = append(filteredTasks, storedTask)
				}
			}
		}
	} else {
		filteredTasks = prefilteredTasks
	}
	// Filter tasks per page size
	var hasNextPage bool
	if pageSize > len(filteredTasks) {
		pageSize = len(filteredTasks)
		hasNextPage = false
	} else {
		hasNextPage = true
	}
	filteredTasks = filteredTasks[:pageSize]
	var nextPageToken string
	if hasNextPage {
		lastElement := filteredTasks[len(filteredTasks)-1]
		nextPageToken = s.base64Encode(lastElement.lastUpdated, lastElement.task.ID)
	}

	// From sorted and filtered tasks of type []*storedTask, apply necessary filters and create []*a2a.Task
	var result []*a2a.Task
	for _, storedTask := range filteredTasks {
		// Copy the task to avoid modifying the original
		taskCopy, err := utils.DeepCopy(storedTask.task)
		if err != nil {
			return nil, err
		}

		// If HistoryLength is set, truncate the history, otherwise keep it as is
		if req.HistoryLength > 0 && len(taskCopy.History) > req.HistoryLength {
			taskCopy.History = taskCopy.History[len(taskCopy.History)-req.HistoryLength:]
		} else if req.HistoryLength < 0 {
			return nil, fmt.Errorf("must be non-negative integer, got %d", req.HistoryLength)
		}

		// If IncludeArtifacts is false, remove the artifacts, otherwise keep it as is
		if !req.IncludeArtifacts {
			taskCopy.Artifacts = nil
		}

		result = append(result, taskCopy)
	}
	return &a2a.ListTasksResponse{
		Tasks:         result,
		TotalSize:     totalSize,
		PageSize:      pageSize,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Mem) base64Encode(updatedTime time.Time, taskID a2a.TaskID) string {
	timeStrNano := updatedTime.Format(time.RFC3339Nano)
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s_%s", timeStrNano, taskID)))
}

func (s *Mem) base64Decode(nextPageToken string) (time.Time, a2a.TaskID, error) {
	decoded, err := base64.URLEncoding.DecodeString(nextPageToken)
	if err != nil {
		return time.Time{}, "", err
	}

	parts := strings.Split(string(decoded), "_")
	if len(parts) != 2 {
		return time.Time{}, "", a2a.ErrParseError
	}

	taskID := a2a.TaskID(parts[1])

	updatedTime, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", a2a.ErrParseError
	}

	return updatedTime, taskID, nil
}
