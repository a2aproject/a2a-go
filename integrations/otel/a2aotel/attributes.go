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
	"errors"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

const (
	a2aMethodNameKey                 = attribute.Key("a2a.method.name")
	a2aProtocolVersionKey            = attribute.Key("a2a.protocol.version")
	a2aTenantKey                     = attribute.Key("a2a.tenant")
	a2aMessageIDKey                  = attribute.Key("a2a.message.id")
	a2aMessageReferenceTaskIDsKey    = attribute.Key("a2a.message.reference_task_ids")
	a2aOperationReturnImmediatelyKey = attribute.Key("a2a.operation.return_immediately")
	a2aTaskIDKey                     = attribute.Key("a2a.task.id")
	a2aTaskStateKey                  = attribute.Key("a2a.task.state")
	genAIAgentNameKey                = attribute.Key("gen_ai.agent.name")
	genAIAgentDescriptionKey         = attribute.Key("gen_ai.agent.description")
	genAIAgentVersionKey             = attribute.Key("gen_ai.agent.version")
	genAIConversationIDKey           = attribute.Key("gen_ai.conversation.id")
)

const methodSendMessage = "SendMessage"

// sdkMethodNames maps the SDK method names which differ from the a2a.method.name values.
var sdkMethodNames = map[string]string{
	"CreateTaskPushConfig": "CreateTaskPushNotificationConfig",
	"GetTaskPushConfig":    "GetTaskPushNotificationConfig",
	"ListTaskPushConfigs":  "ListTaskPushNotificationConfigs",
	"DeleteTaskPushConfig": "DeleteTaskPushNotificationConfig",
}

func methodName(sdkMethod string) string {
	if name, ok := sdkMethodNames[sdkMethod]; ok {
		return name
	}
	return sdkMethod
}

// clientErrorType returns the A2A error reason if err is an A2A protocol error, or the Go type name of err otherwise.
func clientErrorType(err error) string {
	if isProtocolError(err) {
		return a2a.ErrorReason(err)
	}
	return semconv.ErrorType(err).Value.AsString()
}

// isProtocolError reports whether err is an A2A protocol error. Client transports convert every
// error response to an [a2a.Error], and [a2a.ErrorReason] falls back to the internal error reason
// for errors which are not A2A errors.
func isProtocolError(err error) bool {
	var a2aErr *a2a.Error
	if errors.As(err, &a2aErr) || errors.Is(err, a2a.ErrInternalError) {
		return true
	}
	return a2a.ErrorReason(err) != a2a.ErrorReason(a2a.ErrInternalError)
}

// requestAttributes returns the attributes which are known from the request payload alone.
func requestAttributes(method string, payload any) []attribute.KeyValue {
	attrs := []attribute.KeyValue{a2aMethodNameKey.String(method)}
	req, ok := payload.(*a2a.SendMessageRequest)
	if !ok {
		return attrs
	}
	if msg := req.Message; msg != nil {
		if msg.ID != "" {
			attrs = append(attrs, a2aMessageIDKey.String(msg.ID))
		}
		if len(msg.ReferenceTasks) > 0 {
			ids := make([]string, len(msg.ReferenceTasks))
			for i, id := range msg.ReferenceTasks {
				ids[i] = string(id)
			}
			attrs = append(attrs, a2aMessageReferenceTaskIDsKey.StringSlice(ids))
		}
	}
	if method == methodSendMessage && req.Config != nil && req.Config.ReturnImmediately {
		attrs = append(attrs, a2aOperationReturnImmediatelyKey.Bool(true))
	}
	return attrs
}

func agentAttributes(card *a2a.AgentCard) []attribute.KeyValue {
	if card == nil {
		return nil
	}
	var attrs []attribute.KeyValue
	if card.Name != "" {
		attrs = append(attrs, genAIAgentNameKey.String(card.Name))
	}
	if card.Description != "" {
		attrs = append(attrs, genAIAgentDescriptionKey.String(card.Description))
	}
	if card.Version != "" {
		attrs = append(attrs, genAIAgentVersionKey.String(card.Version))
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
		o.taskStates.observe(resp.Status.State.String())
	case *a2a.TaskArtifactUpdateEvent:
		o.taskIDs.observe(string(resp.TaskID))
		o.contextIDs.observe(resp.ContextID)
	case *a2a.ListTasksResponse:
		for _, task := range resp.Tasks {
			o.observeTask(task)
		}
	}
}

func (o *callObserver) observeTask(task *a2a.Task) {
	if task == nil {
		return
	}
	o.taskIDs.observe(string(task.ID))
	o.contextIDs.observe(task.ContextID)
	o.taskStates.observe(task.Status.State.String())
}

func (o *callObserver) attributes() []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if id, ok := o.taskIDs.get(); ok {
		attrs = append(attrs, a2aTaskIDKey.String(id))
	}
	if state, ok := o.taskStates.get(); ok {
		attrs = append(attrs, a2aTaskStateKey.String(state))
	}
	if id, ok := o.contextIDs.get(); ok {
		attrs = append(attrs, genAIConversationIDKey.String(id))
	}
	return attrs
}
