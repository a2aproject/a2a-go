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
	"context"
	"log/slog"
	"time"

	"github.com/a2aproject/a2a-go/v2/log"
)

type loggingStartTimeKey struct{}

// LoggingConfig controls the behavior of the logging [CallInterceptor] created by [NewLoggingInterceptor].
type LoggingConfig struct {
	// Level is the log level for incoming requests. Default: slog.LevelInfo.
	Level slog.Level
	// ErrorLevel is the log level for failed requests. Default: slog.LevelInfo.
	ErrorLevel slog.Level
	// LogPayload enables logging of request and response payloads.
	LogPayload bool
}

type loggingInterceptor struct {
	config LoggingConfig
}

// NewLoggingInterceptor creates a [CallInterceptor] that logs A2A method invocations.
// Incoming requests are logged at the configured level and errors are logged at the error level.
func NewLoggingInterceptor(config *LoggingConfig) CallInterceptor {
	var cfg LoggingConfig
	if config != nil {
		cfg = *config
	}
	return &loggingInterceptor{config: cfg}
}

func (l *loggingInterceptor) Before(ctx context.Context, callCtx *CallContext, req *Request) (context.Context, any, error) {
	method := callCtx.Method()

	attrs := []any{slog.String("method", method)}
	if l.config.LogPayload && req.Payload != nil {
		attrs = append(attrs, slog.Any("payload", req.Payload))
	}

	log.Write(ctx, l.config.Level, "a2a call started", attrs...)

	return context.WithValue(ctx, loggingStartTimeKey{}, time.Now()), nil, nil
}

func (l *loggingInterceptor) After(ctx context.Context, callCtx *CallContext, resp *Response) error {
	attrs := []any{slog.String("method", callCtx.Method())}
	if start, ok := ctx.Value(loggingStartTimeKey{}).(time.Time); ok {
		attrs = append(attrs, slog.Duration("duration_ns", time.Since(start)))
	}

	if resp.Err == nil {
		log.Write(ctx, l.config.Level, "a2a call finished", attrs...)
		return nil
	}

	attrs = append(attrs, slog.String("error", resp.Err.Error()))
	log.Write(ctx, l.config.ErrorLevel, "a2a call failed", attrs...)
	return nil
}
