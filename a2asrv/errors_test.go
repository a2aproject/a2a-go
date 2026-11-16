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
		name      string
		storeErr  error
		wantErr   error
		wantMsg   string
		wantLevel string
	}{
		{
			name:      "a2a error loses its wrapping",
			storeErr:  fmt.Errorf("db: %w", a2a.NewError(a2a.ErrUnauthorized, "no access")),
			wantErr:   a2a.ErrUnauthorized,
			wantMsg:   "no access",
			wantLevel: "level=INFO",
		},
		{
			name:      "wrapped caller error",
			storeErr:  fmt.Errorf("bad id: %w", a2a.ErrInvalidParams),
			wantErr:   a2a.ErrInvalidParams,
			wantMsg:   "invalid params",
			wantLevel: "level=INFO",
		},
		{
			name:      "task not found",
			storeErr:  a2a.ErrTaskNotFound,
			wantErr:   a2a.ErrTaskNotFound,
			wantMsg:   "task not found",
			wantLevel: "level=INFO",
		},
		{
			name:      "context error",
			storeErr:  context.Canceled,
			wantErr:   context.Canceled,
			wantMsg:   "context canceled",
			wantLevel: "level=INFO",
		},
		{
			name:      "internal error",
			storeErr:  errors.New("db down"),
			wantErr:   a2a.ErrInternalError,
			wantMsg:   "internal error",
			wantLevel: "level=ERROR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logs := &syncBuffer{}
			handler := newTestHandler(
				WithTaskStore(testutil.NewTestTaskStore().SetGetOverride(nil, tc.storeErr)),
				WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
				WithCallInterceptors(NewErrorSanitizer()),
			)

			_, err := handler.GetTask(t.Context(), &a2a.GetTaskRequest{ID: a2a.NewTaskID()})

			if !errors.Is(err, tc.wantErr) || err.Error() != tc.wantMsg {
				t.Fatalf("GetTask() error = %v, want %q wrapping %v", err, tc.wantMsg, tc.wantErr)
			}
			logged := logs.String()
			if wantLogged := "failed to get task: " + tc.storeErr.Error(); !strings.Contains(logged, tc.wantLevel) || !strings.Contains(logged, wantLogged) {
				t.Fatalf("GetTask() logs = %q, want a %s line with %q", logged, tc.wantLevel, wantLogged)
			}
		})
	}
}
