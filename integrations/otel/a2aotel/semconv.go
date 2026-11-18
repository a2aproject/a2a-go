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
