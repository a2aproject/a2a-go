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

package a2asrv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// AgentCardHandlerOption customizes the behavior of the agent card HTTP handlers
// created by [NewAgentCardHandler] and [NewStaticAgentCardHandler].
type AgentCardHandlerOption func(*agentCardHandlerConfig)

type agentCardHandlerConfig struct {
	cacheControl string
}

// WithAgentCardCacheControl sets the Cache-Control header of agent card responses,
// e.g. "public, max-age=3600". The header is not sent by default.
func WithAgentCardCacheControl(value string) AgentCardHandlerOption {
	return func(c *agentCardHandlerConfig) {
		c.cacheControl = value
	}
}

func newAgentCardHandlerConfig(opts []AgentCardHandlerOption) *agentCardHandlerConfig {
	cfg := &agentCardHandlerConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// cardETag excludes signature values, which randomized algorithms like ES256 change on every
// request, but covers protected headers, so that a signing key rotation changes the tag.
func cardETag(card *a2a.AgentCard) string {
	unsigned := *card
	signatures := unsigned.Signatures
	unsigned.Signatures = nil
	body, err := json.Marshal(&unsigned)
	if err != nil {
		return ""
	}
	protected := make([]string, 0, len(signatures))
	for _, sig := range signatures {
		protected = append(protected, sig.Protected)
	}
	slices.Sort(protected)
	return hashETag(body, protected...)
}

// rawJSONETag is [cardETag] for raw JSON. Re-marshaling through a map normalizes member order.
func rawJSONETag(raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		var protected []string
		if sigs, ok := m["signatures"].([]any); ok {
			for _, sig := range sigs {
				if sig, ok := sig.(map[string]any); ok {
					if p, ok := sig["protected"].(string); ok {
						protected = append(protected, p)
					}
				}
			}
		}
		delete(m, "signatures")
		if body, err := json.Marshal(m); err == nil {
			slices.Sort(protected)
			return hashETag(body, protected...)
		}
	}
	return hashETag(raw)
}

func hashETag(body []byte, extra ...string) string {
	h := sha256.New()
	h.Write(body)
	for _, e := range extra {
		h.Write([]byte{0})
		h.Write([]byte(e))
	}
	return `W/"` + hex.EncodeToString(h.Sum(nil)) + `"`
}

// ifNoneMatchHits uses the weak comparison function from RFC 9110 section 8.8.3.2.
func ifNoneMatchHits(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}
