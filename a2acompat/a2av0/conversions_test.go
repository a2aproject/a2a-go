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

package a2av0

import (
	"reflect"
	"testing"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/v1/a2a"
)

func TestToCompatParts_PrimitiveData(t *testing.T) {
	val := "hello"
	parts := a2a.ContentParts{a2a.NewDataPart(val)}
	compatParts := FromV1Parts(parts)

	if len(compatParts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(compatParts))
	}

	dp, ok := compatParts[0].(a2alegacy.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", compatParts[0])
	}

	// Verify it's wrapped in a map
	m := dp.Data

	if m["value"] != val {
		t.Errorf("Expected value %q, got %v", val, m["value"])
	}

	// Verify metadata flag
	if compat, ok := dp.Metadata["data_part_compat"].(bool); !ok || !compat {
		t.Errorf("Expected data_part_compat=true in metadata")
	}
}

func TestToCoreParts_PrimitiveDataUnwrap(t *testing.T) {
	val := "hello"
	compatParts := a2alegacy.ContentParts{
		a2alegacy.DataPart{
			Data:     map[string]any{"value": val},
			Metadata: map[string]any{"data_part_compat": true},
		},
	}

	coreParts, err := ToV1Parts(compatParts)
	if err != nil {
		t.Fatalf("toCoreParts failed: %v", err)
	}

	if len(coreParts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(coreParts))
	}

	dataVal := coreParts[0].Data()
	if dataVal != val {
		t.Errorf("Expected data value %q, got %v", val, dataVal)
	}

	// Verify metadata flag is removed (optional but good practice)
	if _, ok := coreParts[0].Metadata["data_part_compat"]; ok {
		t.Errorf("Expected data_part_compat to be removed from metadata")
	}
}

func TestToCompatParts_MapDataNoWrap(t *testing.T) {
	val := map[string]any{"key": "value"}
	parts := a2a.ContentParts{a2a.NewDataPart(val)}
	compatParts := FromV1Parts(parts)

	if len(compatParts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(compatParts))
	}

	dp, ok := compatParts[0].(a2alegacy.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", compatParts[0])
	}

	// Verify it's NOT wrapped
	if !reflect.DeepEqual(dp.Data, val) {
		t.Errorf("Expected data %v, got %v", val, dp.Data)
	}

	// Verify metadata flag is NOT present
	if _, ok := dp.Metadata["data_part_compat"]; ok {
		t.Errorf("Expected data_part_compat to NOT be present")
	}
}
