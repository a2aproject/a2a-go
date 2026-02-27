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

// Package sse provides Server-Sent Events (SSE) implementation for A2A.
package sse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/http"

	"github.com/google/uuid"
)

const (
	// ContentEventStream is the MIME type for Server-Sent Events.
	ContentEventStream = "text/event-stream"

	sseIDPrefix   = "id:"
	sseDataPrefix = "data:"

	// MaxSSETokenSize is the maximum size for SSE data lines (10MB).
	// The default bufio.Scanner buffer of 64KB is insufficient for large payloads
	MaxSSETokenSize = 10 * 1024 * 1024 // 10MB
)

// SSEWriter wraps http.ResponseWriter to provide SSE writing capabilities.
type SSEWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

// NewWriter creates a new [SSEWriter].
func NewWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}
	return &SSEWriter{writer: w, flusher: flusher}, nil
}

// WriteHeaders writes the standard SSE headers.
func (w *SSEWriter) WriteHeaders() {
	header := w.writer.Header()
	header.Set("Content-Type", ContentEventStream)
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.writer.WriteHeader(http.StatusOK)
}

// WriteKeepAlive writes an SSE comment to keep the connection alive.
func (w *SSEWriter) WriteKeepAlive(ctx context.Context) error {
	if _, err := w.writer.Write([]byte(": keep-alive\n\n")); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

// WriteData writes a data block to the SSE stream.
func (w *SSEWriter) WriteData(ctx context.Context, data []byte) error {
	eventID := uuid.NewString()
	if _, err := fmt.Fprintf(w.writer, "%s %s\n", sseIDPrefix, []byte(eventID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.writer, "%s %s\n\n", sseDataPrefix, data); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

// ParseDataStream returns an iterator over the data blocks in an SSE stream.
func ParseDataStream(body io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		scanner := bufio.NewScanner(body)
		buf := make([]byte, 0, bufio.MaxScanTokenSize)
		scanner.Buffer(buf, MaxSSETokenSize)
		// Check for "data:" prefix (without space) to support both "data: foo" and "data:foo"
		prefixBytes := []byte(sseDataPrefix)

		for scanner.Scan() {
			lineBytes := scanner.Bytes()
			if bytes.HasPrefix(lineBytes, prefixBytes) {
				data := lineBytes[len(prefixBytes):]
				if len(data) > 0 && data[0] == ' ' {
					data = data[1:]
				}
				if !yield(data, nil) {
					return
				}
			}
			// Ignore empty lines, comments, and other SSE event types
		}
		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("SSE stream error: %w", err))
		}
	}
}
