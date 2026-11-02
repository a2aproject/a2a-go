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
	"github.com/a2aproject/a2a-go/v2/a2a"
)

// NewResponse creates an input-required response. The message consists of the
// provided parts followed by the structured input part.
func NewResponse(info a2a.TaskInfoProvider, req *Request, data any, parts ...*a2a.Part) (*a2a.Message, error) {
	var id string
	if req != nil {
		id = req.ID
	}
	inputPart, err := NewResponsePart(id, data)
	if err != nil {
		return nil, err
	}
	allParts := make([]*a2a.Part, 0, len(parts)+1)
	allParts = append(allParts, parts...)
	allParts = append(allParts, inputPart)
	msg := a2a.NewMessageForTask(a2a.MessageRoleUser, info, allParts...)
	msg.Extensions = []string{URI}
	return msg, nil
}
