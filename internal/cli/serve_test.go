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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestServe_ModeValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"no mode", []string{"serve"}},
		{"multiple modes", []string{"serve", "--echo", "--exec", "cat"}},
	} {
		t.Run(tt.name+" fails", func(t *testing.T) {
			t.Parallel()
			if _, err := runCMD(t, tt.args...); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExecExecutor_PipesStdinToCommand(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t, "cat", "")

	task := execSendText(t, client, "echo back")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("task.Status.State = %v, want %v", task.Status.State, a2a.TaskStateCompleted)
	}
	got := allArtifactText(task)
	if diff := cmp.Diff("echo back", got); diff != "" {
		t.Fatalf("artifact text wrong result (+got,-want) diff = %s", diff)
	}
}

func TestExecExecutor_CommandOutput(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t, "echo hello", "")

	task := execSendText(t, client, "ignored")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("task.Status.State = %v, want %v", task.Status.State, a2a.TaskStateCompleted)
	}
	got := allArtifactText(task)
	if diff := cmp.Diff("hello\n", got); diff != "" {
		t.Fatalf("artifact text wrong result (+got,-want) diff = %s", diff)
	}
}

func TestExecExecutor_NonZeroExitFails(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t, "exit 1", "")

	task := execSendText(t, client, "will fail")

	if task.Status.State != a2a.TaskStateFailed {
		t.Fatalf("task.Status.State = %v, want %v", task.Status.State, a2a.TaskStateFailed)
	}
}

func TestExecExecutor_ChunkedAssemblesArtifact(t *testing.T) {
	t.Parallel()
	client := startExecTestServer(t, `printf "a\nb\nc"`, "\n")

	task := execSendText(t, client, "go")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("task.Status.State = %v, want %v", task.Status.State, a2a.TaskStateCompleted)
	}
	if len(task.Artifacts) == 0 {
		t.Fatal("task has no artifacts")
	}
	got := allArtifactText(task)
	if got != "abc" {
		t.Fatalf("artifact text = %q, want %q", got, "abc")
	}
}

func TestExecExecutor_ChunkedStreaming(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client := startExecTestServer(t, `printf "x\ny\nz"`, "\n")

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("go"))
	var gotArtifactEvents int
	var lastState a2a.TaskState
	for event, err := range client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: msg}) {
		if err != nil {
			t.Fatalf("SendStreamingMessage() error = %v", err)
		}
		switch e := event.(type) {
		case *a2a.TaskArtifactUpdateEvent:
			gotArtifactEvents++
		case *a2a.TaskStatusUpdateEvent:
			lastState = e.Status.State
		case *a2a.Task:
			lastState = e.Status.State
		}
	}

	if gotArtifactEvents == 0 {
		t.Fatal("expected at least one artifact event")
	}
	if lastState != a2a.TaskStateCompleted {
		t.Fatalf("last task state = %v, want %v", lastState, a2a.TaskStateCompleted)
	}
}

func TestSplitByDelimiter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		delim string
		want  []string
	}{
		{"newline", "a\nb\nc", "\n", []string{"a", "b", "c"}},
		{"space", "alpha beta gamma", " ", []string{"alpha", "beta", "gamma"}},
		{"paragraph", "one\n\ntwo\n\nthree", "\n\n", []string{"one", "two", "three"}},
		{"no delimiter", "all one chunk", "|", []string{"all one chunk"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			split := splitByDelimiter(tt.delim)
			var got []string
			data := []byte(tt.input)
			for {
				advance, token, _ := split(data, true)
				if advance == 0 {
					break
				}
				got = append(got, string(token))
				data = data[advance:]
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("splitByDelimiter(%q) wrong result (+got,-want) diff = %s", tt.delim, diff)
			}
		})
	}
}

func startExecTestServer(t *testing.T, command, chunk string) *a2aclient.Client {
	t.Helper()
	ctx := t.Context()

	executor := newExecExecutor(command, chunk)
	streaming := chunk != ""
	handler := a2asrv.NewHandler(executor)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	card := &a2a.AgentCard{
		Name:    "Exec Test",
		Version: "1.0.0",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL, a2a.TransportProtocolHTTPJSON),
		},
		Capabilities: a2a.AgentCapabilities{Streaming: streaming},
	}
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/", a2asrv.NewRESTHandler(handler))

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Destroy() })
	return client
}

func execSendText(t *testing.T, client *a2aclient.Client, text string) *a2a.Task {
	t.Helper()
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text))
	result, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("client.SendMessage() error = %v", err)
	}
	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("client.SendMessage() result type = %T, want *a2a.Task", result)
	}
	return task
}

func allArtifactText(task *a2a.Task) string {
	var sb strings.Builder
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			sb.WriteString(p.Text())
		}
	}
	return sb.String()
}
