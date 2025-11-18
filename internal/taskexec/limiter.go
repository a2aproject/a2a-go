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
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/log"
)

var errQuotaOverTheLimited = errors.New("bug: acquireQuotaLocked detected quota over the limit")
var errQuotaNotAcquired = errors.New("bug: releaseQuotaLocked called with no acquired quota")

type concurrencyLimiter struct {
	config limiter.ConcurrencyConfig

	executions        int
	executionsByScope map[string]int
}

func newConcurrencyLimiter(config limiter.ConcurrencyConfig) *concurrencyLimiter {
	return &concurrencyLimiter{
		config:            config,
		executionsByScope: make(map[string]int),
	}
}

func (l *concurrencyLimiter) acquireQuotaLocked(ctx context.Context) error {
	if l.config.MaxExecutions > 0 && l.executions >= l.config.MaxExecutions {
		if l.executions > l.config.MaxExecutions {
			log.Error(ctx, "global quota over the limited", errQuotaOverTheLimited)
		}
		return fmt.Errorf("max concurrency limit reached")
	}

	if l.config.GetMaxExecutions == nil {
		l.executions++
		return nil
	}

	if scopeKey, scoped := limiter.ScopeFrom(ctx); scoped {
		scopeActive := l.executionsByScope[scopeKey]
		if limit := l.config.GetMaxExecutions(scopeKey); limit > 0 && scopeActive >= limit {
			if scopeActive > limit {
				log.Error(ctx, "scope quota over the limit", errQuotaOverTheLimited, "scope", scopeKey)
			}
			return fmt.Errorf("max scope concurrency limit reached for %q", scopeKey)
		}
		l.executionsByScope[scopeKey] = scopeActive + 1
	}

	l.executions++
	return nil
}

func (l *concurrencyLimiter) releaseQuotaLocked(ctx context.Context) {
	if l.executions > 0 {
		l.executions--
	} else {
		log.Error(ctx, "no global quota to release", errQuotaNotAcquired)
	}

	if l.config.GetMaxExecutions == nil {
		return
	}
	if scopeKey, scoped := limiter.ScopeFrom(ctx); scoped {
		scopedActive := l.executionsByScope[scopeKey]
		if scopedActive == 1 {
			delete(l.executionsByScope, scopeKey)
		} else if scopedActive > 0 {
			l.executionsByScope[scopeKey] = scopedActive - 1
		} else {
			log.Error(ctx, "no scoped quota to release", errQuotaNotAcquired, "scope", scopeKey)
		}
	}
}
