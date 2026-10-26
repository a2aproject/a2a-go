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
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Converter decides how the agent's session updates appear in the task. See
// [Config.Converter]. It is called from one goroutine per task at a time.
type Converter interface {
	// Convert returns what to emit for u, or nil to emit nothing. Call
	// [ConvertUpdate] for the built-in conversion. An error fails the task
	// and cancels the agent's work on it.
	Convert(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error)
}

// ConverterFunc is a function which implements [Converter].
type ConverterFunc func(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error)

// Convert implements [Converter].
func (f ConverterFunc) Convert(ctx context.Context, execCtx *a2asrv.ExecutorContext, u Update) (*ConverterResult, error) {
	return f(ctx, execCtx, u)
}

// Update is an ACP session/update notification about a task.
type Update struct {
	// Type is the sessionUpdate discriminator, for example
	// "agent_message_chunk", "agent_thought_chunk", "tool_call" or "plan".
	Type string
	// Raw is the update as the agent sent it, including fields the built-in
	// conversion ignores.
	Raw json.RawMessage

	update *acp.SessionUpdate
	// tool is the merged state of the tool call if the update changed its
	// status: agents send partial tool calls, so Raw may lack its title or input.
	tool *acp.ToolCall
}

// ConverterResult is the content of an event emitted for an [Update]. The
// executor creates the event: the first result for an artifact name creates
// the artifact, later results of the task append to it.
type ConverterResult struct {
	// Artifact is the name of the artifact to stream the parts into. If
	// empty, the parts are sent in a working status message.
	Artifact string
	// Parts is the content. A result without parts emits nothing.
	Parts []*a2a.Part
	// Metadata is the metadata of the artifact, merged over earlier chunks,
	// or of the status message.
	Metadata map[string]any
	// EventMetadata is the metadata of the emitted event.
	EventMetadata map[string]any
}

// ResponseArtifact is the name of the artifact the agent's reply streams into.
const ResponseArtifact = "response"

var _ ConverterFunc = ConvertUpdate

// ConvertUpdate is the built-in conversion: agent message chunks stream into
// the [ResponseArtifact] artifact, and tool call status changes become working
// status messages with a text part and a {"toolCall": {"id", "title", "kind",
// "status", "input"}} data part. Other updates are dropped.
func ConvertUpdate(ctx context.Context, _ *a2asrv.ExecutorContext, u Update) (*ConverterResult, error) {
	su := u.update
	if su == nil {
		su = &acp.SessionUpdate{}
		if err := json.Unmarshal(u.Raw, su); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", u.Type, err)
		}
	}
	switch su.SessionUpdate {
	case acp.UpdateAgentMessageChunk:
		var block acp.ContentBlock
		if err := json.Unmarshal(su.Content, &block); err != nil {
			log.Warn(ctx, "dropped malformed acp message chunk", "cause", err)
			return nil, nil
		}
		part := contentPart(ctx, block)
		if part == nil {
			return nil, nil
		}
		return &ConverterResult{Artifact: ResponseArtifact, Parts: []*a2a.Part{part}}, nil

	case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
		tc := u.tool
		if tc == nil && u.update == nil && su.Status != "" {
			tc = &su.ToolCall
		}
		if tc == nil {
			return nil, nil
		}
		return toolStatus(tc), nil

	case acp.UpdateConfigOption:
		return nil, nil
	}
	log.Debug(ctx, "dropped acp session update", "type", su.SessionUpdate)
	return nil, nil
}

func contentPart(ctx context.Context, block acp.ContentBlock) *a2a.Part {
	switch block.Type {
	case "text":
		if block.Text == "" {
			return nil
		}
		return a2a.NewTextPart(block.Text)
	case "image", "audio":
		data, err := base64.StdEncoding.DecodeString(block.Data)
		if err != nil {
			log.Warn(ctx, "dropped acp content with malformed data", "type", block.Type, "cause", err)
			return nil
		}
		part := a2a.NewRawPart(data)
		part.MediaType = block.MimeType
		return part
	case "resource_link":
		part := a2a.NewFileURLPart(a2a.URL(block.URI), block.MimeType)
		part.Filename = block.Name
		return part
	}
	log.Debug(ctx, "dropped acp content", "type", block.Type)
	return nil
}

func toolStatus(tc *acp.ToolCall) *ConverterResult {
	var verb string
	switch tc.Status {
	case acp.ToolStatusInProgress:
		verb = "Running"
	case acp.ToolStatusCompleted:
		verb = "Finished"
	case acp.ToolStatusFailed:
		verb = "Failed"
	default:
		return nil
	}
	data := map[string]any{"id": tc.ToolCallID, "title": tc.Title, "kind": tc.Kind, "status": tc.Status}
	if tc.RawInput != nil {
		data["input"] = tc.RawInput
	}
	return &ConverterResult{Parts: []*a2a.Part{
		a2a.NewTextPart(fmt.Sprintf("%s %s", verb, tc.Title)),
		a2a.NewDataPart(map[string]any{"toolCall": data}),
	}}
}

// taskOutput turns converter results into the events of one task. It outlives
// a turn, so that a task interrupted by a reply keeps its artifacts.
type taskOutput struct {
	artifacts map[string]*artifactStream
	// last is the artifact name of the last emitted result, "" for a status message.
	last    string
	hasLast bool
}

type artifactStream struct {
	id        a2a.ArtifactID
	messageID string
	// needsBreak puts a blank line before the next text, because a new agent
	// message started or other output came in between.
	needsBreak bool
}

func newTaskOutput() *taskOutput {
	return &taskOutput{artifacts: map[string]*artifactStream{}}
}

// emit returns the event for res, or nil if res has no content. messageID
// is the ID of the agent message the update belongs to.
func (o *taskOutput) emit(info a2a.TaskInfoProvider, messageID string, res *ConverterResult) a2a.Event {
	if res == nil || len(res.Parts) == 0 {
		return nil
	}
	defer func() { o.last, o.hasLast = res.Artifact, true }()
	if res.Artifact == "" {
		msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, info, res.Parts...)
		msg.Metadata = res.Metadata
		evt := a2a.NewStatusUpdateEvent(info, a2a.TaskStateWorking, msg)
		evt.Metadata = res.EventMetadata
		return evt
	}

	s := o.artifacts[res.Artifact]
	if s == nil {
		evt := a2a.NewArtifactEvent(info, res.Parts...)
		evt.Artifact.Name = res.Artifact
		evt.Artifact.Metadata = res.Metadata
		evt.Metadata = res.EventMetadata
		o.artifacts[res.Artifact] = &artifactStream{id: evt.Artifact.ID, messageID: messageID}
		return evt
	}
	if messageID != s.messageID || (o.hasLast && o.last != res.Artifact) {
		s.needsBreak = true
	}
	s.messageID = messageID
	parts := res.Parts
	if s.needsBreak {
		parts, s.needsBreak = withBreak(parts)
	}
	evt := a2a.NewArtifactUpdateEvent(info, s.id, parts...)
	evt.Append = true
	evt.Artifact.Metadata = res.Metadata
	evt.Metadata = res.EventMetadata
	return evt
}

// withBreak puts a blank line before the first text part. It reports whether
// the break is still pending because there is no text part.
func withBreak(parts []*a2a.Part) ([]*a2a.Part, bool) {
	for i, p := range parts {
		text, ok := p.Content.(a2a.Text)
		if !ok {
			continue
		}
		out := append([]*a2a.Part(nil), parts...)
		broken := *p
		broken.Content = a2a.Text("\n\n" + string(text))
		out[i] = &broken
		return out, false
	}
	return parts, true
}
