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

// Package semconv defines the OpenTelemetry semantic conventions for the Agent2Agent protocol
// shared by all signals the a2aotel package emits.
//
// See https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/a2a.md.
package semconv

import (
	"errors"

	"go.opentelemetry.io/otel/attribute"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// Attribute keys defined or referenced by the A2A semantic conventions.
const (
	A2AMethodNameKey                 = attribute.Key("a2a.method.name")
	A2AProtocolVersionKey            = attribute.Key("a2a.protocol.version")
	A2ATenantKey                     = attribute.Key("a2a.tenant")
	A2AMessageIDKey                  = attribute.Key("a2a.message.id")
	A2AMessageReferenceTaskIDsKey    = attribute.Key("a2a.message.reference_task_ids")
	A2AOperationReturnImmediatelyKey = attribute.Key("a2a.operation.return_immediately")
	A2ATaskIDKey                     = attribute.Key("a2a.task.id")
	A2ATaskStateKey                  = attribute.Key("a2a.task.state")
	GenAIAgentNameKey                = attribute.Key("gen_ai.agent.name")
	GenAIAgentDescriptionKey         = attribute.Key("gen_ai.agent.description")
	GenAIAgentVersionKey             = attribute.Key("gen_ai.agent.version")
	GenAIConversationIDKey           = attribute.Key("gen_ai.conversation.id")
	ErrorTypeKey                     = otelsemconv.ErrorTypeKey
	ServerAddressKey                 = otelsemconv.ServerAddressKey
	ServerPortKey                    = otelsemconv.ServerPortKey
)

// Well-known values of the a2a.method.name attribute.
const (
	MethodSendMessage                      = "SendMessage"
	MethodSendStreamingMessage             = "SendStreamingMessage"
	MethodGetTask                          = "GetTask"
	MethodListTasks                        = "ListTasks"
	MethodCancelTask                       = "CancelTask"
	MethodSubscribeToTask                  = "SubscribeToTask"
	MethodCreateTaskPushNotificationConfig = "CreateTaskPushNotificationConfig"
	MethodGetTaskPushNotificationConfig    = "GetTaskPushNotificationConfig"
	MethodListTaskPushNotificationConfigs  = "ListTaskPushNotificationConfigs"
	MethodDeleteTaskPushNotificationConfig = "DeleteTaskPushNotificationConfig"
	MethodGetExtendedAgentCard             = "GetExtendedAgentCard"
)

// sdkMethodNames maps the SDK method names which differ from the protocol operation names.
var sdkMethodNames = map[string]string{
	"CreateTaskPushConfig": MethodCreateTaskPushNotificationConfig,
	"GetTaskPushConfig":    MethodGetTaskPushNotificationConfig,
	"ListTaskPushConfigs":  MethodListTaskPushNotificationConfigs,
	"DeleteTaskPushConfig": MethodDeleteTaskPushNotificationConfig,
}

// MethodName returns the a2a.method.name value for a method name reported by the SDK.
func MethodName(sdkMethod string) string {
	if name, ok := sdkMethodNames[sdkMethod]; ok {
		return name
	}
	return sdkMethod
}

// TaskState returns the a2a.task.state value for a task state.
func TaskState(state a2a.TaskState) string {
	return state.String()
}

// ServerErrorType returns the error.type value for an error returned by a server call: the
// A2A error reason the server transports send to the client for it.
func ServerErrorType(err error) string {
	return a2a.ErrorReason(err)
}

// ClientErrorType returns the error.type value for an error returned by a client call: the
// A2A error reason if err is an A2A protocol error, or the Go type name of err otherwise.
func ClientErrorType(err error) string {
	if isProtocolError(err) {
		return a2a.ErrorReason(err)
	}
	return otelsemconv.ErrorType(err).Value.AsString()
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
