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

package a2asrv

import (
	"context"
	"fmt"
	"mime"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// agentCardEnforcer implements [a2asrv.CallInterceptor].
type agentCardEnforcer struct {
	PassthroughCallInterceptor
	capabilities a2a.AgentCapabilities
	inputModes   []string
}

type pushEnabledKey struct{}

// NewAgentCardEnforcer creates a call interceptor that validates incoming requests against
// the provided [a2a.AgentCard] and rejects the ones which do not conform to it.
//
// Requests to methods which are not supported according to [a2a.AgentCard.Capabilities]
// (streaming, push notifications, extended agent card) are rejected, as well as requests
// which do not activate all the required extensions.
//
// Message parts are validated against the input modes declared by the card skills. Skills
// without input modes inherit [a2a.AgentCard.DefaultInputModes], which are also used if the
// card has no skills. If a client did not set [a2a.Part.MediaType], it is inferred from the
// part content: "text/plain" for [a2a.Text], "application/json" for [a2a.Data] and
// "application/octet-stream" for [a2a.Raw] and [a2a.URL]. Parts with media types not listed
// in the input modes are rejected with [a2a.ErrUnsupportedContentType]. No validation is
// performed if the card declares no input modes.
func NewAgentCardEnforcer(card *a2a.AgentCard) CallInterceptor {
	if card == nil {
		return PassthroughCallInterceptor{}
	}
	return &agentCardEnforcer{
		capabilities: card.Capabilities,
		inputModes:   inputModesFromCard(card),
	}
}

func (a *agentCardEnforcer) Before(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
	ctx, err := a.checkCapabilities(ctx, callCtx)
	if err != nil {
		return ctx, nil, err
	}
	if err := a.checkInputModes(callCtx, req); err != nil {
		return ctx, nil, err
	}
	return ctx, nil, nil
}

func (a *agentCardEnforcer) checkCapabilities(ctx context.Context, callCtx *CallContext) (context.Context, error) {
	if err := a.checkRequiredExtensions(callCtx); err != nil {
		return ctx, err
	}
	switch callCtx.Method() {
	case "SendStreamingMessage", "SubscribeToTask":
		if !a.capabilities.Streaming {
			return ctx, a2a.ErrUnsupportedOperation
		}
	case "GetTaskPushConfig", "ListTaskPushConfigs", "CreateTaskPushConfig", "DeleteTaskPushConfig":
		if !a.capabilities.PushNotifications {
			return ctx, a2a.ErrPushNotificationNotSupported
		}
		ctx = context.WithValue(ctx, pushEnabledKey{}, true)
	case "GetExtendedAgentCard":
		if !a.capabilities.ExtendedAgentCard {
			return ctx, a2a.ErrUnsupportedOperation
		}
	}
	return ctx, nil
}

func (a *agentCardEnforcer) checkRequiredExtensions(callCtx *CallContext) error {
	if len(a.capabilities.Extensions) == 0 {
		return nil
	}
	requestedURIs := callCtx.Extensions().RequestedURIs()
	for _, ext := range a.capabilities.Extensions {
		if ext.Required {
			found := slices.Contains(requestedURIs, ext.URI)
			if !found {
				return a2a.ErrExtensionSupportRequired
			}
		}
	}
	return nil
}

func (a *agentCardEnforcer) checkInputModes(callCtx *CallContext, req *Request) error {
	if len(a.inputModes) == 0 {
		return nil
	}
	switch callCtx.Method() {
	case "SendMessage", "SendStreamingMessage":
		if req == nil || req.Payload == nil {
			return a2a.ErrInvalidParams
		}
		r, ok := req.Payload.(*a2a.SendMessageRequest)
		if !ok || r == nil || r.Message == nil {
			return nil
		}
		if err := validateMediaTypes(a.inputModes, r.Message); err != nil {
			return err
		}
	}
	return nil
}

func inputModesFromCard(card *a2a.AgentCard) []string {
	var inputModes []string
	inheritsDefaults := len(card.Skills) == 0
	for _, skill := range card.Skills {
		if len(skill.InputModes) == 0 {
			inheritsDefaults = true
			continue
		}
		inputModes = append(inputModes, parseMediaTypes(skill.InputModes)...)
	}

	if inheritsDefaults {
		inputModes = append(inputModes, parseMediaTypes(card.DefaultInputModes)...)
	}
	return inputModes
}

func parseMediaTypes(mediaTypes []string) []string {
	var parsed []string
	for _, mediaType := range mediaTypes {
		parsedMediaType, _, err := mime.ParseMediaType(mediaType)
		if err != nil {
			panic(fmt.Errorf("invalid media type %q: %v", mediaType, err))
		}
		parsed = append(parsed, parsedMediaType)
	}
	return parsed
}

func validateMediaTypes(inputModes []string, msg *a2a.Message) error {
	for _, part := range msg.Parts {
		mediaType := effectiveMediaType(part)
		parsedMediaType, _, err := mime.ParseMediaType(mediaType)
		if err != nil {
			return fmt.Errorf("failed to parse media type %q: %v: %w", mediaType, err, a2a.ErrInvalidParams)
		}
		if !slices.Contains(inputModes, parsedMediaType) {
			return fmt.Errorf("media type %q is not supported: %w", parsedMediaType, a2a.ErrUnsupportedContentType)
		}
	}
	return nil
}

func effectiveMediaType(p *a2a.Part) string {
	if p.MediaType != "" {
		return p.MediaType
	}
	switch p.Content.(type) {
	case a2a.Text:
		return "text/plain"
	case a2a.Data:
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
