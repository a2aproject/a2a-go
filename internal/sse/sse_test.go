package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/stretchr/testify/assert"
)

func TestNewEventReader(t *testing.T) {
	r := strings.NewReader("test data")
	er := NewEventReader(r)
	if er == nil {
		t.Fatal("Expected non-nil EventReader.")
	}
	if er.scanner == nil {
		t.Fatal("Expected non-nil scanner in EventReader.")
	}
}

func TestReadEvent(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedData  string
		expectedType  string
		expectedError error
		expectNoEvent bool
	}{
		{
			name:          "simple event",
			input:         "data: test data\n\n",
			expectedData:  "test data",
			expectedType:  "message",
			expectedError: nil,
		},
		{
			name:          "event with custom type",
			input:         "event: custom\ndata: test data\n\n",
			expectedData:  "test data",
			expectedType:  "custom",
			expectedError: nil,
		},
		{
			name:          "event with multiple data lines",
			input:         "data: line1\ndata: line2\n\n",
			expectedData:  "line1\nline2",
			expectedType:  "message",
			expectedError: nil,
		},
		{
			name:          "event with id and comments",
			input:         "id: 123\n:comment\ndata: test data\n\n",
			expectedData:  "test data",
			expectedType:  "message",
			expectedError: nil,
		},
		{
			name:          "empty data",
			input:         "data:\n\n",
			expectedData:  "",
			expectedType:  "message",
			expectedError: nil,
		},
		{
			name:          "keep-alive tick",
			input:         "\n\n",
			expectNoEvent: true,
		},
		{
			name:          "data with EOF without final newline",
			input:         "data: test data",
			expectedData:  "test data",
			expectedType:  "message",
			expectedError: io.EOF,
		},
		{
			name:          "empty input",
			input:         "",
			expectedError: io.EOF,
			expectNoEvent: true,
		},
		{
			name:          "unrecognized line prefix",
			input:         "unknown: value\ndata: test\n\n",
			expectedData:  "unknown: value\ntest",
			expectedType:  "message",
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			er := NewEventReader(r)

			data, eventType, err := er.ReadEvent()

			if tt.expectNoEvent {
				if err != io.EOF {
					t.Errorf("Expected EOF for keep-alive tick, got %v.", err)
				}
				return
			}

			if (err == nil && tt.expectedError != nil) || (err != nil && tt.expectedError == nil) ||
				(err != nil && tt.expectedError != nil && err.Error() != tt.expectedError.Error()) {
				t.Errorf("Expected error %v, got %v.", tt.expectedError, err)
			}

			if string(data) != tt.expectedData {
				t.Errorf("Expected data %q, got %q.", tt.expectedData, string(data))
			}

			if eventType != tt.expectedType {
				t.Errorf("Expected event type %q, got %q.", tt.expectedType, eventType)
			}
		})
	}
}

func TestFormatEvent(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		data       interface{}
		expected   string
		shouldFail bool
	}{
		{
			name:      "simple string data",
			eventType: "message",
			data:      "test data",
			expected:  "event: message\ndata: \"test data\"\n\n",
		},
		{
			name:      "struct data",
			eventType: "close",
			data:      CloseEventData{ID: "123", Reason: "completed"},
			expected:  "event: close\ndata: {\"taskId\":\"123\",\"reason\":\"completed\"}\n\n",
		},
		{
			name:       "marshal error",
			eventType:  "error",
			data:       make(chan int), // Cannot be marshaled to JSON
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := FormatEvent(&buf, tt.eventType, tt.data)

			if tt.shouldFail {
				if err == nil {
					t.Error("Expected error, got nil.")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v.", err)
				return
			}

			if buf.String() != tt.expected {
				t.Errorf("Expected output %q, got %q.", tt.expected, buf.String())
			}
		})
	}
}

func TestReadEventSequence(t *testing.T) {
	// Test reading multiple events in sequence
	input := "event: first\ndata: event1\n\nevent: second\ndata: event2\n\n"
	r := strings.NewReader(input)
	er := NewEventReader(r)

	// Read first event
	data1, type1, err1 := er.ReadEvent()
	if err1 != nil {
		t.Fatalf("Unexpected error reading first event: %v.", err1)
	}
	if string(data1) != "event1" {
		t.Errorf("Expected data %q, got %q.", "event1", string(data1))
	}
	if type1 != "first" {
		t.Errorf("Expected event type %q, got %q.", "first", type1)
	}

	// Read second event
	data2, type2, err2 := er.ReadEvent()
	if err2 != nil {
		t.Fatalf("Unexpected error reading second event: %v.", err2)
	}
	if string(data2) != "event2" {
		t.Errorf("Expected data %q, got %q.", "event2", string(data2))
	}
	if type2 != "second" {
		t.Errorf("Expected event type %q, got %q.", "second", type2)
	}

	// Should be at EOF now
	_, _, err3 := er.ReadEvent()
	if err3 != io.EOF {
		t.Errorf("Expected EOF, got %v.", err3)
	}
}

func TestCloseEventDataMarshaling(t *testing.T) {
	closeData := CloseEventData{
		ID:     "task123",
		Reason: "test completed",
	}

	jsonBytes, err := json.Marshal(closeData)
	if err != nil {
		t.Fatalf("Failed to marshal CloseEventData: %v.", err)
	}

	var unmarshaled CloseEventData
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal CloseEventData: %v.", err)
	}

	if unmarshaled.ID != closeData.ID {
		t.Errorf("Expected TaskID %q, got %q.", closeData.ID, unmarshaled.ID)
	}

	if unmarshaled.Reason != closeData.Reason {
		t.Errorf("Expected Reason %q, got %q.", closeData.Reason, unmarshaled.Reason)
	}
}

func TestFormatJSONRPCEvent(t *testing.T) {
	// Test data
	eventType := "test_event"
	eventID := "test-request-123"
	eventData := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	// Buffer to capture the output
	var buf bytes.Buffer

	// Format the event
	err := FormatJSONRPCEvent(&buf, eventType, eventID, eventData)
	assert.NoError(t, err, "FormatJSONRPCEvent should not return an error")

	// Get the formatted output
	output := buf.String()

	// Verify the SSE format structure
	assert.Contains(t, output, "event: test_event", "Output should contain the event type")
	assert.Contains(t, output, "data: {", "Output should contain JSON data")
	assert.Contains(t, output, "\"jsonrpc\":\"2.0\"", "Output should contain JSON-RPC version")
	assert.Contains(t, output, "\"id\":\"test-request-123\"", "Output should contain the request ID")

	// Verify the content can be parsed back
	scanner := bufio.NewScanner(strings.NewReader(output))
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimPrefix(line, "data: ")
			break
		}
	}

	// Parse the JSON-RPC response
	var response jsonrpc.Response
	err = json.Unmarshal([]byte(dataLine), &response)
	assert.NoError(t, err, "Should be able to unmarshal the JSON-RPC response")

	// Check response structure
	assert.Equal(t, "2.0", response.JSONRPC, "JSONRPC version should be 2.0")
	assert.Equal(t, eventID, response.ID, "ID should match the provided request ID")

	// Verify result contains the same key-value pairs
	// JSON unmarshaling creates map[string]interface{}, so we can't use direct equality
	resultMap, ok := response.Result.(map[string]interface{})
	assert.True(t, ok, "Result should be a map")
	assert.Equal(t, "value1", resultMap["key1"], "Value for key1 should match")
	assert.Equal(t, "value2", resultMap["key2"], "Value for key2 should match")
}
