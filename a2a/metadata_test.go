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
