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

package testutil

import (
	"log/slog"
	"strings"
	"testing"
)

// NewLogger delegates to [NewLevelLogger] passing debug as the minimum level.
func NewLogger(t testing.TB) *slog.Logger {
	return NewLevelLogger(t, slog.LevelDebug)
}

// NewLevelLogger returns an [slog.Logger] that directs all output to t.Log.
// Log statements are printed only in case of a failed text or if go test was invoked with -v flag.
func NewLevelLogger(t testing.TB, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(&tWriter{t: t}, &slog.HandlerOptions{
		Level: level,
	}))
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
