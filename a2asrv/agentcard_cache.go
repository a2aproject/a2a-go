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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// AgentCardHandlerOption customizes the behavior of the agent card HTTP handlers
// created by [NewAgentCardHandler] and [NewStaticAgentCardHandler].
type AgentCardHandlerOption func(*agentCardHandlerConfig)

type agentCardHandlerConfig struct {
	cacheControl string
}

// WithCacheControl sets the Cache-Control response header value for agent card
// responses, e.g. "public, max-age=3600". An appropriate max-age depends on how
// often the agent card is expected to change, which only the deployment knows,
// so no Cache-Control header is sent unless this option is used. Revalidation
// against the ETag works either way and costs a single 304 round trip.
func WithCacheControl(value string) AgentCardHandlerOption {
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

// cardETag computes a weak ETag for the given card, excluding signatures.
//
// Signatures are excluded and the tag is weak so that revalidation works for
// cards signed per request (see [NewSignedCardProducer]): randomized signing
// algorithms such as ES256 produce a different signature on every request, so
// a strong tag over the full body would never match. A weak tag claims semantic
// equivalence, which two signings of identical content have. On a 304 the client
// keeps the signature it already holds, which remains valid because it covers
// content that has not changed.
//
// Returns an empty string if the card cannot be marshaled; callers then omit
// the ETag header.
func cardETag(card *a2a.AgentCard) string {
	unsigned := *card
	unsigned.Signatures = nil
	body, err := json.Marshal(&unsigned)
	if err != nil {
		return ""
	}
	return hashETag(body)
}

// rawJSONETag computes a weak ETag from raw agent card JSON, excluding the
// "signatures" member (see [cardETag] for why). Unmarshaling into a map and
// re-marshaling normalizes member order, because encoding/json sorts map keys.
// RFC 8785 canonicalization is deliberately not used: an ETag is opaque and is
// only ever compared against values produced by this same server, so
// determinism is the whole requirement. If raw is not a valid JSON object,
// the tag degrades to a hash of the raw bytes.
func rawJSONETag(raw []byte) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		delete(m, "signatures")
		if body, err := json.Marshal(m); err == nil {
			return hashETag(body)
		}
	}
	return hashETag(raw)
}

func hashETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `W/"` + hex.EncodeToString(sum[:]) + `"`
}

// ifNoneMatchHits reports whether an If-None-Match header value selects the
// given entity tag, using the weak comparison function of RFC 9110 8.2.2:
// "*" matches unconditionally, and W/"x" and "x" denote the same entity on
// either side of the comparison.
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
