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

package a2acrypto

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// A REQUIRED repeated field must appear as an empty list rather than null. A nil
// slice marshals as null, which is not the Protocol Buffers JSON representation of
// an empty repeated field, and the canonical form is what signatures are computed
// over, so the value must be the one a conforming reader produces.
func TestCanonicalPayloadRequiredRepeatedFieldsAreLists(t *testing.T) {
	t.Parallel()

	card := &a2a.AgentCard{Name: "Example Agent"}
	payload, err := canonicalPayload(card)
	if err != nil {
		t.Fatalf("canonicalPayload() error = %v, want nil", err)
	}

	got := string(payload)
	for _, field := range requiredRepeatedFields {
		if strings.Contains(got, `"`+field+`":null`) {
			t.Errorf("canonical payload carries null for %s, want a list: %s", field, got)
		}
		if !strings.Contains(got, `"`+field+`":[]`) {
			t.Errorf("canonical payload does not carry an empty list for %s: %s", field, got)
		}
	}
}

// A populated REQUIRED repeated field keeps its entries and its order.
func TestCanonicalPayloadRequiredRepeatedFieldsKeepEntries(t *testing.T) {
	t.Parallel()

	card := &a2a.AgentCard{
		Name: "Example Agent",
		Skills: []a2a.AgentSkill{
			{ID: "skill-1", Name: "first"},
			{ID: "skill-2", Name: "second"},
		},
	}
	payload, err := canonicalPayload(card)
	if err != nil {
		t.Fatalf("canonicalPayload() error = %v, want nil", err)
	}

	got := string(payload)
	first := strings.Index(got, `"skill-1"`)
	second := strings.Index(got, `"skill-2"`)
	if first < 0 || second < 0 || first > second {
		t.Errorf("canonical payload lost or reordered the skill entries: %s", got)
	}
}
