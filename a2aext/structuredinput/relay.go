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

package structuredinput

import (
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// RelayRequest creates the input request which an intermediary publishes in its own task
// for an input request it received from an agent it called.
//
// The relayed request keeps the schema and the required answer source. It gets a new ID, because
// an ID is unique only within one task. AskedBy is set to askedBy unless the received request
// was already relayed, in which case the original value is kept.
func RelayRequest(received *Request, id string, askedBy string) (*Request, error) {
	if received == nil {
		return nil, fmt.Errorf("input request is required")
	}
	relayed := &Request{ID: id, Schema: received.Schema, AnswerFrom: received.AnswerFrom, AskedBy: received.AskedBy}
	if relayed.AskedBy == "" {
		relayed.AskedBy = askedBy
	}
	return relayed, nil
}

// RelayResponse creates the continuation message which an intermediary sends to the agent it called.
// The message answers the received request with the input accepted for the relayed request
// and keeps the answer source stated by the intermediary's own client.
func RelayResponse(info a2a.TaskInfoProvider, received *Request, answer *ProcessorResult, parts ...*a2a.Part) (*a2a.Message, error) {
	if received == nil {
		return nil, fmt.Errorf("input request is required")
	}
	if answer == nil || !answer.Accepted() {
		return nil, fmt.Errorf("an accepted structured input is required")
	}
	if received.AnswerFrom != "" && answer.AnsweredBy != received.AnswerFrom {
		return nil, fmt.Errorf("input request requires an answer from %q, got %q", received.AnswerFrom, answer.AnsweredBy)
	}
	return NewResponseAnsweredBy(info, received, answer.AnsweredBy, answer.Input, parts...)
}
