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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/internal/utils"
)

type storedTask struct {
	task        *a2a.Task
	version     a2a.TaskVersion
	user        UserName
	lastUpdated time.Time
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

func (s *Mem) Save(ctx context.Context, task *a2a.Task, event a2a.Event, prevVersion a2a.TaskVersion) (a2a.TaskVersion, error) {
	if err := validateTask(task); err != nil {
		return a2a.TaskVersionMissing, err
	}

	userName, ok := s.authenticator(ctx)
	if !ok {
		userName = "anonymous"
	}
	copy, err := utils.DeepCopy(task)
	if err != nil {
		return a2a.TaskVersionMissing, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	version := a2a.TaskVersion(1)
	if stored := s.tasks[task.ID]; stored != nil {
		if prevVersion != a2a.TaskVersionMissing && stored.version != prevVersion {
			return a2a.TaskVersionMissing, fmt.Errorf("concurrent task modification failed")
		}
		version = stored.version + 1
	}

	s.tasks[task.ID] = &storedTask{
		task:        copy,
		version:     version,
		user:        userName,
		lastUpdated: s.timeProvider(),
	}

	return a2a.TaskVersion(version), nil
}

func (s *Mem) Get(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, a2a.TaskVersion, error) {
	s.mu.RLock()
	storedTask, ok := s.tasks[taskID]
	s.mu.RUnlock()

	if !ok {
		return nil, a2a.TaskVersionMissing, a2a.ErrTaskNotFound
	}

	task, err := utils.DeepCopy(storedTask.task)
	if err != nil {
		return nil, a2a.TaskVersionMissing, fmt.Errorf("task copy failed: %w", err)
	}

	return task, a2a.TaskVersion(storedTask.version), nil
}

func (s *Mem) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	userName, ok := s.authenticator(ctx)
	if !ok {
		return nil, a2a.ErrUnauthenticated
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = 50
	} else if pageSize < 1 || pageSize > 100 {
		return nil, fmt.Errorf("page size must be between 1 and 100 inclusive, got %d: %w", pageSize, a2a.ErrInvalidRequest)
	}
	if req.HistoryLength < 0 {
		return nil, fmt.Errorf("history length must be non-negative integer, got %d: %w", req.HistoryLength, a2a.ErrInvalidRequest)
	}
	s.mu.RLock()
	filteredTasks := filterTasks(s.tasks, userName, req)
	s.mu.RUnlock()

	totalSize := len(filteredTasks)
	slices.SortFunc(filteredTasks, func(a, b *storedTask) int {
		if timeCmp := b.lastUpdated.Compare(a.lastUpdated); timeCmp != 0 {
			return timeCmp
		}
		return strings.Compare(string(b.task.ID), string(a.task.ID))
	})

	tasksPage, nextPageToken, err := applyPagination(filteredTasks, pageSize, req)
	if err != nil {
		return nil, err
	}

	listTasksResult, err := toListTasksResult(tasksPage, req)
	if err != nil {
		return nil, err
	}

	return &a2a.ListTasksResponse{
		Tasks:         listTasksResult,
		TotalSize:     totalSize,
		PageSize:      pageSize,
		NextPageToken: nextPageToken,
	}, nil
}

func filterTasks(tasks map[a2a.TaskID]*storedTask, userName UserName, req *a2a.ListTasksRequest) []*storedTask {
	var filteredTasks []*storedTask
	for _, storedTask := range tasks {
		if storedTask.user != userName {
			continue
		}
		if req.ContextID != "" && storedTask.task.ContextID != req.ContextID {
			continue
		}
		if req.Status != a2a.TaskStateUnspecified && storedTask.task.Status.State != req.Status {
			continue
		}
		if req.LastUpdatedAfter != nil && storedTask.lastUpdated.Before(*req.LastUpdatedAfter) {
			continue
		}

		filteredTasks = append(filteredTasks, storedTask)
	}
	return filteredTasks
}

func applyPagination(filteredTasks []*storedTask, pageSize int, req *a2a.ListTasksRequest) ([]*storedTask, string, error) {
	var cursorTime time.Time
	var cursorTaskID a2a.TaskID
	var err error

	var tasksPage []*storedTask
	if req.PageToken != "" {
		cursorTime, cursorTaskID, err = decodePageToken(req.PageToken)
		if err != nil {
			return nil, "", err
		}
		pageStartIndex := sort.Search(len(filteredTasks), func(i int) bool {
			task := filteredTasks[i]

			timeCmp := task.lastUpdated.Compare(cursorTime)
			if timeCmp < 0 {
				return true
			}
			if timeCmp > 0 {
				return false
			}
			return strings.Compare(string(task.task.ID), string(cursorTaskID)) < 0
		})
		tasksPage = filteredTasks[pageStartIndex:]
	} else {
		tasksPage = filteredTasks
	}

	var nextPageToken string
	if pageSize >= len(tasksPage) {
		pageSize = len(tasksPage)
	} else {
		lastElement := tasksPage[pageSize-1]
		nextPageToken = encodePageToken(lastElement.lastUpdated, lastElement.task.ID)
	}
	tasksPage = tasksPage[:pageSize]
	return tasksPage, nextPageToken, nil
}

func toListTasksResult(tasks []*storedTask, req *a2a.ListTasksRequest) ([]*a2a.Task, error) {
	var result []*a2a.Task
	for _, storedTask := range tasks {
		taskCopy, err := utils.DeepCopy(storedTask.task)
		if err != nil {
			return nil, err
		}
		if req.HistoryLength > 0 && len(taskCopy.History) > req.HistoryLength {
			taskCopy.History = taskCopy.History[len(taskCopy.History)-req.HistoryLength:]
		}
		if !req.IncludeArtifacts {
			taskCopy.Artifacts = nil
		}

		result = append(result, taskCopy)
	}
	return result, nil
}

func encodePageToken(updatedTime time.Time, taskID a2a.TaskID) string {
	timeStrNano := updatedTime.Format(time.RFC3339Nano)
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s_%s", timeStrNano, taskID)))
}

func decodePageToken(nextPageToken string) (time.Time, a2a.TaskID, error) {
	decoded, err := base64.URLEncoding.DecodeString(nextPageToken)
	if err != nil {
		return time.Time{}, "", a2a.ErrParseError
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
