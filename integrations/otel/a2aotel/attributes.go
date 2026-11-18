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

package a2aotel

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/integrations/otel/a2aotel/internal/semconv"
)

// requestAttributes returns the attributes which are known from the request payload alone.
func requestAttributes(method string, payload any) []attribute.KeyValue {
	attrs := []attribute.KeyValue{semconv.A2AMethodNameKey.String(method)}
	req, ok := payload.(*a2a.SendMessageRequest)
	if !ok {
		return attrs
	}
	if msg := req.Message; msg != nil {
		if msg.ID != "" {
			attrs = append(attrs, semconv.A2AMessageIDKey.String(msg.ID))
		}
		if len(msg.ReferenceTasks) > 0 {
			ids := make([]string, len(msg.ReferenceTasks))
			for i, id := range msg.ReferenceTasks {
				ids[i] = string(id)
			}
			attrs = append(attrs, semconv.A2AMessageReferenceTaskIDsKey.StringSlice(ids))
		}
	}
	if method == semconv.MethodSendMessage && req.Config != nil && req.Config.ReturnImmediately {
		attrs = append(attrs, semconv.A2AOperationReturnImmediatelyKey.Bool(true))
	}
	return attrs
}

func agentAttributes(card *a2a.AgentCard) []attribute.KeyValue {
	if card == nil {
		return nil
	}
	var attrs []attribute.KeyValue
	if card.Name != "" {
		attrs = append(attrs, semconv.GenAIAgentNameKey.String(card.Name))
	}
	if card.Description != "" {
		attrs = append(attrs, semconv.GenAIAgentDescriptionKey.String(card.Description))
	}
	if card.Version != "" {
		attrs = append(attrs, semconv.GenAIAgentVersionKey.String(card.Version))
	}
	return attrs
}

func requestTenant(payload any) string {
	switch req := payload.(type) {
	case *a2a.SendMessageRequest:
		return req.Tenant
	case *a2a.GetTaskRequest:
		return req.Tenant
	case *a2a.ListTasksRequest:
		return req.Tenant
	case *a2a.CancelTaskRequest:
		return req.Tenant
	case *a2a.SubscribeToTaskRequest:
		return req.Tenant
	case *a2a.PushConfig:
		return req.Tenant
	case *a2a.GetTaskPushConfigRequest:
		return req.Tenant
	case *a2a.ListTaskPushConfigRequest:
		return req.Tenant
	case *a2a.DeleteTaskPushConfigRequest:
		return req.Tenant
	case *a2a.GetExtendedAgentCardRequest:
		return req.Tenant
	default:
		return ""
	}
}

// singleValue tracks whether exactly one distinct non-empty value was observed.
type singleValue struct {
	value    string
	multiple bool
}

func (s *singleValue) observe(value string) {
	if value == "" || s.multiple || s.value == value {
		return
	}
	if s.value != "" {
		s.multiple = true
		return
	}
	s.value = value
}

func (s *singleValue) get() (string, bool) {
	return s.value, s.value != "" && !s.multiple
}

// callObserver collects the attributes which are known only after the call ends. The conventions
// require omitting task IDs, task states and context IDs when a call carries more than one of them.
type callObserver struct {
	taskIDs    singleValue
	taskStates singleValue
	contextIDs singleValue
	err        error
}

func newCallObserver(reqPayload any) *callObserver {
	o := &callObserver{}
	switch req := reqPayload.(type) {
	case *a2a.SendMessageRequest:
		if req.Message != nil {
			o.taskIDs.observe(string(req.Message.TaskID))
			o.contextIDs.observe(req.Message.ContextID)
		}
	case *a2a.GetTaskRequest:
		o.taskIDs.observe(string(req.ID))
	case *a2a.CancelTaskRequest:
		o.taskIDs.observe(string(req.ID))
	case *a2a.SubscribeToTaskRequest:
		o.taskIDs.observe(string(req.ID))
	case *a2a.ListTasksRequest:
		o.contextIDs.observe(req.ContextID)
	case *a2a.PushConfig:
		o.taskIDs.observe(string(req.TaskID))
	case *a2a.GetTaskPushConfigRequest:
		o.taskIDs.observe(string(req.TaskID))
	case *a2a.ListTaskPushConfigRequest:
		o.taskIDs.observe(string(req.TaskID))
	case *a2a.DeleteTaskPushConfigRequest:
		o.taskIDs.observe(string(req.TaskID))
	}
	return o
}

func (o *callObserver) observe(payload any, err error) {
	if err != nil {
		o.err = err
		return
	}
	switch resp := payload.(type) {
	case *a2a.Task:
		o.observeTask(resp)
	case *a2a.Message:
		o.taskIDs.observe(string(resp.TaskID))
		o.contextIDs.observe(resp.ContextID)
	case *a2a.TaskStatusUpdateEvent:
		o.taskIDs.observe(string(resp.TaskID))
		o.contextIDs.observe(resp.ContextID)
		o.taskStates.observe(semconv.TaskState(resp.Status.State))
	case *a2a.TaskArtifactUpdateEvent:
		o.taskIDs.observe(string(resp.TaskID))
		o.contextIDs.observe(resp.ContextID)
	case *a2a.ListTasksResponse:
		for _, task := range resp.Tasks {
			o.observeTask(task)
		}
	case *a2a.PushConfig:
		o.taskIDs.observe(string(resp.TaskID))
	case *a2a.ListTaskPushConfigResponse:
		for _, cfg := range resp.Configs {
			o.taskIDs.observe(string(cfg.TaskID))
		}
	case []*a2a.PushConfig:
		for _, cfg := range resp {
			o.taskIDs.observe(string(cfg.TaskID))
		}
	}
}

func (o *callObserver) observeTask(task *a2a.Task) {
	if task == nil {
		return
	}
	o.taskIDs.observe(string(task.ID))
	o.contextIDs.observe(task.ContextID)
	o.taskStates.observe(semconv.TaskState(task.Status.State))
}

func (o *callObserver) attributes() []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if id, ok := o.taskIDs.get(); ok {
		attrs = append(attrs, semconv.A2ATaskIDKey.String(id))
	}
	if state, ok := o.taskStates.get(); ok {
		attrs = append(attrs, semconv.A2ATaskStateKey.String(state))
	}
	if id, ok := o.contextIDs.get(); ok {
		attrs = append(attrs, semconv.GenAIConversationIDKey.String(id))
	}
	return attrs
}
