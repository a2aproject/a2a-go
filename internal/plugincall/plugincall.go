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

// Package plugincall dispatches the After hooks of server and client plugins.
package plugincall

import "github.com/a2aproject/a2a-go/v2/a2a"

// AfterFunc invokes the After hook of a plugin. Payload is nil when err is non-nil.
type AfterFunc func(payload any, err error, isStreamEvent bool)

// Call carries the plugin state across the After hooks of a single request.
// A nil Call is inactive and all its methods are no-ops.
type Call struct {
	after     AfterFunc
	streamErr error
	finished  bool
}

// New returns a Call which invokes after for every response.
func New(after AfterFunc) *Call {
	return &Call{after: after}
}

// Finish fires the final After hook at most once.
func (c *Call) Finish(payload any, err error) {
	if c == nil || c.finished {
		return
	}
	c.finished = true
	if err != nil {
		payload = nil
	}
	c.after(payload, err, false)
}

// FinishStream fires the final After hook of a stream with the last error
// yielded through the function returned by WrapStreamYield.
func (c *Call) FinishStream() {
	if c == nil {
		return
	}
	c.Finish(nil, c.streamErr)
}

// WrapStreamYield returns a yield function that fires a per-event After hook
// before each yield, recording any yielded error for FinishStream.
func (c *Call) WrapStreamYield(yield func(a2a.Event, error) bool) func(a2a.Event, error) bool {
	if c == nil {
		return yield
	}
	return func(ev a2a.Event, err error) bool {
		var payload any = ev
		if err != nil {
			c.streamErr = err
			payload = nil
		}
		c.after(payload, err, true)
		return yield(ev, err)
	}
}
