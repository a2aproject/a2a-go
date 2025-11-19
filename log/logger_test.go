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
		want string
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

			tc.call(WithLogger(t.Context(), slog.New(handler)))

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
