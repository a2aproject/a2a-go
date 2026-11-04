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

package a2asrv

import (
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestCardETag_Format(t *testing.T) {
	tag := cardETag(&a2a.AgentCard{Name: "A", Description: "D"})
	if !strings.HasPrefix(tag, `W/"`) || !strings.HasSuffix(tag, `"`) {
		t.Fatalf("cardETag = %q, want weak ETag format W/\"...\"", tag)
	}
}

func TestCardETag_IgnoresRandomizedSignatureValues(t *testing.T) {
	first := &a2a.AgentCard{Name: "A", Description: "D",
		Signatures: []a2a.AgentCardSignature{{Protected: "p", Signature: "sig-1"}}}
	second := &a2a.AgentCard{Name: "A", Description: "D",
		Signatures: []a2a.AgentCardSignature{{Protected: "p", Signature: "sig-2"}}}

	if a, b := cardETag(first), cardETag(second); a != b {
		t.Errorf("re-signing with the same key must keep the ETag stable: %q != %q", a, b)
	}
}

func TestCardETag_KeyRotationChangesETag(t *testing.T) {
	oldKey := &a2a.AgentCard{Name: "A", Description: "D",
		Signatures: []a2a.AgentCardSignature{{Protected: "kid-old", Signature: "s"}}}
	newKey := &a2a.AgentCard{Name: "A", Description: "D",
		Signatures: []a2a.AgentCardSignature{{Protected: "kid-new", Signature: "s"}}}

	if a, b := cardETag(oldKey), cardETag(newKey); a == b {
		t.Errorf("rotating the signing key must change the ETag, got %q for both", a)
	}
}

func TestCardETag_SignaturePresenceChangesETag(t *testing.T) {
	plain := &a2a.AgentCard{Name: "A", Description: "D"}
	signed := &a2a.AgentCard{Name: "A", Description: "D",
		Signatures: []a2a.AgentCardSignature{{Protected: "p", Signature: "s"}}}

	if a, b := cardETag(plain), cardETag(signed); a == b {
		t.Errorf("signing a card must change the ETag, got %q for both", a)
	}
}

func TestCardETag_DifferentCardsDiffer(t *testing.T) {
	a := cardETag(&a2a.AgentCard{Name: "A"})
	b := cardETag(&a2a.AgentCard{Name: "B"})
	if a == b {
		t.Errorf("different cards produced the same ETag %q", a)
	}
}

func TestCardETag_DoesNotMutateInput(t *testing.T) {
	card := &a2a.AgentCard{Name: "A",
		Signatures: []a2a.AgentCardSignature{{Protected: "p", Signature: "s"}}}
	cardETag(card)
	if len(card.Signatures) != 1 {
		t.Errorf("cardETag mutated the input card: Signatures = %v", card.Signatures)
	}
}

func TestRawJSONETag_IgnoresKeyOrderAndRandomizedSignatureValues(t *testing.T) {
	a := rawJSONETag([]byte(`{"name":"A","description":"D","signatures":[{"protected":"p","signature":"sig-1"}]}`))
	b := rawJSONETag([]byte(`{"description":"D","name":"A","signatures":[{"protected":"p","signature":"sig-2"}]}`))
	if !strings.HasPrefix(a, `W/"`) {
		t.Fatalf("rawJSONETag = %q, want weak ETag format", a)
	}
	if a != b {
		t.Errorf("key order and re-signing must not affect the ETag: %q != %q", a, b)
	}
}

func TestRawJSONETag_KeyRotationChangesETag(t *testing.T) {
	oldKey := rawJSONETag([]byte(`{"name":"A","signatures":[{"protected":"kid-old","signature":"s"}]}`))
	newKey := rawJSONETag([]byte(`{"name":"A","signatures":[{"protected":"kid-new","signature":"s"}]}`))
	if oldKey == newKey {
		t.Errorf("rotating the signing key must change the ETag, got %q for both", oldKey)
	}
}

func TestRawJSONETag_SignaturePresenceChangesETag(t *testing.T) {
	withSig := rawJSONETag([]byte(`{"name":"A","signatures":[{"protected":"p","signature":"s"}]}`))
	withoutSig := rawJSONETag([]byte(`{"name":"A"}`))
	if withSig == withoutSig {
		t.Errorf("signing a card must change the ETag, got %q for both", withSig)
	}
}

// Note: the raw-JSON path (map re-marshal, sorted keys) and the struct path
// (field-declaration order) intentionally do NOT produce identical tags for
// the same card content. An ETag is opaque and only compared against tags this
// same server produced; a given handler never switches producer types.

func TestRawJSONETag_InvalidJSONFallback(t *testing.T) {
	a := rawJSONETag([]byte(`not json`))
	b := rawJSONETag([]byte(`not json`))
	if !strings.HasPrefix(a, `W/"`) || a != b {
		t.Errorf("rawJSONETag(invalid) = %q, %q; want equal weak tags", a, b)
	}
}

func TestIfNoneMatchHits(t *testing.T) {
	const etag = `W/"abc123"`
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "empty header", header: "", want: false},
		{name: "exact weak match", header: etag, want: true},
		{name: "strong form of weak tag", header: `"abc123"`, want: true},
		{name: "wildcard", header: "*", want: true},
		{name: "list containing tag", header: `"other", W/"abc123"`, want: true},
		{name: "list with spaces", header: ` "other" ,  W/"abc123" `, want: true},
		{name: "list not containing tag", header: `"other", W/"def456"`, want: false},
		{name: "mismatch", header: `W/"def456"`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ifNoneMatchHits(tc.header, etag); got != tc.want {
				t.Errorf("ifNoneMatchHits(%q, %q) = %v, want %v", tc.header, etag, got, tc.want)
			}
		})
	}
}

func TestWithCacheControl(t *testing.T) {
	cfg := newAgentCardHandlerConfig([]AgentCardHandlerOption{WithCacheControl("public, max-age=60")})
	if cfg.cacheControl != "public, max-age=60" {
		t.Errorf("cacheControl = %q, want %q", cfg.cacheControl, "public, max-age=60")
	}
	def := newAgentCardHandlerConfig(nil)
	if def.cacheControl != "" {
		t.Errorf("default cacheControl = %q, want empty", def.cacheControl)
	}
}
