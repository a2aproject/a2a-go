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

// Package testlogger provides test-scoped slog loggers that direct output to testing.T.
package testlogger

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/log"
)

// AttachToContext returns a new context with a test logger attached.
func AttachToContext(t testing.TB) context.Context {
	return log.AttachLogger(t.Context(), New(t))
}

// New delegates to [NewLeveled] passing debug as the minimum level.
func New(t testing.TB) *slog.Logger {
	return NewLeveled(t, slog.LevelDebug)
}

// NewLeveled returns an [slog.Logger] that directs all output to t.Log.
// Log statements are printed only in case of a failed test or if go test was invoked with -v flag.
func NewLeveled(t testing.TB, level slog.Level) *slog.Logger {
	handler := slog.New(slog.NewTextHandler(&tWriter{t: t}, &slog.HandlerOptions{
		Level: level,
	}))
	return handler.With("test_name", t.Name())
}

type tWriter struct {
	t testing.TB
}

// Write implements io.Writer.
func (w *tWriter) Write(p []byte) (n int, err error) {
	w.t.Helper()
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
