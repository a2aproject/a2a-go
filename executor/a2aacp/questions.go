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
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
)

type questionKind string

const (
	// kindPermission is a request to run a tool call (session/request_permission).
	kindPermission questionKind = "permission"
	// kindForm asks for structured input described by a JSON schema (elicitation/create).
	kindForm questionKind = "form"
)

// Option IDs of a permission question, derived from the ACP permission option
// kinds, so an agent may offer only some of them.
const (
	optionOnce         = "once"
	optionAlways       = "always"
	optionReject       = "reject"
	optionRejectAlways = "reject_always"
)

// hitlDataKey is the key of the data part which describes a question in an
// input-required status message, and which a caller can use to answer it.
const hitlDataKey = "hitl"

// question is a decision the agent needs before the current turn can continue.
type question struct {
	id   string
	kind questionKind
	// tool is the ACP tool kind of a permission request: read, edit, delete,
	// move, search, execute, think, fetch or other.
	tool  string
	title string
	input any
	// options are the answers to a permission request, or the alternatives to
	// filling in a form.
	options []option
	// schema describes the fields of a form.
	schema map[string]any
}

type option struct {
	id    string
	label string
}

// answer is a decision on a question. For a permission request option is the
// selected option. For a form it is the elicitation action, and content holds
// the fields of an accepted form.
type answer struct {
	option  string
	content map[string]any
}

var formOptions = []option{{id: acp.ElicitationDecline, label: "Decline"}, {id: acp.ElicitationCancel, label: "Cancel"}}

func (q question) option(id string) (option, bool) {
	for _, o := range q.options {
		if o.id == id {
			return o, true
		}
	}
	return option{}, false
}

// toolRules answer permission requests by tool kind. Patterns are globs, and
// deny takes precedence.
type toolRules struct {
	allow []string
	deny  []string
}

type ruleDecision int

const (
	ruleAsk ruleDecision = iota
	ruleAllow
	ruleDeny
)

func (r toolRules) decide(q question) ruleDecision {
	if q.kind != kindPermission {
		return ruleAsk
	}
	if matchAny(r.deny, q.tool) {
		return ruleDeny
	}
	if matchAny(r.allow, q.tool) {
		return ruleAllow
	}
	return ruleAsk
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, s); ok {
			return true
		}
	}
	return false
}

// toData converts q to plain JSON values: task stores copy events with gob,
// which only handles registered types in data parts.
func (q question) toData() map[string]any {
	options := make([]any, len(q.options))
	for i, o := range q.options {
		options[i] = map[string]any{"id": o.id, "label": o.label}
	}
	data := map[string]any{
		"id":      q.id,
		"type":    string(q.kind),
		"title":   q.title,
		"options": options,
	}
	if q.kind == kindPermission {
		detail := map[string]any{"tool": q.tool}
		if q.input != nil {
			detail["input"] = q.input
		}
		data["detail"] = detail
	}
	if q.schema != nil {
		data["schema"] = q.schema
	}
	return data
}

func inputRequiredMessage(info a2a.TaskInfoProvider, q question, preamble string) *a2a.Message {
	var text string
	if q.kind == kindForm {
		text = formText(q)
	} else {
		text = permissionText(q)
	}
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, info,
		a2a.NewTextPart(preamble+text),
		a2a.NewDataPart(map[string]any{hitlDataKey: q.toData()}),
	)
}

func permissionText(q question) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Permission required: %s", q.title)
	if s := inputSummary(q.input); s != "" && !strings.Contains(q.title, s) {
		fmt.Fprintf(&sb, " (%s)", s)
	}
	sb.WriteString(".")
	if len(q.options) > 0 {
		ids := make([]string, len(q.options))
		for i, o := range q.options {
			ids[i] = o.id
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

// formField is one property of an elicitation schema.
type formField struct {
	name     string
	typ      string
	desc     string
	required bool
	choices  []string
}

func formFields(schema map[string]any) []formField {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	var fields []formField
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		f := formField{name: name, required: slices.Contains(required, any(name))}
		f.typ, _ = p["type"].(string)
		if f.desc, _ = p["description"].(string); f.desc == "" {
			f.desc, _ = p["title"].(string)
		}
		for _, v := range asSlice(p["enum"]) {
			if s, ok := v.(string); ok {
				f.choices = append(f.choices, s)
			}
		}
		for _, v := range asSlice(p["oneOf"]) {
			if m, ok := v.(map[string]any); ok {
				if s, ok := m["const"].(string); ok {
					f.choices = append(f.choices, s)
				}
			}
		}
		if items, ok := p["items"].(map[string]any); ok {
			for _, v := range asSlice(items["enum"]) {
				if s, ok := v.(string); ok {
					f.choices = append(f.choices, s)
				}
			}
		}
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
	return fields
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func formText(q question) string {
	var sb strings.Builder
	sb.WriteString(q.title)
	fields := formFields(q.schema)
	for _, f := range fields {
		fmt.Fprintf(&sb, "\n- %s (%s", f.name, f.typ)
		if f.required {
			sb.WriteString(", required")
		}
		sb.WriteString(")")
		if f.desc != "" {
			fmt.Fprintf(&sb, ": %s", f.desc)
		}
		if len(f.choices) > 0 {
			fmt.Fprintf(&sb, " One of: %s.", strings.Join(f.choices, ", "))
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

var errNoAnswer = errors.New("the reply does not answer the question")

// parseAnswer extracts an answer to q from a caller's message. It accepts a
// hitl data part {"hitl": {"id": ..., "answer": ...}}, a data part
// {"answer": ...}, or text. For a permission request the answer is an option
// ID or label, or yes/no. For a form it is an object of fields, decline or
// cancel; text can also be a JSON object or, for a single field, its value.
func parseAnswer(q question, msg *a2a.Message) (answer, error) {
	if msg == nil {
		return answer{}, errNoAnswer
	}
	var texts []string
	for _, p := range msg.Parts {
		if v, ok := answerFromData(q, p.Data()); ok {
			return answerFromValue(q, v)
		}
		if t := strings.TrimSpace(p.Text()); t != "" {
			texts = append(texts, t)
		}
	}
	text := strings.Join(texts, "\n")
	if text == "" {
		return answer{}, errNoAnswer
	}
	if q.kind == kindForm {
		return formAnswerFromText(q, text)
	}
	return permissionAnswerFromText(q, text)
}

func answerFromData(q question, data any) (any, bool) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, false
	}
	if hitl, ok := m[hitlDataKey].(map[string]any); ok {
		if id, ok := hitl["id"].(string); ok && id != q.id {
			return nil, false
		}
		m = hitl
	}
	v, ok := m["answer"]
	return v, ok
}

func answerFromValue(q question, v any) (answer, error) {
	if s, ok := v.(string); ok {
		if q.kind == kindForm {
			return formAnswerFromText(q, s)
		}
		if _, ok := q.option(s); ok {
			return answer{option: s}, nil
		}
		return answer{}, errNoAnswer
	}
	if m, ok := v.(map[string]any); ok && q.kind == kindForm {
		return acceptForm(q, m)
	}
	return answer{}, errNoAnswer
}

func normalize(text string) string {
	return strings.ToLower(strings.Trim(text, " \n.!\"'`"))
}

func permissionAnswerFromText(q question, text string) (answer, error) {
	reply := normalize(text)
	for _, o := range q.options {
		if reply == strings.ToLower(o.id) || reply == strings.ToLower(o.label) {
			return answer{option: o.id}, nil
		}
	}
	var id string
	switch reply {
	case "y", "yes", "ok", "approve", "approved", "allow", "lgtm":
		id = optionOnce
	case "n", "no", "deny", "denied", "decline":
		id = optionReject
	}
	if _, ok := q.option(id); ok {
		return answer{option: id}, nil
	}
	return answer{}, errNoAnswer
}

func formAnswerFromText(q question, text string) (answer, error) {
	fields := formFields(q.schema)
	switch reply := normalize(text); reply {
	case acp.ElicitationDecline, acp.ElicitationCancel:
		return answer{option: reply}, nil
	case "accept", "yes", "ok":
		if len(fields) == 0 {
			return answer{option: acp.ElicitationAccept}, nil
		}
	}
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			return answer{}, fmt.Errorf("the reply is not a valid JSON object: %w", err)
		}
		return acceptForm(q, m)
	}
	if len(fields) == 1 {
		return acceptForm(q, map[string]any{fields[0].name: text})
	}
	return answer{}, errNoAnswer
}

// acceptForm checks the values against the form schema and converts text
// values to the field types.
func acceptForm(q question, values map[string]any) (answer, error) {
	content := map[string]any{}
	for _, f := range formFields(q.schema) {
		v, ok := values[f.name]
		if !ok {
			if f.required {
				return answer{}, fmt.Errorf("the field %s is required", f.name)
			}
			continue
		}
		cv, err := convertField(f, v)
		if err != nil {
			return answer{}, err
		}
		content[f.name] = cv
	}
	return answer{option: acp.ElicitationAccept, content: content}, nil
}

func convertField(f formField, v any) (any, error) {
	s, isText := v.(string)
	s = strings.TrimSpace(s)
	switch f.typ {
	case "number", "integer":
		if n, ok := v.(float64); ok {
			return n, nil
		}
		if n, err := strconv.ParseFloat(s, 64); isText && err == nil {
			return n, nil
		}
		return nil, fmt.Errorf("the field %s must be a number", f.name)
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		switch normalize(s) {
		case "true", "yes", "y":
			return true, nil
		case "false", "no", "n":
			return false, nil
		}
		return nil, fmt.Errorf("the field %s must be true or false", f.name)
	case "array":
		var items []string
		if isText {
			for item := range strings.SplitSeq(s, ",") {
				items = append(items, strings.TrimSpace(item))
			}
		} else {
			for _, item := range asSlice(v) {
				str, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("the field %s must be a list of strings", f.name)
				}
				items = append(items, str)
			}
		}
		for _, item := range items {
			if len(f.choices) > 0 && !slices.Contains(f.choices, item) {
				return nil, fmt.Errorf("%q is not a valid choice for %s", item, f.name)
			}
		}
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = item
		}
		return out, nil
	default:
		if !isText {
			return nil, fmt.Errorf("the field %s must be text", f.name)
		}
		if len(f.choices) > 0 && !slices.Contains(f.choices, s) {
			return nil, fmt.Errorf("%q is not a valid choice for %s", s, f.name)
		}
		return s, nil
	}
}
