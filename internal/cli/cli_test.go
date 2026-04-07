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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestDiscover(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)

	t.Run("returns agent card", func(t *testing.T) {
		t.Parallel()
		out := mustRunCMD(t, "discover", url, "-o", "json")
		var card a2a.AgentCard
		if err := json.Unmarshal([]byte(out), &card); err != nil {
			t.Fatalf("json.Unmarshal(discover output) error = %v", err)
		}
		if card.Name != "Test Echo" {
			t.Fatalf("a2a discover card.Name = %q, want %q", card.Name, "Test Echo")
		}
		if !card.Capabilities.Streaming {
			t.Fatal("a2a discover card.Capabilities.Streaming = false, want true")
		}
	})

	t.Run("missing url fails", func(t *testing.T) {
		t.Parallel()
		if _, err := runCMD(t, "discover"); err == nil {
			t.Fatal("a2a discover (no url) should fail")
		}
	})
}

func TestGetCard(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)

	out := mustRunCMD(t, "get", "card", url, "-o", "json")
	var card a2a.AgentCard
	if err := json.Unmarshal([]byte(out), &card); err != nil {
		t.Fatalf("json.Unmarshal(get card output) error = %v", err)
	}
	if card.Name != "Test Echo" {
		t.Fatalf("a2a get card card.Name = %q, want %q", card.Name, "Test Echo")
	}
}

func TestSend(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)

	msgText := "hello hello!"
	msgJSON := fmt.Sprintf(`{"role":"ROLE_USER","parts":[{"text":"%s"}]}`, msgText)
	path := filepath.Join(t.TempDir(), "msg.json")
	if err := os.WriteFile(path, []byte(msgJSON), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	sendTests := []struct {
		name     string
		args     []string
		wantText string
		wantErr  bool
	}{
		{
			name:     "text",
			args:     []string{"send", url, "-o", "json", msgText},
			wantText: msgText,
		},
		{
			name:     "parts",
			args:     []string{"send", url, "-o", "json", "--parts", `[{"text":"part one"},{"text":"part two"}]`},
			wantText: "part one part two",
		},
		{
			name:     "message json",
			args:     []string{"send", url, "-o", "json", "--json", msgJSON},
			wantText: msgText,
		},
		{
			name:     "message from file",
			args:     []string{"send", url, "-o", "json", "-f", path},
			wantText: msgText,
		},
		{
			name:    "fails when no message",
			args:    []string{"send", url},
			wantErr: true,
		},
		{
			name:    "fails when no url",
			args:    []string{"send"},
			wantErr: true,
		},
		{
			name:    "fails on bad --json",
			args:    []string{"send", url, "--json", "{bad"},
			wantErr: true,
		},
	}
	for _, tt := range sendTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := runCMD(t, tt.args...)
			if err != nil && tt.wantErr {
				return
			}
			if err != nil {
				t.Fatalf("runCMD(%q) error = %v", strings.Join(tt.args, " "), err)
			}
			var task a2a.Task
			if err := json.Unmarshal([]byte(out), &task); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if text := allArtifactText(&task); text != tt.wantText {
				t.Fatalf("allArtifactText() = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestSendStreaming(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)
	out := mustRunCMD(t, "send", url, "-o", "json", "--stream", "stream me")
	dec := json.NewDecoder(strings.NewReader(out))
	var events []a2a.StreamResponse
	for dec.More() {
		var sr a2a.StreamResponse
		if err := dec.Decode(&sr); err != nil {
			t.Fatalf("json.Decode(event %d) error = %v", len(events), err)
		}
		if sr.Event == nil {
			t.Fatalf("json.Decode(event %d) produced nil Event", len(events))
		}
		events = append(events, sr)
	}
	if len(events) <= 1 {
		t.Fatalf("a2a send --stream produced %d events, want > 1", len(events))
	}
}

func TestGetTask(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)

	taskID := sendTestMessage(t, url, "setup")

	t.Run("get task by id", func(t *testing.T) {
		t.Parallel()
		out := mustRunCMD(t, "get", "task", url, string(taskID), "-o", "json")
		var task a2a.Task
		if err := json.Unmarshal([]byte(out), &task); err != nil {
			t.Fatalf("json.Unmarshal(get task output) error = %v", err)
		}
		if task.ID != taskID {
			t.Fatalf("a2a get task ID = %q, want %q", task.ID, taskID)
		}
		if task.Status.State != a2a.TaskStateCompleted {
			t.Fatalf("a2a get task Status.State = %q, want %q", task.Status.State, a2a.TaskStateCompleted)
		}
	})

	t.Run("get task with --history", func(t *testing.T) {
		t.Parallel()
		out := mustRunCMD(t, "get", "task", url, string(taskID), "--history", "10", "-o", "json")
		var task a2a.Task
		if err := json.Unmarshal([]byte(out), &task); err != nil {
			t.Fatalf("json.Unmarshal(get task --history output) error = %v", err)
		}
		if task.ID != taskID {
			t.Fatalf("a2a get task --history ID = %q, want %q", task.ID, taskID)
		}
		if len(task.History) == 0 {
			t.Fatal("a2a get task --history returned no history")
		}
	})

	t.Run("missing args fails", func(t *testing.T) {
		t.Parallel()
		if _, err := runCMD(t, "get", "task", url); err == nil {
			t.Fatal("a2a get task (missing id) should fail")
		}
	})
}

func startTestServer(t *testing.T) string {
	t.Helper()

	handler := a2asrv.NewHandler(&echoExecutor{},
		a2asrv.WithCapabilityChecks(&a2a.AgentCapabilities{Streaming: true}),
	)

	mux := http.NewServeMux()
	mux.Handle("/", a2asrv.NewRESTHandler(handler))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&a2a.AgentCard{
		Name:                "Test Echo",
		Version:             "1.0.0",
		Capabilities:        a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(server.URL, a2a.TransportProtocolHTTPJSON)},
	}))

	return server.URL
}

func sendTestMessage(t *testing.T, url, text string) a2a.TaskID {
	t.Helper()
	ctx := t.Context()

	client, err := a2aclient.NewFromEndpoints(ctx, []*a2a.AgentInterface{
		a2a.NewAgentInterface(url, a2a.TransportProtocolHTTPJSON),
	})
	if err != nil {
		t.Fatalf("a2aclient.NewFromEndpoints() error = %v", err)
	}
	defer func() { _ = client.Destroy() }()

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	result, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("SendMessage() result type = %T, want *a2a.Task", result)
	}
	return task.ID
}

func mustRunCMD(t *testing.T, args ...string) string {
	t.Helper()
	r, err := runCMD(t, args...)
	if err != nil {
		t.Fatalf("runCMD(%q) error = %v", strings.Join(args, " "), err)
	}
	return r
}

func runCMD(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cfg := &globalConfig{}
	root := newRootCmd(cfg, &buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}
