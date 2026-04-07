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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

var taskStateNames = map[a2a.TaskState]string{
	a2a.TaskStateSubmitted:     "submitted",
	a2a.TaskStateWorking:       "working",
	a2a.TaskStateCompleted:     "completed",
	a2a.TaskStateFailed:        "failed",
	a2a.TaskStateCanceled:      "canceled",
	a2a.TaskStateRejected:      "rejected",
	a2a.TaskStateInputRequired: "input-required",
	a2a.TaskStateAuthRequired:  "auth-required",
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (g *globalConfig) printCard(card *a2a.AgentCard) error {
	if g.output == "json" {
		return printJSON(g.out, card)
	}
	_, err := io.WriteString(g.out, formatCard(card))
	return err
}

func (g *globalConfig) printTask(task *a2a.Task) error {
	if g.output == "json" {
		return printJSON(g.out, task)
	}
	_, err := io.WriteString(g.out, formatTask(task))
	return err
}

func (g *globalConfig) printEvent(event a2a.Event) error {
	if g.output == "json" {
		return printJSON(g.out, a2a.StreamResponse{Event: event})
	}
	var s string
	switch e := event.(type) {
	case *a2a.TaskStatusUpdateEvent:
		state := shortState(e.Status.State)
		if e.Status.Message != nil {
			s = fmt.Sprintf("[status] %s: %s\n", state, messageText(e.Status.Message))
		} else {
			s = fmt.Sprintf("[status] %s\n", state)
		}
	case *a2a.TaskArtifactUpdateEvent:
		text := partsText(e.Artifact.Parts)
		if e.Append {
			s = fmt.Sprintf("[artifact+] %s\n", text)
		} else {
			s = fmt.Sprintf("[artifact] %s\n", text)
		}
	case *a2a.Task:
		s = formatTask(e)
	case *a2a.Message:
		s = formatMessage(e)
	default:
		return nil
	}
	_, err := io.WriteString(g.out, s)
	return err
}

func (g *globalConfig) printSendResult(result a2a.SendMessageResult) error {
	if g.output == "json" {
		return printJSON(g.out, result)
	}
	switch r := result.(type) {
	case *a2a.Task:
		_, err := io.WriteString(g.out, formatTask(r))
		return err
	case *a2a.Message:
		_, err := io.WriteString(g.out, formatMessage(r))
		return err
	}
	return nil
}

func (g *globalConfig) printTaskList(resp *a2a.ListTasksResponse) error {
	if g.output == "json" {
		return printJSON(g.out, resp)
	}
	_, err := io.WriteString(g.out, formatTaskList(resp))
	return err
}

func formatCard(card *a2a.AgentCard) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name:         %s\n", card.Name)
	if card.Description != "" {
		fmt.Fprintf(&sb, "Description:  %s\n", card.Description)
	}
	fmt.Fprintf(&sb, "Version:      %s\n", card.Version)

	if len(card.SupportedInterfaces) > 0 {
		sb.WriteString("Interfaces:\n")
		for _, iface := range card.SupportedInterfaces {
			fmt.Fprintf(&sb, "  %-12s %s\n", iface.ProtocolBinding, iface.URL)
		}
	}

	fmt.Fprintf(&sb, "Streaming:    %v\n", card.Capabilities.Streaming)

	if len(card.Skills) > 0 {
		sb.WriteString("Skills:\n")
		for _, s := range card.Skills {
			fmt.Fprintf(&sb, "  %-20s %s\n", s.ID, s.Name)
		}
	}

	return sb.String()
}

func formatTask(task *a2a.Task) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Task:     %s\n", task.ID)
	if task.ContextID != "" {
		fmt.Fprintf(&sb, "Context:  %s\n", task.ContextID)
	}
	fmt.Fprintf(&sb, "Status:   %s", shortState(task.Status.State))
	if task.Status.Timestamp != nil {
		fmt.Fprintf(&sb, " (%s)", task.Status.Timestamp.Format("2006-01-02T15:04:05Z07:00"))
	}
	fmt.Fprintf(&sb, "\n")
	if task.Status.Message != nil {
		fmt.Fprintf(&sb, "  %s\n", messageText(task.Status.Message))
	}

	if len(task.Artifacts) > 0 {
		sb.WriteString("Artifacts:\n")
		for _, art := range task.Artifacts {
			label := string(art.ID)
			if art.Name != "" {
				label = art.Name
			}
			fmt.Fprintf(&sb, "  [%s] %s\n", label, partsText(art.Parts))
		}
	}

	if len(task.History) > 0 {
		sb.WriteString("History:\n")
		for _, msg := range task.History {
			role := "user"
			if msg.Role == a2a.MessageRoleAgent {
				role = "agent"
			}
			fmt.Fprintf(&sb, "  [%s] %s\n", role, messageText(msg))
		}
	}

	return sb.String()
}

func formatMessage(msg *a2a.Message) string {
	role := "user"
	if msg.Role == a2a.MessageRoleAgent {
		role = "agent"
	}
	return fmt.Sprintf("[%s] %s\n", role, messageText(msg))
}

func formatTaskList(resp *a2a.ListTasksResponse) string {
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 4, 2, ' ', 0)
	_, _ = io.WriteString(tw, "ID\tSTATUS\tCONTEXT\n")
	for _, t := range resp.Tasks {
		_, _ = io.WriteString(tw, fmt.Sprintf("%s\t%s\t%s\n", t.ID, shortState(t.Status.State), t.ContextID))
	}
	_ = tw.Flush()
	if resp.NextPageToken != "" {
		fmt.Fprintf(&sb, "\nNext page token: %s\n", resp.NextPageToken)
	}
	return sb.String()
}

func messageText(msg *a2a.Message) string {
	return partsText(msg.Parts)
}

func partsText(parts a2a.ContentParts) string {
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(" ")
		}
		if t := p.Text(); t != "" {
			sb.WriteString(t)
			continue
		}
		if u := p.URL(); u != "" {
			sb.WriteString("[file: ")
			sb.WriteString(string(u))
			sb.WriteString("]")
			continue
		}
		if p.Raw() != nil {
			fmt.Fprintf(&sb, "[binary %d bytes]", len(p.Raw()))
			continue
		}
		if p.Data() != nil {
			b, err := json.Marshal(p.Data())
			if err != nil {
				fmt.Fprintf(&sb, "[data: %v]", err)
				continue
			}
			sb.WriteString(string(b))
			continue
		}
	}
	return sb.String()
}

func shortState(state a2a.TaskState) string {
	if name, ok := taskStateNames[state]; ok {
		return name
	}
	return string(state)
}

func parseTaskState(s string) (a2a.TaskState, error) {
	lower := strings.ToLower(s)
	for state, name := range taskStateNames {
		if name == lower {
			return state, nil
		}
	}
	return "", fmt.Errorf("unknown task state %q", s)
}
