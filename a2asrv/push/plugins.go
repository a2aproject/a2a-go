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

package push

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aplugin/core"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Plugin is invoked around every push notification delivered by [HTTPPushSender].
// Custom [Sender] implementations need to call the hooks themselves.
// See [github.com/a2aproject/a2a-go/v2/a2aplugin] for composing and registering plugins.
type Plugin interface {
	core.Plugin

	// BeforePushSend is called before the notification is sent. Plugins may modify
	// [Delivery.Headers] or set [Delivery.Skip]. A non-nil error fails the delivery.
	BeforePushSend(ctx context.Context, delivery *Delivery) (context.Context, error)

	// OnPushSent is called after the delivery attempt, or after the delivery was skipped.
	OnPushSent(ctx context.Context, result *DeliveryResult) error
}

// Delivery is passed to [Plugin.BeforePushSend].
type Delivery struct {
	// TaskID is the ID of the task the notification is about.
	TaskID a2a.TaskID
	// URL is the webhook URL. Changing it has no effect.
	URL string
	// Headers are the outgoing HTTP headers. Plugins may add or change entries.
	Headers http.Header
	// Event is the event being pushed.
	Event a2a.Event
	// Skip can be set to true to drop this delivery.
	Skip bool
}

// DeliveryResult is passed to [Plugin.OnPushSent].
type DeliveryResult struct {
	// Delivery is the delivery passed to [Plugin.BeforePushSend].
	Delivery *Delivery
	// Duration is the duration of the HTTP request.
	Duration time.Duration
	// StatusCode is the HTTP status code of the response, or 0 if no response was received.
	StatusCode int
	// Err is the delivery error, if any. It is set even if the sender doesn't return it.
	Err error
	// Skipped is true if a plugin dropped the delivery.
	Skipped bool
}

// pushHooksReporter is implemented by plugin managers which can report
// that none of the registered plugins implement push hooks.
type pushHooksReporter interface {
	HasPushHooks() bool
}

// sendWithPlugin sends req running the plugin hooks around the delivery.
func (s *HTTPPushSender) sendWithPlugin(ctx context.Context, config *a2a.PushConfig, event a2a.Event, req *http.Request) error {
	delivery := &Delivery{TaskID: config.TaskID, URL: config.URL, Headers: req.Header, Event: event}
	ctx, err := s.plugin.BeforePushSend(ctx, delivery)
	if err != nil {
		return s.handleError(ctx, fmt.Errorf("push plugin failed: %w", err))
	}

	result := &DeliveryResult{Delivery: delivery, Skipped: delivery.Skip}
	if !delivery.Skip {
		start := time.Now()
		result.StatusCode, result.Err = s.do(ctx, req.WithContext(ctx))
		result.Duration = time.Since(start)
	}
	if err := s.plugin.OnPushSent(ctx, result); err != nil {
		log.Error(ctx, "plugin OnPushSent failed", err, "plugin", s.plugin.Name())
	}

	if result.Err != nil {
		return s.handleError(ctx, result.Err)
	}
	return nil
}
