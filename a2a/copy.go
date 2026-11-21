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

package a2a

import (
	"encoding/json"
	"time"
)

// CopyTask returns a deep copy of the task. The result does not share any
// mutable state with the input.
func CopyTask(t *Task) *Task {
	if t == nil {
		return nil
	}
	cp := *t
	cp.Artifacts = copyArtifacts(t.Artifacts)
	cp.History = copyMessages(t.History)
	cp.Metadata = copyAnyMap(t.Metadata)
	cp.Status = copyTaskStatus(t.Status)
	return &cp
}

// CopyArtifact returns a deep copy of the artifact. The result does not share
// any mutable state with the input.
func CopyArtifact(a *Artifact) *Artifact {
	if a == nil {
		return nil
	}
	cp := *a
	cp.Extensions = copyStringSlice(a.Extensions)
	cp.Metadata = copyAnyMap(a.Metadata)
	cp.Parts = copyParts(a.Parts)
	return &cp
}

// CopyMessage returns a deep copy of the message. The result does not share
// any mutable state with the input.
func CopyMessage(m *Message) *Message {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Extensions = copyStringSlice(m.Extensions)
	cp.Metadata = copyAnyMap(m.Metadata)
	cp.Parts = copyParts(m.Parts)
	cp.ReferenceTasks = copyTaskIDSlice(m.ReferenceTasks)
	return &cp
}

// CopyPart returns a deep copy of the part. The result does not share any
// mutable state with the input.
func CopyPart(p *Part) *Part {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Content = copyPartContent(p.Content)
	cp.Metadata = copyAnyMap(p.Metadata)
	return &cp
}

// CopyPushConfig returns a deep copy of the push config. The result does not
// share any mutable state with the input.
func CopyPushConfig(c *PushConfig) *PushConfig {
	if c == nil {
		return nil
	}
	cp := *c
	if c.Auth != nil {
		authCopy := *c.Auth
		cp.Auth = &authCopy
	}
	return &cp
}

func copyTaskStatus(s TaskStatus) TaskStatus {
	cp := s
	cp.Message = CopyMessage(s.Message)
	if s.Timestamp != nil {
		ts := *s.Timestamp
		cp.Timestamp = &ts
	}
	return cp
}

func copyArtifacts(in []*Artifact) []*Artifact {
	if in == nil {
		return nil
	}
	out := make([]*Artifact, len(in))
	for i, a := range in {
		out[i] = CopyArtifact(a)
	}
	return out
}

func copyMessages(in []*Message) []*Message {
	if in == nil {
		return nil
	}
	out := make([]*Message, len(in))
	for i, m := range in {
		out[i] = CopyMessage(m)
	}
	return out
}

func copyParts(in ContentParts) ContentParts {
	if in == nil {
		return nil
	}
	out := make(ContentParts, len(in))
	for i, p := range in {
		out[i] = CopyPart(p)
	}
	return out
}

func copyPartContent(c PartContent) PartContent {
	switch v := c.(type) {
	case Text:
		return v
	case URL:
		return v
	case Raw:
		return Raw(append([]byte(nil), v...))
	case Data:
		return Data{Value: cloneAny(v.Value)}
	case nil:
		return nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return v
		}
		var dst any
		if err := json.Unmarshal(raw, &dst); err != nil {
			return v
		}
		return Data{Value: dst}
	}
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func copyTaskIDSlice(in []TaskID) []TaskID {
	if in == nil {
		return nil
	}
	out := make([]TaskID, len(in))
	copy(out, in)
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAny(v)
	}
	return out
}

func cloneAny(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return val
	case bool:
		return val
	case int:
		return val
	case int8:
		return val
	case int16:
		return val
	case int32:
		return val
	case int64:
		return val
	case uint:
		return val
	case uint8:
		return val
	case uint16:
		return val
	case uint32:
		return val
	case uint64:
		return val
	case float32:
		return val
	case float64:
		return val
	case time.Time:
		return val
	case map[string]any:
		return copyAnyMap(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return copyStringSlice(val)
	case []byte:
		return append([]byte(nil), val...)
	default:
		raw, err := json.Marshal(val)
		if err != nil {
			return val
		}
		var dst any
		if err := json.Unmarshal(raw, &dst); err != nil {
			return val
		}
		return dst
	}
}
