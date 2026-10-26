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

package a2aacp

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type closeRecorder struct {
	io.ReadWriter
	calls *[]string
	err   error
}

func (c closeRecorder) Close() error {
	*c.calls = append(*c.calls, "close")
	return c.err
}

func TestWithCleanup(t *testing.T) {
	t.Parallel()
	closeErr, cleanupErr := errors.New("close failed"), errors.New("cleanup failed")
	var calls []string
	rwc := WithCleanup(closeRecorder{calls: &calls, err: closeErr}, func() error {
		calls = append(calls, "cleanup")
		return cleanupErr
	})

	first, second := rwc.Close(), rwc.Close()

	if !errors.Is(first, closeErr) || !errors.Is(first, cleanupErr) {
		t.Fatalf("WithCleanup().Close() error = %v, want %v and %v", first, closeErr, cleanupErr)
	}
	if second != first {
		t.Fatalf("WithCleanup().Close() second error = %v, want %v", second, first)
	}
	if diff := cmp.Diff([]string{"close", "cleanup"}, calls); diff != "" {
		t.Fatalf("WithCleanup().Close() wrong calls (-want +got) diff = %s", diff)
	}
}

func TestProfileCommand(t *testing.T) {
	t.Parallel()
	got, err := ProfileCommand("opencode")
	if err != nil {
		t.Fatalf("ProfileCommand() error = %v", err)
	}
	if diff := cmp.Diff([]string{"opencode", "acp"}, got); diff != "" {
		t.Fatalf("ProfileCommand() wrong result (-want +got) diff = %s", diff)
	}
	if _, err := ProfileCommand("unknown"); err == nil || !strings.Contains(err.Error(), `unknown profile "unknown"`) {
		t.Fatalf("ProfileCommand(unknown) error = %v, want unknown profile", err)
	}
}
