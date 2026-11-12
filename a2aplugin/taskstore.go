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

package a2aplugin

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// TaskWrite describes a write performed on the task store. Its fields must not be modified.
type TaskWrite struct {
	// Task is the task state passed to the store.
	Task *a2a.Task
	// Event is the event that triggered the write. Nil on create.
	Event a2a.Event
	// PrevTask is the previous state of the task. Nil on create.
	PrevTask *a2a.Task
}

func (p *serverPlugin) WrapTaskStore(next taskstore.Store) taskstore.Store {
	if p.hooks.BeforeTaskWrite != nil || p.hooks.AfterTaskWrite != nil {
		next = &hookStore{Store: next, hooks: p.hooks}
	}
	if p.hooks.WrapTaskStore != nil {
		next = p.hooks.WrapTaskStore(next)
	}
	return next
}

type hookStore struct {
	taskstore.Store
	hooks ServerHooks
}

func (s *hookStore) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	write := &TaskWrite{Task: task}
	hookCtx, err := s.before(ctx, write)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	version, err := s.Store.Create(hookCtx, task)
	s.after(hookCtx, write, err)
	return version, err
}

func (s *hookStore) Update(ctx context.Context, req *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	write := &TaskWrite{Task: req.Task, Event: req.Event, PrevTask: req.PrevTask}
	hookCtx, err := s.before(ctx, write)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	version, err := s.Store.Update(hookCtx, req)
	s.after(hookCtx, write, err)
	return version, err
}

func (s *hookStore) before(ctx context.Context, write *TaskWrite) (context.Context, error) {
	if s.hooks.BeforeTaskWrite == nil {
		return ctx, nil
	}
	hookCtx, err := s.hooks.BeforeTaskWrite(ctx, write)
	if err != nil {
		if s.hooks.AfterTaskWrite != nil {
			s.hooks.AfterTaskWrite(ctx, write, err)
		}
		return ctx, err
	}
	return hookCtx, nil
}

func (s *hookStore) after(ctx context.Context, write *TaskWrite, err error) {
	if s.hooks.AfterTaskWrite != nil {
		s.hooks.AfterTaskWrite(ctx, write, err)
	}
}
