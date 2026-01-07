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
