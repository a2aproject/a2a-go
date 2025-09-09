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
	"bytes"
	"context"
	"encoding/gob"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
)

// InMemoryTaskStore stores deep-copied Tasks in memory.
type Mem struct {
	mu    sync.RWMutex
	tasks map[a2a.TaskID]*a2a.Task
}

// NewInMemoryTaskStore creates an empty InMemoryTaskStore
func NewMem() *Mem {
	return &Mem{
		tasks: make(map[a2a.TaskID]*a2a.Task),
	}
}

func (s *Mem) Save(ctx context.Context, task *a2a.Task) error {
	copy, err := deepCopy(task)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.tasks[task.ID] = copy
	s.mu.Unlock()

	return nil
}

func (s *Mem) Get(ctx context.Context, taskId a2a.TaskID) (*a2a.Task, error) {
	s.mu.RLock()
	task, ok := s.tasks[taskId]
	s.mu.RUnlock()

	if !ok {
		return nil, a2a.ErrTaskNotFound
	}

	return deepCopy(task)
}

// Copy to keep a saved Task unchanged until an explicit Save.
func deepCopy(task *a2a.Task) (*a2a.Task, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	if err := enc.Encode(*task); err != nil {
		return nil, err
	}

	copy := a2a.Task{}
	if err := dec.Decode(&copy); err != nil {
		return nil, err
	}

	return &copy, nil
}
