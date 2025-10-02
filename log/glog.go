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

package log

import (
	"context"
	"slices"

	"github.com/golang/glog"
)

type glogLogger struct {
	scopeAttrs []any
}

// Glog creates a new Logger implementation based on github.com/golang/glog.
func Glog() Logger {
	return &glogLogger{}
}

func (*glogLogger) V(_ context.Context, level Level) bool {
	return bool(glog.V(glog.Level(level)))
}

func (l *glogLogger) Verbose(ctx context.Context, level Level, msg string, keyValArgs ...any) {
	if glog.V(glog.Level(level)) {
		l.Info(ctx, msg, keyValArgs...)
	}
}

func (l *glogLogger) Info(ctx context.Context, msg string, keyValArgs ...any) {
	glog.InfoContext(ctx, slices.Concat([]any{msg}, keyValArgs, l.scopeAttrs)...)
}

func (l *glogLogger) Error(ctx context.Context, msg string, err error, keyValArgs ...any) {
	glog.ErrorContext(ctx, slices.Concat([]any{msg, "error", err}, keyValArgs, l.scopeAttrs)...)
}

func (l *glogLogger) With(keyValArgs ...any) Logger {
	return &glogLogger{scopeAttrs: slices.Concat(keyValArgs, l.scopeAttrs)}
}
