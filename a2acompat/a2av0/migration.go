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

package a2av0

import (
	"context"
	"iter"

	legacya2a "github.com/a2aproject/a2a-go/a2a"
	legacyclient "github.com/a2aproject/a2a-go/a2aclient"
	legacysrv "github.com/a2aproject/a2a-go/a2asrv"
	legacyqueue "github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/a2aproject/a2a-go/v1/a2a"
	"github.com/a2aproject/a2a-go/v1/a2aclient"
	"github.com/a2aproject/a2a-go/v1/a2asrv"
	"github.com/a2aproject/a2a-go/v1/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v1/a2asrv/taskstore"
)

func NewServerInterceptor(old legacysrv.CallInterceptor) a2asrv.CallInterceptor {
	return &srvInterceptorAdapter{old}
}

func NewClientInterceptor(old legacyclient.CallInterceptor) a2aclient.CallInterceptor {
	return &clientInterceptorAdapter{old}
}

func NewQueueManager(old legacyqueue.Manager) eventqueue.Manager {
	return &queueManagerAdapter{old}
}

func NewQueue(old legacyqueue.Queue) (eventqueue.Reader, eventqueue.Writer) {
	return &queueAdapter{old}, &queueAdapter{old}
}

func NewAgentExecutor(old legacysrv.AgentExecutor) a2asrv.AgentExecutor {
	return &executorAdapter{old}
}

func NewTaskStore(old legacysrv.TaskStore) taskstore.Store {
	return &taskStoreAdapter{old}
}

type srvInterceptorAdapter struct {
	legacysrv.CallInterceptor
}

func (s *srvInterceptorAdapter) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	return s.CallInterceptor.Before(ctx, callCtx, req)
}

func (s *srvInterceptorAdapter) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {

	return s.CallInterceptor.After(ctx, callCtx, resp)
}

type clientInterceptorAdapter struct {
	legacyclient.CallInterceptor
}

func (s *clientInterceptorAdapter) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	return s.CallInterceptor.Before(ctx, req)
}

func (s *clientInterceptorAdapter) After(ctx context.Context, resp *a2aclient.Response) error {
	return s.CallInterceptor.After(ctx, resp)
}

type queueManagerAdapter struct {
	legacyqueue.Manager
}

func (q *queueManagerAdapter) CreateReader(ctx context.Context, taskID a2a.TaskID) (Reader, error) {
	return q.Manager.CreateReader(ctx, taskID)
}

func (q *queueManagerAdapter) CreateWriter(ctx context.Context, taskID a2a.TaskID) (Writer, error) {
	return q.Manager.CreateWriter(ctx, taskID)
}

func (q *queueManagerAdapter) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	return q.Manager.Destroy(ctx, taskID)
}

type queueAdapter struct {
	legacyqueue.Queue
}

func (q *queueAdapter) Read(ctx context.Context) (*eventqueue.Message, error) {
	return q.Queue.Read(ctx)
}

func (q *queueAdapter) Write(ctx context.Context, msg *eventqueue.Message) error {
	return q.Queue.Write(ctx, msg)
}

func (q *queueAdapter) Close() error {
	return q.Queue.Close()
}

type executorAdapter struct {
	legacysrv.AgentExecutor
}

func (e *executorAdapter) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return e.AgentExecutor.Execute(ctx, execCtx)
}

func (e *executorAdapter) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return e.AgentExecutor.Cancel(ctx, execCtx)
}

type taskStoreAdapter struct {
	legacysrv.TaskStore
}

func (s *taskStoreAdapter) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	return s.TaskStore.Save(ctx, task, task, legacya2a.TaskVersionMissing)
}

func (s *taskStoreAdapter) Update(ctx context.Context, update *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	return s.TaskStore.Save(ctx, update.Task, update.Event, update.PrevVersion)
}

func (s *taskStoreAdapter) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	return s.TaskStore.Get(ctx, taskID)
}

func (s *taskStoreAdapter) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return s.TaskStore.List(ctx, req)
}
