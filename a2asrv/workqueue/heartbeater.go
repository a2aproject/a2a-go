// Copyright 2025 The A2A Authors
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

package workqueue

import (
	"context"
	"time"
)

type heartbeaterKeyType struct{}

// WithHeartbeater creates a new context with the provided [Hearbeater] attached.
func WithHeartbeater(ctx context.Context, hb Heartbeater) context.Context {
	return context.WithValue(ctx, heartbeaterKeyType{}, hb)
}

// HeartbeaterFrom returns a [Heartbeater] attached using [WithHeartbeater].
func HeartbeaterFrom(ctx context.Context) (Heartbeater, bool) {
	hb, ok := ctx.Value(heartbeaterKeyType{}).(Heartbeater)
	return hb, ok
}

// Heartbeater defines an optional heartbeat policy for message handler. Heartbeats are sent while worker is handling a message.
// For [NewPullQueue] the interface must be implemented by [Message] structs returned from Read.
// For [NewPushQueue] heartbeater needs to be attached to context passed to [HandlerFn] using [WithHeartbeater].
type Heartbeater interface {
	// HeartbeatInterval returns the interval between heartbeats.
	HeartbeatInterval() time.Duration
	// Heartbeat is called at heartbeat interval to mark the message as still being handled.
	Heartbeat(ctx context.Context) error
}
