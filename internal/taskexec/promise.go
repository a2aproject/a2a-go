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

package taskexec

import (
	"context"

	"github.com/a2aproject/a2a-go/a2a"
)

type promise struct {
	// done channel gets closed once value or err field is set
	done  chan struct{}
	value a2a.SendMessageResult
	err   error
}

func newPromise() *promise {
	return &promise{done: make(chan struct{})}
}

func (p *promise) resolve(value a2a.SendMessageResult) {
	p.value = value
	close(p.done)
}

func (p *promise) reject(err error) {
	p.err = err
	close(p.done)
}

func (r *promise) wait(ctx context.Context) (a2a.SendMessageResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return r.value, r.err
	}
}
