package limiter

import (
	"context"
)

type limiterScopeKeyType struct{}

var limiterScopeKey = limiterScopeKeyType{}

type ConcurrencyConfig struct {
	MaxExecutions    int
	GetMaxExecutions func(scope string) int
}

func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, limiterScopeKey, scope)
}

func ScopeFrom(ctx context.Context) (string, bool) {
	scope, ok := ctx.Value(limiterScopeKey).(string)
	return scope, ok
}
