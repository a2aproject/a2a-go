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
//
// Per the WHATWG SSE spec, an event ends at a blank line and consecutive
// "data:" fields belonging to the same event are joined by a "\n"
// separator. This parser therefore accumulates "data:" values until a blank
// line (or EOF) and yields the joined payload as one event.
func ParseDataStream(body io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		scanner := bufio.NewScanner(body)
		buf := make([]byte, 0, bufio.MaxScanTokenSize)
		scanner.Buffer(buf, MaxSSETokenSize)
		prefixBytes := []byte(sseDataPrefix)

		var event bytes.Buffer
		flush := func() bool {
			if event.Len() == 0 {
				return true
			}
			data := append([]byte(nil), event.Bytes()...)
			event.Reset()
			return yield(data, nil)
		}

		for scanner.Scan() {
			lineBytes := scanner.Bytes()
			if len(lineBytes) == 0 {
				if !flush() {
					return
				}
				continue
			}
			if !bytes.HasPrefix(lineBytes, prefixBytes) {
				continue
			}
			data := lineBytes[len(prefixBytes):]
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			if event.Len() > 0 {
				event.WriteByte('\n')
			}
			event.Write(data)
		}
		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("SSE stream error: %w", err))
			return
		}
		flush()
	}
}
