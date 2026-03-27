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

package log

import (
	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// DefaultA2ATypeFormatter is a [TypeFormatter] that formats [a2a.Event] types as concise
// structured groups containing only the fields most useful for operational logging.
func DefaultA2ATypeFormatter(val any) (slog.Value, bool) {
	switch v := val.(type) {
	case *a2a.Task:
		if v == nil {
			return slog.Value{}, false
		}
		return slog.GroupValue(
			slog.String("id", string(v.ID)),
			slog.String("context_id", v.ContextID),
			slog.String("state", v.Status.State.String()),
		), true

	case *a2a.Message:
		if v == nil {
			return slog.Value{}, false
		}
		attrs := []slog.Attr{
			slog.String("id", v.ID),
			slog.String("role", v.Role.String()),
		}
		if v.TaskID != "" {
			attrs = append(attrs, slog.String("task_id", string(v.TaskID)))
		}
		if v.ContextID != "" {
			attrs = append(attrs, slog.String("context_id", string(v.ContextID)))
		}
		attrs = append(attrs, slog.Int("parts", len(v.Parts)))
		return slog.GroupValue(attrs...), true

	case *a2a.TaskStatusUpdateEvent:
		if v == nil {
			return slog.Value{}, false
		}
		return slog.GroupValue(
			slog.String("task_id", string(v.TaskID)),
			slog.String("context_id", string(v.ContextID)),
			slog.String("state", v.Status.State.String()),
		), true

	case *a2a.TaskArtifactUpdateEvent:
		if v == nil {
			return slog.Value{}, false
		}
		attrs := []slog.Attr{
			slog.String("task_id", string(v.TaskID)),
			slog.String("context_id", string(v.ContextID)),
		}
		if v.Artifact != nil {
			attrs = append(attrs, slog.String("artifact_id", string(v.Artifact.ID)))
			attrs = append(attrs, slog.Int("parts", len(v.Artifact.Parts)))
		}
		attrs = append(attrs,
			slog.Bool("append", v.Append),
			slog.Bool("last_chunk", v.LastChunk),
		)
		return slog.GroupValue(attrs...), true
	}
	return slog.Value{}, false
}
