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

package a2asrv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/testutil"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestErrorSanitizer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		storeErr    error
		verbose     bool
		wantErr     error
		wantMsg     string
		wantLogLine string
	}{
		{
			name:        "a2a error loses its wrapping",
			storeErr:    fmt.Errorf("db: %w", a2a.NewError(a2a.ErrUnauthorized, "no access")),
			wantErr:     a2a.ErrUnauthorized,
			wantMsg:     "no access",
			wantLogLine: "level=INFO",
		},
		{
			name:        "a2a error with verbose errors",
			storeErr:    fmt.Errorf("db: %w", a2a.NewError(a2a.ErrUnauthorized, "no access")),
			verbose:     true,
			wantErr:     a2a.ErrUnauthorized,
			wantMsg:     "failed to load a task: db: no access",
			wantLogLine: "level=INFO",
		},
		{
			name:        "caller error sentinel",
			storeErr:    fmt.Errorf("bad id: %w", a2a.ErrInvalidParams),
			wantErr:     a2a.ErrInvalidParams,
			wantMsg:     "failed to load a task: bad id: invalid params",
			wantLogLine: "level=INFO",
		},
		{
			name:        "context error",
			storeErr:    context.Canceled,
			wantErr:     context.Canceled,
			wantMsg:     "failed to load a task: context canceled",
			wantLogLine: "level=INFO",
		},
		{
			name:        "internal error",
			storeErr:    errors.New("db down"),
			wantErr:     a2a.ErrInternalError,
			wantMsg:     "internal error",
			wantLogLine: "level=ERROR",
		},
		{
			name:        "internal error with verbose errors",
			storeErr:    errors.New("db down"),
			verbose:     true,
			wantMsg:     "failed to load a task: db down",
			wantLogLine: "level=ERROR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := &syncBuffer{}
			opts := []RequestHandlerOption{
				WithTaskStore(testutil.NewTestTaskStore().SetGetOverride(nil, tc.storeErr)),
				WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
			}
			if tc.verbose {
				opts = append(opts, WithVerboseErrors())
			}
			handler := newTestHandler(opts...)

			_, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: a2a.NewTaskID()})

			if err == nil || err.Error() != tc.wantMsg {
				t.Fatalf("GetTask() error = %v, want %q", err, tc.wantMsg)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetTask() error = %v, want %v", err, tc.wantErr)
			}
			logged := logs.String()
			if !strings.Contains(logged, tc.wantLogLine) || !strings.Contains(logged, tc.storeErr.Error()) || !strings.Contains(logged, "failed to load a task") {
				t.Fatalf("GetTask() logs = %q, want a %s line with the full error", logged, tc.wantLogLine)
			}
		})
	}
}

func TestErrorSanitizer_VerboseErrorsEnv(t *testing.T) {
	t.Setenv(VerboseErrorsEnv, "true")
	handler := newTestHandler(WithTaskStore(testutil.NewTestTaskStore().SetGetOverride(nil, errors.New("db down"))))

	_, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: a2a.NewTaskID()})

	if want := "failed to load a task: db down"; err == nil || err.Error() != want {
		t.Fatalf("GetTask() error = %v, want %q", err, want)
	}
}
