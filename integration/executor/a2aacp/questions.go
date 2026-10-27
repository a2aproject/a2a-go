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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
	"github.com/google/jsonschema-go/jsonschema"
)

// Option IDs of questions. Permissions offer some of once, always, reject and
// reject_always; an option the agent offers twice keeps the agent's own ID.
// Forms offer accept, decline and cancel.
const (
	OptionOnce         = "once"
	OptionAlways       = "always"
	OptionReject       = "reject"
	OptionRejectAlways = "reject_always"
	OptionAccept       = "accept"
	OptionDecline      = "decline"
	OptionCancel       = "cancel"
)

// Question is a decision the agent needs before it can continue: a permission
// to run a tool, or a form. Unless [Config.AutoAnswer] answers it, the task
// pauses in input-required with a structured input request (see package
// [structuredinput]) for an [Answer].
type Question struct {
	// Request is the structured input request sent to the caller. Its schema
	// is an object with an "option" property, one of Options, and for forms a
	// "content" property with the form fields, required for "accept".
	Request *structuredinput.Request
	// Title is a short description of what is asked.
	Title string
	// Options are the IDs of the options the question offers.
	Options []string
	// Tool is the tool call a permission is asked for. Nil for forms.
	Tool *ToolCall
}

// ToolCall describes a tool call the agent asks permission for.
type ToolCall struct {
	ID    string
	Title string
	// Kind is the ACP tool kind: read, edit, delete, move, search, execute,
	// think, fetch or other.
	Kind  string
	Input any
}

// Answer is the structured input which answers a [Question].
type Answer struct {
	// Option is the ID of the chosen option.
	Option string `json:"option"`
	// Content holds the fields of an accepted form.
	Content map[string]any `json:"content,omitempty"`
}

// incomingQuestion is a permission request or a form elicitation from the agent.
type incomingQuestion struct {
	rpcID       json.RawMessage
	permission  *acp.RequestPermissionRequest
	elicitation *acp.CreateElicitationRequest
}

// cancel responds to the request with the cancelled outcome.
func (in *incomingQuestion) cancel(ag *agent) {
	var err error
	if in.elicitation != nil {
		err = ag.client.RespondElicitation(in.rpcID, acp.CreateElicitationResponse{Action: acp.ElicitationCancel})
	} else {
		err = ag.client.RespondPermission(in.rpcID, acp.PermissionOutcome{Outcome: acp.OutcomeCancelled})
	}
	if err != nil {
		log.Warn(context.Background(), "failed to cancel acp request", "cause", err)
	}
}

type option struct {
	id    string
	label string
}

// pendingQuestion is an ACP request translated to a question for the caller.
type pendingQuestion struct {
	in        *incomingQuestion
	q         Question
	options   []option
	fields    *jsonschema.Schema // form fields, nil for permissions
	optionIDs map[string]string  // question option ID -> ACP option ID
	timer     *time.Timer
}

var optionIDByKind = map[string]string{
	acp.PermissionAllowOnce:    OptionOnce,
	acp.PermissionAllowAlways:  OptionAlways,
	acp.PermissionRejectOnce:   OptionReject,
	acp.PermissionRejectAlways: OptionRejectAlways,
}

var formOptions = []option{{OptionAccept, "Accept"}, {OptionDecline, "Decline"}, {OptionCancel, "Cancel"}}

// newPending translates an agent request. elicitationID identifies a form,
// since the JSON-RPC ID of the request repeats after the agent restarts.
func (t *turn) newPending(in *incomingQuestion, elicitationID string) (*pendingQuestion, error) {
	p := &pendingQuestion{in: in, optionIDs: map[string]string{}}
	if el := in.elicitation; el != nil {
		fields := &jsonschema.Schema{}
		if err := convertJSON(el.RequestedSchema, fields); err != nil {
			return nil, fmt.Errorf("invalid elicitation schema: %w", err)
		}
		p.fields = fields
		p.options = formOptions
		p.q.Title = el.Message
		schema := p.schema()
		schema.Properties["content"] = fields
		if len(fields.Properties) > 0 {
			schema.If = &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"option": {Const: jsonschema.Ptr[any](OptionAccept)}}}
			schema.Then = &jsonschema.Schema{Required: []string{"content"}}
		}
		p.q.Request = &structuredinput.Request{ID: elicitationID, Schema: schema}
		return p, nil
	}

	tc := t.mergeTool(in.permission.ToolCall)
	p.q.Title = tc.Title
	if p.q.Title == "" {
		p.q.Title = tc.Kind
	}
	p.q.Tool = &ToolCall{ID: tc.ToolCallID, Title: tc.Title, Kind: tc.Kind, Input: tc.RawInput}
	for _, o := range in.permission.Options {
		id, ok := optionIDByKind[o.Kind]
		if _, taken := p.optionIDs[id]; !ok || taken {
			id = o.OptionID
		}
		p.optionIDs[id] = o.OptionID
		p.options = append(p.options, option{id, o.Name})
	}
	p.q.Request = &structuredinput.Request{ID: tc.ToolCallID, Schema: p.schema()}
	return p, nil
}

// schema returns the request schema without form fields.
func (p *pendingQuestion) schema() *jsonschema.Schema {
	optionSchema := &jsonschema.Schema{Type: "string"}
	for _, o := range p.options {
		p.q.Options = append(p.q.Options, o.id)
		optionSchema.OneOf = append(optionSchema.OneOf, &jsonschema.Schema{Const: jsonschema.Ptr[any](o.id), Title: o.label})
	}
	return &jsonschema.Schema{
		Type:       "object",
		Title:      p.q.Title,
		Properties: map[string]*jsonschema.Schema{"option": optionSchema},
		Required:   []string{"option"},
	}
}

// parts returns the parts of the message which asks the question, before the
// schema part: the question for people and, for a permission, a {"toolCall":
// {"id", "title", "kind", "input"}} data part.
func (p *pendingQuestion) parts(preamble string) []*a2a.Part {
	parts := []*a2a.Part{a2a.NewTextPart(preamble + p.text())}
	if tc := p.q.Tool; tc != nil {
		data := map[string]any{"id": tc.ID, "title": tc.Title, "kind": tc.Kind}
		if tc.Input != nil {
			data["input"] = tc.Input
		}
		parts = append(parts, a2a.NewDataPart(map[string]any{"toolCall": data}))
	}
	return parts
}

// errNotOffered is returned for an answer with an option the question doesn't offer.
var errNotOffered = errors.New("the option was not offered")

func (p *pendingQuestion) offers(id string) bool {
	return slices.Contains(p.q.Options, id)
}

func (p *pendingQuestion) reply(ag *agent, a Answer) error {
	if !p.offers(a.Option) && a.Option == OptionCancel {
		p.in.cancel(ag)
		return nil
	}
	if p.fields != nil {
		if !p.offers(a.Option) {
			return fmt.Errorf("%w: %q", errNotOffered, a.Option)
		}
		resp := acp.CreateElicitationResponse{Action: a.Option}
		if a.Option == OptionAccept {
			resp.Content = a.Content
		}
		return ag.client.RespondElicitation(p.in.rpcID, resp)
	}
	acpID, ok := p.optionIDs[a.Option]
	if !ok {
		return fmt.Errorf("%w: %q", errNotOffered, a.Option)
	}
	return ag.client.RespondPermission(p.in.rpcID, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: acpID})
}

// reject declines a form, or rejects a permission request if the agent offers
// a way to, and cancels it otherwise.
func (p *pendingQuestion) reject(ag *agent) error {
	a := Answer{Option: OptionDecline}
	if p.fields == nil {
		a.Option = firstOffered(p.q, OptionReject, OptionRejectAlways)
		if a.Option == "" {
			a.Option = OptionCancel
		}
	}
	return p.reply(ag, a)
}

func firstOffered(q Question, ids ...string) string {
	for _, id := range ids {
		if slices.Contains(q.Options, id) {
			return id
		}
	}
	return ""
}

// text describes the question for people, with the replies it accepts.
func (p *pendingQuestion) text() string {
	if p.fields != nil {
		return p.formText()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Permission required: %s", p.q.Title)
	if s := inputSummary(p.q.Tool.Input); s != "" && !strings.Contains(p.q.Title, s) {
		fmt.Fprintf(&sb, " (%s)", s)
	}
	sb.WriteString(".")
	if len(p.q.Options) > 0 {
		fmt.Fprintf(&sb, "\nReply with one of: %s.", strings.Join(p.q.Options, ", "))
	}
	return sb.String()
}

// inputSummary returns the field of a tool input which best tells a person
// what the tool call does, since agents often title a call by the tool name.
func inputSummary(input any) string {
	m, ok := input.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range []string{"command", "cmd", "filePath", "file_path", "path", "url", "pattern"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (p *pendingQuestion) formText() string {
	var sb strings.Builder
	sb.WriteString(p.q.Title)
	for _, name := range slices.Sorted(maps.Keys(p.fields.Properties)) {
		f := p.fields.Properties[name]
		fmt.Fprintf(&sb, "\n- %s (%s", name, f.Type)
		if slices.Contains(p.fields.Required, name) {
			sb.WriteString(", required")
		}
		sb.WriteString(")")
		description := f.Description
		if description == "" {
			description = f.Title
		}
		if description != "" {
			fmt.Fprintf(&sb, ": %s", description)
		}
		if choices := fieldChoices(f); len(choices) > 0 {
			fmt.Fprintf(&sb, " One of: %s.", strings.Join(choices, ", "))
		}
	}
	if len(p.fields.Properties) == 0 {
		sb.WriteString("\nReply with accept, decline or cancel.")
	} else {
		sb.WriteString("\nReply with a JSON object of the fields, or with decline or cancel.")
	}
	return sb.String()
}

func fieldChoices(f *jsonschema.Schema) []string {
	var choices []string
	for _, v := range f.Enum {
		if s, ok := v.(string); ok {
			choices = append(choices, s)
		}
	}
	for _, alt := range f.OneOf {
		if alt.Const == nil {
			continue
		}
		if s, ok := (*alt.Const).(string); ok {
			choices = append(choices, s)
		}
	}
	if f.Items != nil {
		choices = append(choices, fieldChoices(f.Items)...)
	}
	return choices
}

// errMalformedReply is reported for a typed reply which looks like an answer but can't be read.
var errMalformedReply = errors.New("the reply is not a valid JSON object")

// matchText reads a typed reply for clients which can't send structured
// input: an option ID or label, yes or no for permissions, and for forms a
// JSON object of the fields. It returns false if the text isn't an answer.
func (p *pendingQuestion) matchText(text string) (Answer, bool, error) {
	reply := normalize(text)
	if reply == "" {
		return Answer{}, false, nil
	}
	for _, o := range p.options {
		if reply != strings.ToLower(o.id) && reply != strings.ToLower(o.label) {
			continue
		}
		if o.id == OptionAccept && len(p.fields.Properties) > 0 {
			return Answer{}, false, nil
		}
		return Answer{Option: o.id}, true, nil
	}
	if p.fields != nil {
		if !strings.HasPrefix(strings.TrimSpace(text), "{") {
			return Answer{}, false, nil
		}
		var content map[string]any
		if err := json.Unmarshal([]byte(text), &content); err != nil {
			return Answer{}, false, fmt.Errorf("%w: %w", errMalformedReply, err)
		}
		return Answer{Option: OptionAccept, Content: content}, true, nil
	}
	var id string
	switch reply {
	case "y", "yes", "ok", "approve", "approved", "allow", "lgtm":
		id = OptionOnce
	case "always allow", "allow always":
		id = OptionAlways
	case "n", "no", "deny", "denied", "decline":
		id = OptionReject
	}
	if id == "" || !p.offers(id) {
		return Answer{}, false, nil
	}
	return Answer{Option: id}, true, nil
}

func normalize(text string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(text), " \n.!\"'`"))
}

// convertJSON converts between JSON-compatible values with a JSON round-trip.
func convertJSON(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}
