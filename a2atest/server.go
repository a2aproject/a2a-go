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

package a2atest

import (
	"context"
	"fmt"
	"iter"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/a2asrv/workqueue"
)

// Server is an in-process A2A server for integration testing.
// It wraps a [a2asrv.RequestHandler] with in-memory dependencies.
type Server struct {
	Handler a2asrv.RequestHandler
	store   taskstore.Store
}

// ServerOption is a functional option for configuring a Server.
// Options that perform I/O may return an error.
type ServerOption func(*serverConfig) error

type serverConfig struct {
	taskStore    taskstore.Store
	queueManager eventqueue.Manager
	workQueue    workqueue.Queue
	clusterMode  bool
	options      []a2asrv.RequestHandlerOption
}

// WithTaskStore sets a custom task store for the server.
func WithTaskStore(store taskstore.Store) ServerOption {
	return func(c *serverConfig) error {
		c.taskStore = store
		return nil
	}
}

// WithQueueManager sets a custom event queue manager for the server.
func WithQueueManager(m eventqueue.Manager) ServerOption {
	return func(c *serverConfig) error {
		c.queueManager = m
		return nil
	}
}

// WithWorkQueue sets a custom work queue for the server.
// Implies cluster mode.
func WithWorkQueue(q workqueue.Queue) ServerOption {
	return func(c *serverConfig) error {
		c.workQueue = q
		c.clusterMode = true
		return nil
	}
}

// WithHandlerOptions adds [a2asrv.RequestHandlerOption] values (e.g. interceptors).
func WithHandlerOptions(opts ...a2asrv.RequestHandlerOption) ServerOption {
	return func(c *serverConfig) error {
		c.options = append(c.options, opts...)
		return nil
	}
}

// WithTasks pre-seeds the task store with the given tasks.
// Tasks are created using context.Background() since this happens during
// setup, not during a test run.
func WithTasks(tasks ...*a2a.Task) ServerOption {
	return func(c *serverConfig) error {
		if c.taskStore == nil {
			c.taskStore = taskstore.NewInMemory(&taskstore.InMemoryStoreConfig{})
		}
		for _, task := range tasks {
			if _, err := c.taskStore.Create(context.Background(), task); err != nil {
				return fmt.Errorf("unexpected taskstore.Create failure: %w", err)
			}
		}
		return nil
	}
}

// NewServer creates a new in-process A2A server for testing.
//
// Usage:
//
//	exec := ExecutorFromFunction(myFunc)
//	server := NewServer(t, exec)
//	result, err := server.SendMessage(ctx, &a2a.SendMessageRequest{...})
func NewServer(t *testing.T, executor a2asrv.AgentExecutor, opts ...ServerOption) *Server {
	t.Helper()

	cfg := &serverConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			t.Fatalf("server option failed: %v", err)
		}
	}

	if cfg.queueManager == nil {
		cfg.queueManager = eventqueue.NewInMemoryManager()
	}
	if cfg.taskStore == nil {
		cfg.taskStore = taskstore.NewInMemory(&taskstore.InMemoryStoreConfig{})
	}
	if cfg.workQueue == nil && cfg.clusterMode {
		cfg.workQueue = workqueue.NewInMemory(nil)
	}

	var handler a2asrv.RequestHandler
	if cfg.clusterMode {
		handler = a2asrv.NewHandler(executor,
			append(cfg.options,
				a2asrv.WithClusterMode(a2asrv.ClusterConfig{
					QueueManager: cfg.queueManager,
					WorkQueue:    cfg.workQueue,
					TaskStore:    cfg.taskStore,
				}),
			)...,
		)
	} else {
		handler = a2asrv.NewHandler(executor,
			append(cfg.options,
				a2asrv.WithTaskStore(cfg.taskStore),
				a2asrv.WithEventQueueManager(cfg.queueManager),
			)...,
		)
	}

	return &Server{
		Handler: handler,
		store:   cfg.taskStore,
	}
}

// SendMessage sends a message through the server and returns the result.
// It calls [a2asrv.RequestHandler.SendMessage] directly.
func (s *Server) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	return s.Handler.SendMessage(ctx, req)
}

// SubscribeToTask subscribes to events for a task and returns an iterator.
// It calls [a2asrv.RequestHandler.SubscribeToTask] directly.
func (s *Server) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return s.Handler.SubscribeToTask(ctx, req)
}

// GetTask retrieves a task from the server's task store by ID.
func (s *Server) GetTask(ctx context.Context, taskID a2a.TaskID) (*a2a.Task, error) {
	stored, err := s.store.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return stored.Task, nil
}

// TaskStore returns the server's underlying task store.
func (s *Server) TaskStore() taskstore.Store {
	return s.store
}
