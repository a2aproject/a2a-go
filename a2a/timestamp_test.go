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
	"strings"
	"testing"
	"time"
)

func TestNewStatusUpdateEventTimestampIsUTC(t *testing.T) {
	event := NewStatusUpdateEvent(TaskInfo{TaskID: "task-1", ContextID: "ctx-1"}, TaskStateWorking, nil)

	if event.Status.Timestamp == nil {
		t.Fatal("Status.Timestamp is nil")
	}
	if got := event.Status.Timestamp.Location(); got != time.UTC {
		t.Errorf("Status.Timestamp location = %v, want UTC", got)
	}
}

func TestTaskStatusMarshalJSONWritesUTC(t *testing.T) {
	belgrade := time.FixedZone("UTC+2", 2*60*60)
	ts := time.Date(2026, 10, 1, 10, 30, 0, 0, belgrade)

	testCases := []struct {
		name   string
		status any
	}{
		{name: "value", status: TaskStatus{State: TaskStateWorking, Timestamp: &ts}},
		{name: "pointer", status: &TaskStatus{State: TaskStateWorking, Timestamp: &ts}},
		{name: "inside task", status: &Task{ID: "task-1", ContextID: "ctx-1", Status: TaskStatus{State: TaskStateWorking, Timestamp: &ts}}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.status)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			got := string(data)
			if !strings.Contains(got, `"timestamp":"2026-10-01T08:30:00Z"`) {
				t.Errorf("json.Marshal() = %s, want timestamp 2026-10-01T08:30:00Z", got)
			}
		})
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
