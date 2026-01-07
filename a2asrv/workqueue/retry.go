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

import (
	"math"
	"math/rand"
	"time"
)

var defaultExponentialBackoff = &ExponentialReadBackoff{
	BaseDelay: time.Second,
	MaxDelay:  30 * time.Second,
}

// ReadRetryPolicy is used to configure pull-queue loop behavior in case of errors returned from Read.
type ReadRetryPolicy interface {
	// NextDelay returns the sleep duration for a failed Read attempt.
	NextDelay(attempt int) time.Duration
}

// ExponentialReadBackoff is a [ReadRetryPolicy] implementation which uses exponential backoff with full jitter.
type ExponentialReadBackoff struct {
	// BaseDelay is used for calculating retry interval as base * 2 ^ attempt.
	BaseDelay time.Duration
	// MaxDelay sets a cap for the value returned from NextDelay.
	MaxDelay time.Duration
}

func (e *ExponentialReadBackoff) NextDelay(attempt int) time.Duration {
	delay := float64(e.BaseDelay) * math.Pow(2.0, float64(attempt))
	if delay > float64(e.MaxDelay) {
		delay = float64(e.MaxDelay)
	}
	return time.Duration(rand.Int63n(int64(delay + 1)))
}
