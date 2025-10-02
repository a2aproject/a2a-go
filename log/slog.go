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
	"log/slog"
)

type slogLogger struct {
	logger *slog.Logger
}

// FromSlog creates a new Logger implementation backed by the provided log/slog logger.
func FromSlog(logger *slog.Logger) Logger {
	return &slogLogger{logger}
}

func (s *slogLogger) V(ctx context.Context, level Level) bool {
	return s.logger.Enabled(context.Background(), slog.Level(level))
}

func (s *slogLogger) Verbose(ctx context.Context, level Level, msg string, keyValArgs ...any) {
	if s.logger.Enabled(ctx, slog.Level(level)) {
		s.logger.InfoContext(ctx, msg, keyValArgs...)
	}
}

func (s *slogLogger) Info(ctx context.Context, msg string, keyValArgs ...any) {
	s.logger.InfoContext(ctx, msg, keyValArgs...)
}

func (s *slogLogger) Error(ctx context.Context, msg string, err error, keyValArgs ...any) {
	s.logger.ErrorContext(ctx, msg, append([]any{"error", err.Error()}, keyValArgs...)...)
}

func (s *slogLogger) With(keyValArgs ...any) Logger {
	return &slogLogger{logger: s.logger.With(keyValArgs...)}
}
