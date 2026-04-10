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

package workqueue

// semaphore is a channel-based concurrency limiter. The channel is nil when no limit is enforced.
type semaphore struct {
	ch chan struct{}
}

func newSemaphore(maxConcurrency int) *semaphore {
	if maxConcurrency <= 0 {
		return &semaphore{}
	}
	return &semaphore{ch: make(chan struct{}, maxConcurrency)}
}

func (s *semaphore) acquire() {
	if s.ch == nil {
		return
	}
	s.ch <- struct{}{}
}

func (s *semaphore) tryAcquire() bool {
	if s.ch == nil {
		return true
	}
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *semaphore) release() {
	if s.ch == nil {
		return
	}
	<-s.ch
}
