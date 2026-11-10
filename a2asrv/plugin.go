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
	"reflect"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// Plugin extends a [RequestHandler] by wrapping its components. Each method receives
// the next component and returns a wrapped one, or next itself to leave it unchanged.
// If multiple plugins are installed, the first one is the outermost wrapper.
type Plugin interface {
	// Name identifies the plugin.
	Name() string

	// WrapCall wraps the handling of every call. Plugins wrap outside the [CallInterceptor]s.
	WrapCall(next CallHandler) CallHandler

	// WrapExecutor wraps the [AgentExecutor]. It is called once, when the handler is created.
	// If the wrapper implements [AgentExecutionCleaner], its Cleanup is called after the Cleanup of the
	// executors it wraps, so it must not forward Cleanup to next.
	WrapExecutor(next AgentExecutor) AgentExecutor

	// WrapTaskStore wraps the task store, including the default in-memory store.
	WrapTaskStore(next taskstore.Store) taskstore.Store

	// WrapPushDelivery wraps the delivery of every push notification. It is not called if push notifications
	// are not configured.
	WrapPushDelivery(next push.DeliveryHandler) push.DeliveryHandler
}

// WithPlugins adds plugins to the ones already set. The first plugin is the outermost wrapper.
func WithPlugins(plugins ...Plugin) RequestHandlerOption {
	return func(ih *InterceptedHandler, h *defaultRequestHandler) {
		ih.plugins = append(ih.plugins, plugins...)
	}
}

func wrapWithPlugins[T any](next T, plugins []Plugin, wrap func(Plugin, T) T) T {
	for _, plugin := range slices.Backward(plugins) {
		next = wrap(plugin, next)
	}
	return next
}

func wrapExecutor(executor AgentExecutor, plugins []Plugin) (AgentExecutor, []AgentExecutionCleaner) {
	var cleaners []AgentExecutionCleaner
	if cleaner, ok := executor.(AgentExecutionCleaner); ok {
		cleaners = append(cleaners, cleaner)
	}
	executor = wrapWithPlugins(executor, plugins, func(plugin Plugin, next AgentExecutor) AgentExecutor {
		wrapped := plugin.WrapExecutor(next)
		if cleaner, ok := wrapped.(AgentExecutionCleaner); ok && !sameExecutor(wrapped, next) {
			cleaners = append(cleaners, cleaner)
		}
		return wrapped
	})
	return executor, cleaners
}

// sameExecutor reports whether a plugin returned next unchanged. Values that can't be compared with ==,
// such as functions, are treated as different, because comparing them would panic.
func sameExecutor(a, b AgentExecutor) bool {
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.ValueOf(a).Comparable() && a == b
}

// PassthroughPlugin can be embedded by [Plugin] implementers who don't need all methods.
// It returns every component unchanged, so adding a method to [Plugin] doesn't break plugins embedding it.
type PassthroughPlugin struct{}

// WrapCall implements [Plugin.WrapCall].
func (PassthroughPlugin) WrapCall(next CallHandler) CallHandler {
	return next
}

// WrapExecutor implements [Plugin.WrapExecutor].
func (PassthroughPlugin) WrapExecutor(next AgentExecutor) AgentExecutor {
	return next
}

// WrapTaskStore implements [Plugin.WrapTaskStore].
func (PassthroughPlugin) WrapTaskStore(next taskstore.Store) taskstore.Store {
	return next
}

// WrapPushDelivery implements [Plugin.WrapPushDelivery].
func (PassthroughPlugin) WrapPushDelivery(next push.DeliveryHandler) push.DeliveryHandler {
	return next
}
