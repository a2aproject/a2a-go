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

package a2aacp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
)

// Update is an ACP session/update notification of a turn, as the agent sent it.
type Update struct {
	// Type is the sessionUpdate discriminator, for example
	// "agent_message_chunk", "agent_thought_chunk", "tool_call" or "plan".
	Type string
	// Raw is the JSON of the update object, including fields the built-in
	// conversion ignores.
	Raw json.RawMessage
}

// UpdateConverter translates the session updates of one turn to A2A events.
// It is created per turn by [Config.UpdateConverter], so it can keep state
// across updates, and is called from one goroutine at a time.
//
// Convert receives the events of the built-in conversion, possibly none, and
// returns the events to emit: it can add to them, replace them or drop them.
// Dropping or replacing built-in artifact events breaks the appends which
// follow them, so a converter should handle all updates of a type or none.
// An error fails the task and cancels the turn.
type UpdateConverter interface {
	Convert(ctx context.Context, u Update, events []a2a.Event) ([]a2a.Event, error)
}

// UpdateConverterFunc is a function which implements [UpdateConverter].
type UpdateConverterFunc func(ctx context.Context, u Update, events []a2a.Event) ([]a2a.Event, error)

// Convert implements [UpdateConverter].
func (f UpdateConverterFunc) Convert(ctx context.Context, u Update, events []a2a.Event) ([]a2a.Event, error) {
	return f(ctx, u, events)
}

// ChainUpdateConverters returns a converter which passes every update through
// converters in order, each receiving the events of the previous one.
func ChainUpdateConverters(converters ...UpdateConverter) UpdateConverter {
	return UpdateConverterFunc(func(ctx context.Context, u Update, events []a2a.Event) ([]a2a.Event, error) {
		for _, c := range converters {
			var err error
			if events, err = c.Convert(ctx, u, events); err != nil {
				return nil, err
			}
		}
		return events, nil
	})
}

// ThoughtsAsArtifact returns a converter which streams the agent's thoughts
// (agent_thought_chunk text) of the task into an artifact with the given
// name, the way the agent's reply streams into the "response" artifact.
func ThoughtsAsArtifact(info a2a.TaskInfoProvider, name string) UpdateConverter {
	stream := &textStream{name: name}
	return UpdateConverterFunc(func(_ context.Context, u Update, events []a2a.Event) ([]a2a.Event, error) {
		if u.Type != updateAgentThoughtChunk {
			return events, nil
		}
		var chunk struct {
			MessageID string           `json:"messageId"`
			Content   acp.ContentBlock `json:"content"`
		}
		if err := json.Unmarshal(u.Raw, &chunk); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", u.Type, err)
		}
		if chunk.Content.Type != "text" {
			return events, nil
		}
		if evt := stream.write(info, chunk.MessageID, chunk.Content.Text); evt != nil {
			events = append(events, evt)
		}
		return events, nil
	})
}

const updateAgentThoughtChunk = "agent_thought_chunk"
