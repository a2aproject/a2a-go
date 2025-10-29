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
	sseIDPrefix   = "id: "
	sseDataPrefix = "data: "
)

func ParseDataStream(body io.Reader) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		scanner := bufio.NewScanner(body)
		prefixBytes := []byte(sseDataPrefix)

		for scanner.Scan() {
			lineBytes := scanner.Bytes()
			if bytes.HasPrefix(lineBytes, prefixBytes) {
				data := lineBytes[len(prefixBytes):]
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

type SSEWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

func NewWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}
	return &SSEWriter{flusher: flusher}, nil
}

func (w *SSEWriter) WriteHeaders() {
	w.writer.Header().Set("Cache-Control", "no-cache")
	w.writer.Header().Set("Connection", "keep-alive")
	w.writer.Header().Set("X-Accel-Buffering", "no")
	w.writer.WriteHeader(http.StatusOK)
}

func (w *SSEWriter) WriteKeepAlive(ctx context.Context) error {
	if _, err := w.writer.Write([]byte(": keep-alive\n\n")); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

func (w *SSEWriter) WriteData(ctx context.Context, data []byte) error {
	eventID := uuid.NewString()
	if _, err := fmt.Fprintf(w.writer, "%s%s\n", sseIDPrefix, []byte(eventID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.writer, "%s%s\n\n", sseDataPrefix, data); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}
