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
		{"Part", &Part{}},
		{"SendMessageRequest", &SendMessageRequest{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.carrier.SetMeta("key", "value")

			got := tc.carrier.Meta()
			if val, ok := got["key"]; !ok || val != "value" {
				t.Errorf("Meta() = %v, want map containing key=value", got)
			}
		})
	}
}
