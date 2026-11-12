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
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// ServerHooks are the hooks of a server plugin. All fields are optional.
type ServerHooks struct {
	// BeforeCall runs before every call and can modify the request. The returned context is passed to
	// the call and to AfterCall. If it returns a result or an error, the call is not handled and the value
	// is returned to the client.
	BeforeCall func(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error)

	// AfterCall runs on every response value: once for a unary call, once per event for a streaming call.
	// It can modify the response. If it returns an error, the error is returned instead of the value.
	AfterCall func(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error

	// WrapCall wraps every call outside the call hooks.
	WrapCall func(next a2asrv.CallHandler) a2asrv.CallHandler

	// BeforeExecute runs before the agent executes. The returned context is passed to the agent,
	// OnExecuteEvent and AfterExecute. If it returns an error, the agent is not executed.
	BeforeExecute func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (context.Context, error)

	// OnExecuteEvent runs on every event emitted by the agent and returns the event to emit instead.
	// If it returns an error, the execution stops with the error.
	OnExecuteEvent func(ctx context.Context, execCtx *a2asrv.ExecutorContext, event a2a.Event) (a2a.Event, error)

	// AfterExecute runs after the execution finishes with its result or error. Its context carries
	// the values of the context returned by BeforeExecute but is not canceled when the execution ends.
	AfterExecute func(ctx context.Context, execCtx *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error)

	// WrapExecutor wraps the agent executor outside the execution hooks.
	WrapExecutor func(next a2asrv.AgentExecutor) a2asrv.AgentExecutor

	// BeforeTaskWrite runs before every Create or Update call on the task store. The returned
	// context is passed to the store and to AfterTaskWrite. If it returns an error, the write
	// is not performed and the error is returned from the store call.
	BeforeTaskWrite func(ctx context.Context, w *TaskWrite) (context.Context, error)

	// AfterTaskWrite runs after every Create or Update call on the task store with the error
	// returned by the store (nil on success, or the error returned by BeforeTaskWrite when the
	// write was skipped).
	AfterTaskWrite func(ctx context.Context, w *TaskWrite, err error)

	// WrapTaskStore wraps the task store outside the task store hooks.
	WrapTaskStore func(next taskstore.Store) taskstore.Store

	// BeforePush runs before every push notification delivery. It can add headers or skip the
	// delivery through the [push.Delivery] fields. The returned context is passed to the delivery
	// and to AfterPush. If it returns an error, the push is not delivered and the execution stops.
	BeforePush func(ctx context.Context, d *push.Delivery) (context.Context, error)

	// AfterPush runs after every push notification delivery with its result. If BeforePush failed,
	// the result carries its error.
	AfterPush func(ctx context.Context, d *push.Delivery, result *push.DeliveryResult)

	// WrapPushDelivery wraps the push delivery outside the push hooks.
	WrapPushDelivery func(next push.DeliveryHandler) push.DeliveryHandler
}

// Server returns an [a2asrv.Plugin] which runs the hooks. Execution hooks run for agent executions,
// not for cancelations.
func Server(name string, hooks ServerHooks) a2asrv.Plugin {
	return &serverPlugin{name: name, hooks: hooks}
}

type serverPlugin struct {
	a2asrv.PassthroughPlugin
	name  string
	hooks ServerHooks
}

func (p *serverPlugin) Name() string {
	return p.name
}
