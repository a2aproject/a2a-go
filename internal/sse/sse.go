package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/log"
)

// CloseEventData represents the data payload for a close event.
// Used when formatting SSE messages indicating stream closure.
type CloseEventData struct {
	ID     string `json:"taskId"`
	Reason string `json:"reason"`
}

// EventReader helps parse text/event-stream formatted data.
type EventReader struct {
	scanner *bufio.Scanner
}

// NewEventReader creates a new reader for SSE events.
// Exported function.
func NewEventReader(r io.Reader) *EventReader {
	scanner := bufio.NewScanner(r)
	return &EventReader{scanner: scanner}
}

// ReadEvent reads the next complete event from the stream.
// It returns the event data, event type, and any error (including io.EOF).
// Exported method.
func (r *EventReader) ReadEvent() (data []byte, eventType string, err error) {
	dataBuffer := bytes.Buffer{}
	eventType = "message" // Default event type per SSE spec.
	for r.scanner.Scan() {
		line := r.scanner.Bytes()
		if len(line) == 0 {
			// Empty line signifies end of event.
			if dataBuffer.Len() > 0 {
				// We have data, return the completed event.
				// Remove the last newline added by the loop.
				d := dataBuffer.Bytes()
				if len(d) > 0 && d[len(d)-1] == '\n' {
					d = d[:len(d)-1]
				}
				return d, eventType, nil
			}
			// Double newline without data is just a keep-alive tick, ignore.
			continue
		}
		// Process field lines (e.g., "event: ", "data: ", "id: ", "retry: ").
		if bytes.HasPrefix(line, []byte("event:")) {
			eventType = string(bytes.TrimSpace(line[len("event:"):]))
		} else if bytes.HasPrefix(line, []byte("data:")) {
			// Append data field, preserving newlines within the data.
			dataChunk := line[len("data:"):]
			if len(dataChunk) > 0 && dataChunk[0] == ' ' {
				dataChunk = dataChunk[1:] // Trim leading space if present.
			}
			dataBuffer.Write(dataChunk)
			dataBuffer.WriteByte('\n') // Add newline between data chunks.
		} else if bytes.HasPrefix(line, []byte("id:")) {
			// Store or process last event ID (optional, ignored here).
		} else if bytes.HasPrefix(line, []byte("retry:")) {
			// Store or process retry timeout (optional, ignored here).
		} else if bytes.HasPrefix(line, []byte(":")) {
			// Comment line, ignore.
		} else {
			// Lines without a field prefix might be invalid per spec,
			// but some implementations might just treat them as data.
			// For robustness, let's treat it as data.
			log.Warnf("SSE line without recognized prefix: %s", string(line))
			dataBuffer.Write(line)
			dataBuffer.WriteByte('\n')
		}
	}
	// Scanner finished, check for errors.
	if err := r.scanner.Err(); err != nil {
		return nil, "", err
	}
	// Check if there was remaining data when EOF was hit without a final newline.
	if dataBuffer.Len() > 0 {
		d := dataBuffer.Bytes()
		if len(d) > 0 && d[len(d)-1] == '\n' {
			d = d[:len(d)-1]
		}
		return d, eventType, io.EOF // Return data with EOF.
	}
	return nil, "", io.EOF // Normal EOF.
}

// FormatEvent marshals the given data to JSON and writes it to the writer
// in the standard SSE format (event: type\\ndata: json\\n\\n).
// It handles potential JSON marshaling errors.
// Exported function.
func FormatEvent(w io.Writer, eventType string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE event data: %w", err)
	}
	// Format according to text/event-stream specification.
	// event: <eventType>
	// data: <jsonData>
	// <empty line>
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}
	return nil
}

// FormatJSONRPCEvent marshals the given data as a JSON-RPC response and writes
// it to the writer in SSE format. The event type is used as the SSE event type.
// The id parameter allows correlation with the original request.
// It handles potential JSON marshaling errors.
// Exported function.
func FormatJSONRPCEvent(w io.Writer, eventType string, id interface{}, data interface{}) error {
	// Create a JSON-RPC response with the data as the result
	response := jsonrpc.NewNotificationResponse(id, data)
	// Marshal the entire JSON-RPC envelope
	jsonData, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON-RPC SSE event data: %w", err)
	}
	// Format according to text/event-stream specification
	// event: <eventType>
	// data: <jsonrpc_envelope>
	// <empty line>
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write JSON-RPC SSE event: %w", err)
	}
	return nil
}
