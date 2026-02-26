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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestLogAddSource(t *testing.T) {
	wantFile, wantFunc := "logger_test.go", "TestLogAddSource"

	testCases := []struct {
		name string
		call func(context.Context)
	}{
		{
			name: "Log",
			call: func(ctx context.Context) { Log(ctx, slog.LevelInfo, "hello") },
		},
		{
			name: "Info",
			call: func(ctx context.Context) { Info(ctx, "hello") },
		},
		{
			name: "Warn",
			call: func(ctx context.Context) { Warn(ctx, "hello") },
		},
		{
			name: "Error",
			call: func(ctx context.Context) { Error(ctx, "hello", fmt.Errorf("fail")) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: true})

			tc.call(AttachLogger(t.Context(), slog.New(handler)))

			var written map[string]any
			if err := json.Unmarshal(buf.Bytes(), &written); err != nil {
				t.Fatalf("json.Unmarshal() error = %v, for %q", err, buf.String())
			}

			source := written["source"].(map[string]any)
			if file := source["file"].(string); !strings.HasSuffix(file, wantFile) {
				t.Fatalf("logged source path %q, want file %q", source, wantFile)
			}
			if funcName := source["function"].(string); !strings.Contains(funcName, wantFunc) {
				t.Fatalf("logged source function %q, want containing %q", source, wantFunc)
			}
		})
	}

}
