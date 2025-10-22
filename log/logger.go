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
)

// A Level is the importance or severity of a log event.
// The higher the level, the more important or severe the event.
type Level int32

// Logger provides a minimalistic logging interface.
type Logger interface {
	V(ctx context.Context, level Level) bool
	Verbose(ctx context.Context, level Level, msg string, keyValArgs ...any)
	Info(ctx context.Context, msg string, keyValArgs ...any)
	Error(ctx context.Context, msg string, err error, keyValArgs ...any)
	With(keyValArgs ...any) Logger
}

type loggerKey struct{}

// WithLogger creates a new Context with the provided Logger attached.
func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// LoggerFrom returns the Logger associated with the context, or false if no logger is available.
func LoggerFrom(ctx context.Context) (Logger, bool) {
	logger, ok := ctx.Value(loggerKey{}).(Logger)
	return logger, ok
}

// V invokes V on the Logger associated with the provided Context or returns false if there's no Logger attached.
func V(ctx context.Context, level Level) bool {
	if logger, ok := LoggerFrom(ctx); ok {
		return logger.V(ctx, level)
	}
	return false
}

// Verbose invokes Verbose on the Logger associated with the provided Context or does nothing if there's no Logger attached.
func Verbose(ctx context.Context, level Level, msg string, keyValArgs ...any) {
	if logger, ok := LoggerFrom(ctx); ok {
		logger.Verbose(ctx, level, msg, keyValArgs...)
	}
}

// Info invokes Info on the Logger associated with the provided Context or does nothing if there's no Logger attached.
func Info(ctx context.Context, msg string, keyValArgs ...any) {
	if logger, ok := LoggerFrom(ctx); ok {
		logger.Info(ctx, msg, keyValArgs...)
	}
}

// Error invokes Error on the Logger associated with the provided Context or does nothing if there's no Logger attached.
func Error(ctx context.Context, msg string, err error, keyValArgs ...any) {
	if logger, ok := LoggerFrom(ctx); ok {
		logger.Error(ctx, msg, err, keyValArgs...)
	}
}
