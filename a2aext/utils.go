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

package a2aext

import (
	"slices"

	"github.com/a2aproject/a2a-go/a2a"
)

func isExtensionSupported(card *a2a.AgentCard, extURI string) bool {
	if card == nil {
		// by default assume that if a client was created from an AgentInterface we're connected to our
		// server which supports the same extensions.
		return true
	}
	return slices.ContainsFunc(card.Capabilities.Extensions, func(e a2a.AgentExtension) bool {
		return e.URI == extURI
	})
}
