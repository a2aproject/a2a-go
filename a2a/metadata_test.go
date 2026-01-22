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
	"reflect"
	"testing"
)

func TestMetadataCarrierImplementation(t *testing.T) {
	tests := []struct {
		name    string
		carrier MetadataCarrier
	}{
		{"Message", &Message{}},
		{"Task", &Task{}},
		{"Artifact", &Artifact{}},
		{"TaskArtifactUpdateEvent", &TaskArtifactUpdateEvent{}},
		{"TaskStatusUpdateEvent", &TaskStatusUpdateEvent{}},
		{"TextPart", &TextPart{}},
		{"DataPart", &DataPart{}},
		{"FilePart", &FilePart{}},
		{"TaskIDParams", &TaskIDParams{}},
		{"TaskQueryParams", &TaskQueryParams{}},
		{"MessageSendParams", &MessageSendParams{}},
		{"GetTaskPushConfigParams", &GetTaskPushConfigParams{}},
		{"ListTaskPushConfigParams", &ListTaskPushConfigParams{}},
		{"DeleteTaskPushConfigParams", &DeleteTaskPushConfigParams{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{"key": "value"}
			tc.carrier.SetMeta(meta)

			got := tc.carrier.Meta()
			if !reflect.DeepEqual(got, meta) {
				t.Errorf("Meta() = %v, want %v", got, meta)
			}
		})
	}
}
