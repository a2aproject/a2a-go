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

package hitl

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// ErrNoAnswer is returned by [ParseAnswer] for a message which doesn't
// answer the question, as opposed to an answer with invalid values.
var ErrNoAnswer = errors.New("the reply does not answer the question")

// ParseAnswer extracts the answer to q from a message. It accepts a hitl data
// part {"hitl": {"id": ..., "answer": ...}} whose ID matches q, a data part
// {"answer": ...}, or text. For a permission the answer is an option ID or
// label, or yes/no (see [Question.MatchOption]). For a form it is an object
// of fields, decline or cancel; text can also be a JSON object or, for a
// single field, its value. Form values are checked against the schema and
// converted to the field types.
func ParseAnswer(q Question, msg *a2a.Message) (Answer, error) {
	if msg == nil {
		return Answer{}, ErrNoAnswer
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
		return Answer{}, ErrNoAnswer
	}
	if q.Type == TypeForm {
		return formAnswerFromText(q, text)
	}
	if o, ok := q.MatchOption(text); ok {
		return Answer{Option: o.ID}, nil
	}
	return Answer{}, ErrNoAnswer
}

func answerFromData(q Question, data any) (any, bool) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, false
	}
	if hitl, ok := m[DataKey].(map[string]any); ok {
		if id, ok := hitl["id"].(string); ok && id != q.ID {
			return nil, false
		}
		m = hitl
	}
	v, ok := m["answer"]
	return v, ok
}

func answerFromValue(q Question, v any) (Answer, error) {
	if s, ok := v.(string); ok {
		if q.Type == TypeForm {
			return formAnswerFromText(q, s)
		}
		if _, ok := q.Option(s); ok {
			return Answer{Option: s}, nil
		}
		return Answer{}, ErrNoAnswer
	}
	if m, ok := v.(map[string]any); ok && q.Type == TypeForm {
		return q.AcceptForm(m)
	}
	return Answer{}, ErrNoAnswer
}

func normalize(text string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(text), " \n.!\"'`"))
}

// MatchOption maps a typed reply to an option: an option ID or label, in any
// case, or a word such as yes, approve, always, no or deny.
func (q Question) MatchOption(text string) (Option, bool) {
	reply := normalize(text)
	for _, o := range q.Options {
		if reply == strings.ToLower(o.ID) || reply == strings.ToLower(o.Label) {
			return o, true
		}
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
	return q.Option(id)
}

func formAnswerFromText(q Question, text string) (Answer, error) {
	fields := q.Fields()
	switch reply := normalize(text); reply {
	case ActionDecline, ActionCancel:
		return Answer{Option: reply}, nil
	case ActionAccept, "yes", "ok":
		if len(fields) == 0 {
			return Answer{Option: ActionAccept}, nil
		}
	}
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			return Answer{}, fmt.Errorf("the reply is not a valid JSON object: %w", err)
		}
		return q.AcceptForm(m)
	}
	if len(fields) == 1 {
		return q.AcceptForm(map[string]any{fields[0].Name: text})
	}
	return Answer{}, ErrNoAnswer
}

// AcceptForm returns the answer which accepts a form with the values. It
// checks them against the schema and converts text to the field types.
func (q Question) AcceptForm(values map[string]any) (Answer, error) {
	content := map[string]any{}
	for _, f := range q.Fields() {
		v, ok := values[f.Name]
		if !ok {
			if f.Required {
				return Answer{}, fmt.Errorf("the field %s is required", f.Name)
			}
			continue
		}
		cv, err := convertField(f, v)
		if err != nil {
			return Answer{}, err
		}
		content[f.Name] = cv
	}
	return Answer{Option: ActionAccept, Content: content}, nil
}

func convertField(f Field, v any) (any, error) {
	s, isText := v.(string)
	s = strings.TrimSpace(s)
	switch f.Type {
	case "number", "integer":
		if n, ok := v.(float64); ok {
			return n, nil
		}
		if n, err := strconv.ParseFloat(s, 64); isText && err == nil {
			return n, nil
		}
		return nil, fmt.Errorf("the field %s must be a number", f.Name)
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
		return nil, fmt.Errorf("the field %s must be true or false", f.Name)
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
					return nil, fmt.Errorf("the field %s must be a list of strings", f.Name)
				}
				items = append(items, str)
			}
		}
		out := make([]any, len(items))
		for i, item := range items {
			if len(f.Choices) > 0 && !slices.Contains(f.Choices, item) {
				return nil, fmt.Errorf("%q is not a valid choice for %s", item, f.Name)
			}
			out[i] = item
		}
		return out, nil
	default:
		if !isText {
			return nil, fmt.Errorf("the field %s must be text", f.Name)
		}
		if len(f.Choices) > 0 && !slices.Contains(f.Choices, s) {
			return nil, fmt.Errorf("%q is not a valid choice for %s", s, f.Name)
		}
		return s, nil
	}
}
