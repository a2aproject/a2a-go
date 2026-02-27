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
	"errors"
	"fmt"
	"iter"
	"maps"

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

// NewServerInterceptor adapts a legacy server call interceptor to the v1 interceptor interface.
func NewServerInterceptor(old legacysrv.CallInterceptor) a2asrv.CallInterceptor {
	return &srvInterceptorAdapter{old}
}

// NewClientInterceptor adapts a legacy client call interceptor to the v1 interceptor interface.
func NewClientInterceptor(old legacyclient.CallInterceptor) a2aclient.CallInterceptor {
	return &clientInterceptorAdapter{old}
}

// NewQueueManager adapts a legacy queue manager to the v1 manager interface.
func NewQueueManager(old legacyqueue.Manager) eventqueue.Manager {
	return &queueManagerAdapter{old}
}

// NewQueue adapts a legacy queue to v1 reader and writer interfaces.
func NewQueue(old legacyqueue.Queue) (eventqueue.Reader, eventqueue.Writer) {
	return &queueAdapter{old}, &queueAdapter{old}
}

// NewAgentExecutor adapts a legacy agent executor to the v1 executor interface.
func NewAgentExecutor(old legacysrv.AgentExecutor) a2asrv.AgentExecutor {
	return &executorAdapter{old}
}

// NewTaskStore adapts a legacy task store to the v1 store interface.
func NewTaskStore(old legacysrv.TaskStore) taskstore.Store {
	return &taskStoreAdapter{old}
}

// NewClient adapts a legacy client to the v1 client interface.
func NewClient(old *legacyclient.Client) *a2aclient.Client {
	return nil
}

type srvInterceptorAdapter struct {
	legacysrv.CallInterceptor
}

func (s *srvInterceptorAdapter) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	ctx, legacyCallCtx := toLegacyCallContext(ctx, callCtx)
	legacyReq := toLegacyRequest(req)
	newCtx, err := s.CallInterceptor.Before(ctx, legacyCallCtx, legacyReq)
	if err != nil {
		return newCtx, nil, err
	}
	return newCtx, nil, nil
}

func (s *srvInterceptorAdapter) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	_, legacyCallCtx := toLegacyCallContext(ctx, callCtx)
	legacyResp := toLegacyResponse(resp)

	err := s.CallInterceptor.After(ctx, legacyCallCtx, legacyResp)
	if err != nil {
		return err
	}

	if legacyResp.Payload != nil {
		v1Payload, v1Err := ToV1Event(legacyResp.Payload.(legacya2a.Event))
		if v1Err != nil {
			return v1Err
		}
		resp.Payload = v1Payload
	}
	resp.Err = legacyResp.Err

	return nil
}

type clientInterceptorAdapter struct {
	legacyclient.CallInterceptor
}

func (s *clientInterceptorAdapter) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	legacyReq := toLegacyClientRequest(req)
	newCtx, err := s.CallInterceptor.Before(ctx, legacyReq)
	if err != nil {
		return newCtx, nil, err
	}
	// No early response in legacy client interceptor before
	return newCtx, nil, nil
}

func (s *clientInterceptorAdapter) After(ctx context.Context, resp *a2aclient.Response) error {
	legacyResp := toLegacyClientResponse(resp)
	err := s.CallInterceptor.After(ctx, legacyResp)
	if err != nil {
		return err
	}
	// Copy back potential changes from legacyResp to resp
	resp.Err = legacyResp.Err
	if legacyResp.Payload != nil {
		v1Payload, v1Err := ToV1Event(legacyResp.Payload.(legacya2a.Event))
		if v1Err != nil {
			return v1Err
		}
		resp.Payload = v1Payload
	}
	return nil
}

type queueManagerAdapter struct {
	legacyqueue.Manager
}

func (q *queueManagerAdapter) CreateReader(ctx context.Context, taskID a2a.TaskID) (eventqueue.Reader, error) {
	legacyQueue, err := q.Manager.GetOrCreate(ctx, legacya2a.TaskID(taskID))
	if err != nil {
		return nil, err
	}
	return &queueAdapter{legacyQueue}, nil
}

func (q *queueManagerAdapter) CreateWriter(ctx context.Context, taskID a2a.TaskID) (eventqueue.Writer, error) {
	legacyQueue, err := q.Manager.GetOrCreate(ctx, legacya2a.TaskID(taskID))
	if err != nil {
		return nil, err
	}
	return &queueAdapter{legacyQueue}, nil
}

func (q *queueManagerAdapter) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	return q.Manager.Destroy(ctx, legacya2a.TaskID(taskID))
}

type queueAdapter struct {
	legacyqueue.Queue
}

func (q *queueAdapter) Read(ctx context.Context) (*eventqueue.Message, error) {
	ev, version, err := q.Queue.Read(ctx)
	if err != nil {
		return nil, err
	}
	v1Event, err := ToV1Event(ev)
	if err != nil {
		return nil, err
	}
	return &eventqueue.Message{Event: v1Event, TaskVersion: taskstore.TaskVersion(version), Protocol: Version}, nil
}

func (q *queueAdapter) Write(ctx context.Context, msg *eventqueue.Message) error {
	legacyEvent, err := FromV1Event(msg.Event)
	if err != nil {
		return err
	}
	return q.Queue.WriteVersioned(ctx, legacyEvent, legacya2a.TaskVersion(msg.TaskVersion))
}

func (q *queueAdapter) Close() error {
	return q.Queue.Close()
}

type executorAdapter struct {
	legacysrv.AgentExecutor
}

var errYieldStopped = errors.New("yield stopped")

func (e *executorAdapter) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yieldAdapter := &yieldAdapter{yield: yield}
		legacyReqCtx := toLegacyRequestContext(execCtx)
		err := e.AgentExecutor.Execute(ctx, legacyReqCtx, yieldAdapter)
		if err != nil && !errors.Is(err, errYieldStopped) {
			yield(nil, err)
		}
	}
}

func (e *executorAdapter) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yieldAdapter := &yieldAdapter{yield: yield}
		legacyReqCtx := toLegacyRequestContext(execCtx)
		err := e.AgentExecutor.Cancel(ctx, legacyReqCtx, yieldAdapter)
		if err != nil && !errors.Is(err, errYieldStopped) {
			yield(nil, err)
		}
	}
}

type yieldAdapter struct {
	yield func(a2a.Event, error) bool
}

func (p *yieldAdapter) Write(ctx context.Context, ev legacya2a.Event) error {
	v1Event, err := ToV1Event(ev)
	if err != nil {
		return err
	}
	if !p.yield(v1Event, nil) {
		return errYieldStopped
	}
	return nil
}

func (p *yieldAdapter) Read(ctx context.Context) (legacya2a.Event, legacya2a.TaskVersion, error) {
	return nil, legacya2a.TaskVersionMissing, fmt.Errorf("AgentExecutor must not Read() from the queue")
}

func (p *yieldAdapter) WriteVersioned(ctx context.Context, ev legacya2a.Event, version legacya2a.TaskVersion) error {
	return fmt.Errorf("AgentExecutor must use Write()")
}

func (p *yieldAdapter) Close() error {
	return fmt.Errorf("AgentExecutor must not Close() the queue")
}

type taskStoreAdapter struct {
	legacysrv.TaskStore
}

func (s *taskStoreAdapter) Create(ctx context.Context, task *a2a.Task) (taskstore.TaskVersion, error) {
	legacyTask := FromV1Task(task)
	version, err := s.TaskStore.Save(ctx, legacyTask, legacyTask, legacya2a.TaskVersionMissing)
	return taskstore.TaskVersion(version), err
}

func (s *taskStoreAdapter) Update(ctx context.Context, update *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	legacyTask := FromV1Task(update.Task)
	legacyEvent, err := FromV1Event(update.Event)
	if err != nil {
		return taskstore.TaskVersionMissing, err
	}
	version, err := s.TaskStore.Save(ctx, legacyTask, legacyEvent, legacya2a.TaskVersion(update.PrevVersion))
	return taskstore.TaskVersion(version), err
}

func (s *taskStoreAdapter) Get(ctx context.Context, taskID a2a.TaskID) (*taskstore.StoredTask, error) {
	task, version, err := s.TaskStore.Get(ctx, legacya2a.TaskID(taskID))
	if err != nil {
		return nil, err
	}
	v1Task, err := ToV1Task(task)
	if err != nil {
		return nil, err
	}
	return &taskstore.StoredTask{Task: v1Task, Version: taskstore.TaskVersion(version)}, nil
}

func (s *taskStoreAdapter) List(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	legacyReq := FromV1ListTasksRequest(req)
	resp, err := s.TaskStore.List(ctx, legacyReq)
	if err != nil {
		return nil, err
	}
	return ToV1ListTasksResponse(resp)
}

func toLegacyCallContext(ctx context.Context, v1 *a2asrv.CallContext) (context.Context, *legacysrv.CallContext) {
	if v1 == nil {
		return ctx, nil
	}
	params := make(map[string][]string)
	for k, v := range v1.ServiceParams().List() {
		params[k] = v
	}
	legacyMeta := legacysrv.NewRequestMeta(params)
	newCtx, legacyCallCtx := legacysrv.WithCallContext(ctx, legacyMeta)
	if v1.User != nil {
		legacyCallCtx.User = &legacyUserWrapper{v1.User}
	}
	return newCtx, legacyCallCtx
}

type legacyUserWrapper struct {
	user *a2asrv.User
}

func (w *legacyUserWrapper) Name() string {
	return w.user.Name
}

func (w *legacyUserWrapper) Authenticated() bool {
	return w.user.Authenticated
}

func toLegacyRequest(v1 *a2asrv.Request) *legacysrv.Request {
	if v1 == nil {
		return nil
	}
	var legacyPayload any
	if v1.Payload != nil {
		legacyPayload = fromV1Payload(v1.Payload)
	}
	return &legacysrv.Request{Payload: legacyPayload}
}

func toLegacyResponse(v1 *a2asrv.Response) *legacysrv.Response {
	if v1 == nil {
		return nil
	}
	var legacyPayload legacya2a.Event
	if v1.Payload != nil {
		legacyPayload, _ = FromV1Event(v1.Payload.(a2a.Event))
	}
	return &legacysrv.Response{
		Payload: legacyPayload,
		Err:     v1.Err,
	}
}

func fromV1Payload(payload any) any {
	switch v := payload.(type) {
	case *a2a.GetTaskRequest:
		return FromV1GetTaskRequest(v)
	case *a2a.ListTasksRequest:
		return FromV1ListTasksRequest(v)
	case *a2a.CancelTaskRequest:
		return FromV1CancelTaskRequest(v)
	case *a2a.SendMessageRequest:
		return FromV1SendMessageRequest(v)
	default:
		return payload
	}
}

func toLegacyClientRequest(v1 *a2aclient.Request) *legacyclient.Request {
	if v1 == nil {
		return nil
	}
	m := make(map[string][]string)
	maps.Copy(m, v1.ServiceParams)
	return &legacyclient.Request{
		Method:  v1.Method,
		BaseURL: v1.BaseURL,
		Meta:    legacyclient.CallMeta(m),
		Card:    FromV1AgentCard(v1.Card),
		Payload: fromV1Payload(v1.Payload),
	}
}

func toLegacyClientResponse(v1 *a2aclient.Response) *legacyclient.Response {
	if v1 == nil {
		return nil
	}
	m := make(map[string][]string)
	for k, v := range v1.ServiceParams {
		m[k] = v
	}
	return &legacyclient.Response{
		Method:  v1.Method,
		BaseURL: v1.BaseURL,
		Err:     v1.Err,
		Meta:    legacyclient.CallMeta(m),
		Card:    FromV1AgentCard(v1.Card),
		Payload: fromV1Payload(v1.Payload),
	}
}

func toLegacyRequestContext(v1 *a2asrv.ExecutorContext) *legacysrv.RequestContext {
	if v1 == nil {
		return nil
	}
	return &legacysrv.RequestContext{
		Message:    FromV1Message(v1.Message),
		TaskID:     legacya2a.TaskID(v1.TaskID),
		StoredTask: FromV1Task(v1.StoredTask),
		ContextID:  v1.ContextID,
		Metadata:   v1.Metadata,
	}
}
