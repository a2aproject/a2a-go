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

// Package hitl describes the questions an agent asks while it works (human
// in the loop) and their answers, in the form they take in A2A messages.
//
// A question is sent in an input-required status message with a text part
// for people and a data part {"hitl": {"id", "type", "title", "detail",
// "options", "schema"}} for programs. It is answered by a message on the same
// task: an option ID or label, yes/no for permissions, a JSON object of form
// fields, or a data part {"hitl": {"id": "<question id>", "answer": <answer>}}.
//
// Servers use [NewStatusMessage] and [ParseAnswer]; clients use
// [FromStatus] or [FromMessage] and [NewAnswerMessage].
package hitl

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// DataKey is the key of the data part which describes a question or answers it.
const DataKey = "hitl"

// Type is the kind of a question.
type Type string

const (
	// TypePermission asks to approve a tool call.
	TypePermission Type = "permission"
	// TypeForm asks for structured input described by a JSON schema.
	TypeForm Type = "form"
)

// Option IDs of permission questions. An agent may offer only some of them,
// and options it offers twice keep the agent's own IDs.
const (
	OptionOnce         = "once"
	OptionAlways       = "always"
	OptionReject       = "reject"
	OptionRejectAlways = "reject_always"
)

// Actions which answer a form. Accept carries the form fields in
// [Answer.Content]; decline and cancel are the options of every form.
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// Question is a decision an agent needs before it can continue.
type Question struct {
	// ID identifies the question within its task.
	ID   string
	Type Type
	// Title is a short description of what is asked.
	Title string
	// Tool is the kind of tool a permission is asked for, for example read,
	// edit, delete, move, search, execute, think, fetch or other.
	Tool string
	// Input is the input of the tool call a permission is asked for.
	Input any
	// Options are the answers to a permission question, or the alternatives
	// to filling in a form.
	Options []Option
	// Schema is the JSON schema of a form's fields.
	Schema map[string]any
}

// Option is a possible answer to a question.
type Option struct {
	ID    string
	Label string
}

// Answer is a decision on a question. For a permission Option is the selected
// option ID. For a form it is an action, and Content holds the fields of an
// accepted form.
type Answer struct {
	Option  string
	Content map[string]any
}

// Option returns the option with the ID.
func (q Question) Option(id string) (Option, bool) {
	for _, o := range q.Options {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

// Data returns the question as the value of the hitl data part. It contains
// only plain JSON values, which task stores can copy.
func (q Question) Data() map[string]any {
	options := make([]any, len(q.Options))
	for i, o := range q.Options {
		options[i] = map[string]any{"id": o.ID, "label": o.Label}
	}
	data := map[string]any{
		"id":      q.ID,
		"type":    string(q.Type),
		"title":   q.Title,
		"options": options,
	}
	if q.Type == TypePermission {
		detail := map[string]any{"tool": q.Tool}
		if q.Input != nil {
			detail["input"] = q.Input
		}
		data["detail"] = detail
	}
	if q.Schema != nil {
		data["schema"] = q.Schema
	}
	return data
}

// FromData returns the question described by the value of a hitl data part.
func FromData(data map[string]any) (Question, bool) {
	var q Question
	q.ID, _ = data["id"].(string)
	typ, _ := data["type"].(string)
	q.Type = Type(typ)
	if q.ID == "" || q.Type == "" {
		return Question{}, false
	}
	q.Title, _ = data["title"].(string)
	if detail, ok := data["detail"].(map[string]any); ok {
		q.Tool, _ = detail["tool"].(string)
		q.Input = detail["input"]
	}
	q.Schema, _ = data["schema"].(map[string]any)
	for _, raw := range asSlice(data["options"]) {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		label, _ := m["label"].(string)
		if label == "" {
			label = id
		}
		q.Options = append(q.Options, Option{ID: id, Label: label})
	}
	return q, true
}

// FromMessage returns the question in a status message.
func FromMessage(msg *a2a.Message) (Question, bool) {
	if msg == nil {
		return Question{}, false
	}
	for _, p := range msg.Parts {
		m, ok := p.Data().(map[string]any)
		if !ok {
			continue
		}
		if data, ok := m[DataKey].(map[string]any); ok {
			if q, ok := FromData(data); ok {
				return q, true
			}
		}
	}
	return Question{}, false
}

// FromStatus returns the question of an input-required task status.
func FromStatus(st a2a.TaskStatus) (Question, bool) {
	if st.State != a2a.TaskStateInputRequired {
		return Question{}, false
	}
	return FromMessage(st.Message)
}

// NewStatusMessage returns the agent message which asks q, to be sent with
// the input-required state. preamble is put before the question text.
func NewStatusMessage(info a2a.TaskInfoProvider, q Question, preamble string) *a2a.Message {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, info,
		a2a.NewTextPart(preamble+q.Text()),
		a2a.NewDataPart(map[string]any{DataKey: q.Data()}),
	)
}

// NewAnswerPart returns the data part which answers the question with the
// ID. answer is an option ID, a form action, or an object of form fields.
func NewAnswerPart(questionID string, answer any) *a2a.Part {
	return a2a.NewDataPart(map[string]any{DataKey: map[string]any{"id": questionID, "answer": answer}})
}

// NewAnswerMessage returns the user message which answers the question with
// the ID on the task. See [NewAnswerPart].
func NewAnswerMessage(info a2a.TaskInfoProvider, questionID string, answer any) *a2a.Message {
	return a2a.NewMessageForTask(a2a.MessageRoleUser, info, NewAnswerPart(questionID, answer))
}

// Text describes the question for people, with the replies it accepts.
func (q Question) Text() string {
	if q.Type == TypeForm {
		return q.formText()
	}
	return q.permissionText()
}

func (q Question) permissionText() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Permission required: %s", q.Title)
	if s := inputSummary(q.Input); s != "" && !strings.Contains(q.Title, s) {
		fmt.Fprintf(&sb, " (%s)", s)
	}
	sb.WriteString(".")
	if len(q.Options) > 0 {
		ids := make([]string, len(q.Options))
		for i, o := range q.Options {
			ids[i] = o.ID
		}
		fmt.Fprintf(&sb, "\nReply with one of: %s.", strings.Join(ids, ", "))
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

func (q Question) formText() string {
	var sb strings.Builder
	sb.WriteString(q.Title)
	fields := q.Fields()
	for _, f := range fields {
		fmt.Fprintf(&sb, "\n- %s (%s", f.Name, f.Type)
		if f.Required {
			sb.WriteString(", required")
		}
		sb.WriteString(")")
		if f.Description != "" {
			fmt.Fprintf(&sb, ": %s", f.Description)
		}
		if len(f.Choices) > 0 {
			fmt.Fprintf(&sb, " One of: %s.", strings.Join(f.Choices, ", "))
		}
	}
	switch len(fields) {
	case 0:
		sb.WriteString("\nReply with accept, decline or cancel.")
	case 1:
		sb.WriteString("\nReply with the value, or with decline or cancel.")
	default:
		sb.WriteString("\nReply with a JSON object of the fields, or with decline or cancel.")
	}
	return sb.String()
}

// Field is a field of a form.
type Field struct {
	Name string
	// Type is the JSON schema type: string, number, integer, boolean or array.
	Type        string
	Description string
	Required    bool
	// Choices are the allowed values of an enum field or the items of an array field.
	Choices []string
}

// Fields returns the fields of a form, sorted by name.
func (q Question) Fields() []Field {
	props, _ := q.Schema["properties"].(map[string]any)
	required := asSlice(q.Schema["required"])
	var fields []Field
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		f := Field{Name: name, Required: slices.Contains(required, any(name))}
		f.Type, _ = p["type"].(string)
		if f.Description, _ = p["description"].(string); f.Description == "" {
			f.Description, _ = p["title"].(string)
		}
		f.Choices = append(f.Choices, stringValues(p["enum"])...)
		for _, v := range asSlice(p["oneOf"]) {
			if m, ok := v.(map[string]any); ok {
				if s, ok := m["const"].(string); ok {
					f.Choices = append(f.Choices, s)
				}
			}
		}
		if items, ok := p["items"].(map[string]any); ok {
			f.Choices = append(f.Choices, stringValues(items["enum"])...)
		}
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func stringValues(v any) []string {
	var out []string
	for _, item := range asSlice(v) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// asSlice accepts the []any of decoded JSON and the []map[string]any of
// values built in Go.
func asSlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case []map[string]any:
		out := make([]any, len(s))
		for i, m := range s {
			out[i] = m
		}
		return out
	case []string:
		out := make([]any, len(s))
		for i, str := range s {
			out[i] = str
		}
		return out
	}
	return nil
}
