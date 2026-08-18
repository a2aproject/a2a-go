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
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	a2acorev0 "github.com/a2aproject/a2a-go/a2a"
	a2asrvv0 "github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestDiscover(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)
	legacyURL := startLegacyTestServer(t)

	modes := []struct {
		suffix string
		url    string
	}{{suffix: " v1", url: url}, {suffix: " v0", url: legacyURL}}

	for _, mode := range modes {
		t.Run("returns agent card"+mode.suffix, func(t *testing.T) {
			t.Parallel()
			out := mustRunCMD(t, "discover", mode.url, "-o", "json")
			var card a2a.AgentCard
			if err := json.Unmarshal([]byte(out), &card); err != nil {
				t.Fatalf("json.Unmarshal(discover output) error = %v", err)
			}
			if card.Name != "Test Echo" {
				t.Errorf("a2a discover card.Name = %q, want %q", card.Name, "Test Echo")
			}
			if !card.Capabilities.Streaming {
				t.Errorf("a2a discover card.Capabilities.Streaming = false, want true")
			}
			if len(card.SupportedInterfaces) == 0 {
				t.Errorf("a2a discover supported interfaces is empty")
			}
		})

		t.Run("returns agent card with complete card url"+mode.suffix, func(t *testing.T) {
			t.Parallel()
			out := mustRunCMD(t, "discover", mode.url+"/.well-known/agent-card.json", "-o", "json")
			var card a2a.AgentCard
			if err := json.Unmarshal([]byte(out), &card); err != nil {
				t.Fatalf("json.Unmarshal(discover output) error = %v", err)
			}
			if card.Name != "Test Echo" {
				t.Fatalf("a2a discover card.Name = %q, want %q", card.Name, "Test Echo")
			}
		})
	}

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

func TestVersion(t *testing.T) {
	t.Parallel()

	t.Run("text output", func(t *testing.T) {
		t.Parallel()
		out := mustRunCMD(t, "version")
		if !strings.HasPrefix(out, "a2a ") {
			t.Fatalf("a2a version output = %q, want it to start with %q", out, "a2a ")
		}
	})

	t.Run("json output", func(t *testing.T) {
		t.Parallel()
		out := mustRunCMD(t, "version", "-o", "json")
		var info versionInfo
		if err := json.Unmarshal([]byte(out), &info); err != nil {
			t.Fatalf("json.Unmarshal(version output) error = %v", err)
		}
		if info.Version == "" {
			t.Fatalf("a2a version info.Version = %q, want non-empty", info.Version)
		}
		if info.GoVersion == "" {
			t.Fatalf("a2a version info.GoVersion = %q, want non-empty", info.GoVersion)
		}
	})
}

func TestSend(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)
	legacyURL := startLegacyTestServer(t)

	msgText := "hello hello!"
	msgJSON := fmt.Sprintf(`{"role":"ROLE_USER","parts":[{"text":"%s"}]}`, msgText)
	path := filepath.Join(t.TempDir(), "msg.json")
	if err := os.WriteFile(path, []byte(msgJSON), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	sendTests := []struct {
		name     string
		args     func(url string) []string
		wantText string
		wantErr  bool
	}{
		{
			name: "text",
			args: func(url string) []string {
				return []string{"send", url, "-o", "json", msgText}
			},
			wantText: msgText,
		},
		{
			name: "parts",
			args: func(url string) []string {
				return []string{"send", url, "-o", "json", "--parts", `[{"text":"part one"},{"text":"part two"}]`}
			},
			wantText: "part one part two",
		},
		{
			name: "message json",
			args: func(url string) []string {
				return []string{"send", url, "-o", "json", "--json", msgJSON}
			},
			wantText: msgText,
		},
		{
			name: "message from file",
			args: func(url string) []string {
				return []string{"send", url, "-o", "json", "-f", path}
			},
			wantText: msgText,
		},
		{
			name: "fails when no message",
			args: func(url string) []string {
				return []string{"send", url}
			},
			wantErr: true,
		},
		{
			name: "fails when no url",
			args: func(url string) []string {
				return []string{"send"}
			},
			wantErr: true,
		},
		{
			name: "fails on bad --json",
			args: func(url string) []string {
				return []string{"send", url, "--json", "{bad"}
			},
			wantErr: true,
		},
	}

	modes := []struct {
		suffix string
		url    string
	}{{suffix: "_v1", url: url}, {suffix: "_v0", url: legacyURL}}

	for _, mode := range modes {
		for _, tt := range sendTests {
			t.Run(tt.name+mode.suffix, func(t *testing.T) {
				t.Parallel()
				out, err := runCMD(t, tt.args(mode.url)...)
				if err != nil && tt.wantErr {
					return
				}
				if err != nil {
					t.Fatalf("runCMD(%q) error = %v", strings.Join(tt.args(mode.url), " "), err)
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
}

func TestSendStreaming(t *testing.T) {
	t.Parallel()
	url := startTestServer(t)
	nonStreamingServerURL := startTestServerWith(t, a2a.AgentCapabilities{Streaming: false})

	testCases := []struct {
		name               string
		command            []string
		wantPollerFallback bool
	}{
		{
			name:               "streaming supported",
			command:            []string{"send", url, "-o", "json", "--stream", "stream me"},
			wantPollerFallback: false,
		},
		{
			name:               "poller fallback with create from card",
			command:            []string{"send", nonStreamingServerURL, "-o", "json", "--stream", "stream me"},
			wantPollerFallback: true,
		},
		{
			name:               "poller fallback with create from interface",
			command:            []string{"send", nonStreamingServerURL, "-o", "json", "--transport", "rest", "--stream", "stream me"},
			wantPollerFallback: true,
		},
	}
	for _, tc := range testCases {
		fallbackToPoller := false
		poller := pollerFunc(func(ctx context.Context, client *a2aclient.Client, req *a2a.SendMessageRequest, interval time.Duration) iter.Seq2[a2a.Event, error] {
			return func(yield func(a2a.Event, error) bool) {
				fallbackToPoller = true
				task := &a2a.Task{ID: a2a.NewTaskID(), ContextID: a2a.NewContextID(), Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}
				if !yield(task, nil) {
					return
				}
				yield(a2a.NewStatusUpdateEvent(task, a2a.TaskStateCompleted, nil), nil)
			}
		})
		out, err := runCMDWithPoller(t, poller, tc.command...)
		if err != nil {
			t.Fatalf("runCMDWithPoller() error = %v", err)
		}
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
		if fallbackToPoller != tc.wantPollerFallback {
			t.Fatalf("fallback to poller = %v, want the opposite", fallbackToPoller)
		}
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
	return startTestServerWith(t, a2a.AgentCapabilities{Streaming: true})
}

func startTestServerWith(t *testing.T, capabilities a2a.AgentCapabilities) string {
	t.Helper()

	handler := a2asrv.NewHandler(&echoExecutor{}, a2asrv.WithCapabilityChecks(&capabilities))

	mux := http.NewServeMux()
	mux.Handle("/", a2asrv.NewRESTHandler(handler))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(&a2a.AgentCard{
		Name:                "Test Echo",
		Version:             "1.0.0",
		Capabilities:        capabilities,
		SupportedInterfaces: []*a2a.AgentInterface{a2a.NewAgentInterface(server.URL, a2a.TransportProtocolHTTPJSON)},
	}))

	return server.URL
}

func startLegacyTestServer(t *testing.T) string {
	t.Helper()

	handler := a2asrvv0.NewHandler(&legacyExecutor{})

	mux := http.NewServeMux()
	mux.Handle("/", a2asrvv0.NewJSONRPCHandler(handler))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrvv0.NewStaticAgentCardHandler(&a2acorev0.AgentCard{
		Name:               "Test Echo",
		Version:            "1.0.0",
		URL:                server.URL,
		PreferredTransport: a2acorev0.TransportProtocol(a2a.TransportProtocolJSONRPC),
		Capabilities:       a2acorev0.AgentCapabilities{Streaming: true},
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
	return runCMDWithPoller(t, handlePolling, args...)
}

func runCMDWithPoller(t *testing.T, poller pollerFunc, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cfg := &globalConfig{}
	root := newRootCmd(cfg, &buf, poller)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

type legacyExecutor struct{}

func (e *legacyExecutor) Execute(ctx context.Context, reqCtx *a2asrvv0.RequestContext, queue eventqueue.Queue) error {
	if err := queue.Write(ctx, a2acorev0.NewStatusUpdateEvent(reqCtx, a2acorev0.TaskState(a2a.TaskStateSubmitted), nil)); err != nil {
		return err
	}
	msg, err := a2av0.ToV1Message(reqCtx.Message)
	if err != nil {
		return err
	}
	echo := a2acorev0.TextPart{Text: messageText(msg)}
	if err := queue.Write(ctx, a2acorev0.NewArtifactEvent(reqCtx, echo)); err != nil {
		return err
	}
	finalEvent := a2acorev0.NewStatusUpdateEvent(reqCtx, a2acorev0.TaskState(a2a.TaskStateCompleted), nil)
	finalEvent.Final = true
	return queue.Write(ctx, finalEvent)
}

func (e *legacyExecutor) Cancel(ctx context.Context, reqCtx *a2asrvv0.RequestContext, queue eventqueue.Queue) error {
	return fmt.Errorf("not implemented")
}
