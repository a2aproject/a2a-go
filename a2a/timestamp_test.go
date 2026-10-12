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

package a2a

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

var specTimestamp = regexp.MustCompile(`"timestamp":"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z"`)

func TestNewStatusUpdateEventTimestampIsUTC(t *testing.T) {
	event := NewStatusUpdateEvent(TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}, TaskStateWorking, nil)

	if event.Status.Timestamp == nil {
		t.Fatal("Status.Timestamp is nil")
	}
	if got := event.Status.Timestamp.Location(); got != time.UTC {
		t.Errorf("Status.Timestamp location = %v, want UTC", got)
	}
	if got := event.Status.Timestamp; !got.Equal(got.Truncate(time.Millisecond)) {
		t.Errorf("Status.Timestamp = %v, want millisecond precision", got)
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !specTimestamp.Match(data) {
		t.Errorf("json.Marshal() = %s, want timestamp as YYYY-MM-DDTHH:mm:ss.sssZ", data)
	}

	var got TaskStatusUpdateEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.Status.Timestamp == nil || !got.Status.Timestamp.Equal(*event.Status.Timestamp) {
		t.Errorf("round trip timestamp = %v, want %v", got.Status.Timestamp, event.Status.Timestamp)
	}
}

func TestTaskStatusMarshalJSONWritesSpecFormat(t *testing.T) {
	belgrade := time.FixedZone("UTC+2", 2*60*60)

	testCases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{name: "whole second", ts: time.Date(2026, 10, 1, 10, 30, 0, 0, belgrade), want: "2026-10-01T08:30:00.000Z"},
		{name: "milliseconds", ts: time.Date(2026, 10, 1, 10, 30, 0, 142000000, belgrade), want: "2026-10-01T08:30:00.142Z"},
		{name: "nanoseconds", ts: time.Date(2026, 10, 1, 6, 10, 55, 114002345, time.UTC), want: "2026-10-01T06:10:55.114Z"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wantField := `"timestamp":"` + tc.want + `"`
			for name, v := range map[string]any{
				"value":       TaskStatus{State: TaskStateWorking, Timestamp: &tc.ts},
				"pointer":     &TaskStatus{State: TaskStateWorking, Timestamp: &tc.ts},
				"inside task": &Task{ID: "task-1", ContextID: "ctx-1", Status: TaskStatus{State: TaskStateWorking, Timestamp: &tc.ts}},
			} {
				data, err := json.Marshal(v)
				if err != nil {
					t.Fatalf("%s: json.Marshal() error = %v", name, err)
				}
				if got := string(data); !strings.Contains(got, wantField) {
					t.Errorf("%s: json.Marshal() = %s, want %s", name, got, wantField)
				}
			}
		})
	}
}

func TestTaskStatusMarshalJSONOmitsNilTimestamp(t *testing.T) {
	data, err := json.Marshal(TaskStatus{State: TaskStateWorking})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if got, want := string(data), `{"state":"TASK_STATE_WORKING"}`; got != want {
		t.Errorf("json.Marshal() = %s, want %s", got, want)
	}
}

func TestTaskStatusMarshalJSONRejectsYearOutOfRange(t *testing.T) {
	ts := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := json.Marshal(TaskStatus{State: TaskStateWorking, Timestamp: &ts}); err == nil {
		t.Error("json.Marshal() error = nil, want error for year 10000")
	}
}

func TestTaskStatusMarshalJSONKeepsCallerTimestamp(t *testing.T) {
	belgrade := time.FixedZone("UTC+2", 2*60*60)
	ts := time.Date(2026, 10, 1, 10, 30, 0, 0, belgrade)
	status := TaskStatus{State: TaskStateWorking, Timestamp: &ts}

	if _, err := json.Marshal(status); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if ts.Location() != belgrade {
		t.Errorf("caller timestamp location changed to %v", ts.Location())
	}
}

func TestTaskStatusUnmarshalJSONAcceptsAnyPrecision(t *testing.T) {
	want := time.Date(2026, 10, 1, 8, 30, 0, 0, time.UTC)

	testCases := []struct {
		name string
		ts   string
		want time.Time
	}{
		{name: "no fraction", ts: "2026-10-01T08:30:00Z", want: want},
		{name: "3 digits", ts: "2026-10-01T08:30:00.123Z", want: want.Add(123 * time.Millisecond)},
		{name: "6 digits", ts: "2026-10-01T08:30:00.123456Z", want: want.Add(123456 * time.Microsecond)},
		{name: "9 digits", ts: "2026-10-01T08:30:00.123456789Z", want: want.Add(123456789 * time.Nanosecond)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var got TaskStatus
			if err := json.Unmarshal([]byte(`{"state":"TASK_STATE_COMPLETED","timestamp":"`+tc.ts+`"}`), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got.Timestamp == nil || !got.Timestamp.Equal(tc.want) {
				t.Errorf("json.Unmarshal() timestamp = %v, want %v", got.Timestamp, tc.want)
			}
		})
	}
}

func TestTaskStatusJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 10, 1, 8, 30, 0, 123000000, time.UTC)
	want := TaskStatus{State: TaskStateCompleted, Timestamp: &ts}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got TaskStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.State != want.State || got.Timestamp == nil || !got.Timestamp.Equal(ts) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestListTasksRequestMarshalJSONWritesSpecFormat(t *testing.T) {
	after := time.Date(2026, 10, 1, 10, 30, 0, 114002345, time.FixedZone("UTC+2", 2*60*60))
	historyLength := 5

	data, err := json.Marshal(&ListTasksRequest{ContextID: "ctx-1", HistoryLength: &historyLength, StatusTimestampAfter: &after})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"contextId":"ctx-1","historyLength":5,"statusTimestampAfter":"2026-10-01T08:30:00.114Z"}`
	if got := string(data); got != want {
		t.Errorf("json.Marshal() = %s, want %s", got, want)
	}

	var got ListTasksRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.StatusTimestampAfter == nil || !got.StatusTimestampAfter.Equal(after.Truncate(time.Millisecond)) {
		t.Errorf("json.Unmarshal() statusTimestampAfter = %v, want %v", got.StatusTimestampAfter, after.Truncate(time.Millisecond))
	}
}

func TestListTasksRequestMarshalJSONOmitsNilTimestamp(t *testing.T) {
	data, err := json.Marshal(ListTasksRequest{ContextID: "ctx-1"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if got, want := string(data), `{"contextId":"ctx-1"}`; got != want {
		t.Errorf("json.Marshal() = %s, want %s", got, want)
	}
}
