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

package a2atest_test

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2atest"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestServerSendMessage(t *testing.T) {
	t.Parallel()

	exec := a2atest.ExecutorFromEvents(
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")),
	)
	server := a2atest.NewServer(t, exec)

	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi")),
	}
	result, err := server.SendMessage(t.Context(), req)
	if err != nil {
		t.Fatalf("server.SendMessage() error = %v, want nil", err)
	}
	msg, ok := result.(*a2a.Message)
	if !ok {
		t.Fatalf("expected *a2a.Message, got %T", result)
	}
	texts := a2atest.GetTextParts(msg)
	if diff := cmp.Diff([]string{"hello"}, texts); diff != "" {
		t.Fatalf("GetTextParts() mismatch (-want +got):\n%s", diff)
	}
}

func TestServerSendMessage_TaskResult(t *testing.T) {
	t.Parallel()

	// The executor yields a Message, which the system wraps in an implicit task.
	// The SendMessage result is the agent message.
	exec := a2atest.ExecutorFromEvents(
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done")),
	)
	server := a2atest.NewServer(t, exec)

	req := &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("go")),
	}
	result, err := server.SendMessage(t.Context(), req)
	if err != nil {
		t.Fatalf("server.SendMessage() error = %v, want nil", err)
	}

	got := a2atest.MustToMessage(t, result)
	texts := a2atest.GetTextParts(got)
	if diff := cmp.Diff([]string{"done"}, texts); diff != "" {
		t.Fatalf("GetTextParts() mismatch (-want +got):\n%s", diff)
	}
}

func TestGetTextParts(t *testing.T) {
	t.Parallel()

	t.Run("message", func(t *testing.T) {
		t.Parallel()
		msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello"), a2a.NewTextPart("world"))
		texts := a2atest.GetTextParts(msg)
		if diff := cmp.Diff([]string{"hello", "world"}, texts); diff != "" {
			t.Fatalf("GetTextParts() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("status update", func(t *testing.T) {
		t.Parallel()
		update := &a2a.TaskStatusUpdateEvent{
			TaskID: a2a.NewTaskID(),
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateCompleted,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("all done")),
			},
		}
		texts := a2atest.GetTextParts(update)
		if diff := cmp.Diff([]string{"all done"}, texts); diff != "" {
			t.Fatalf("GetTextParts() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("task returns nil", func(t *testing.T) {
		t.Parallel()
		task := &a2a.Task{ID: a2a.NewTaskID()}
		texts := a2atest.GetTextParts(task)
		if texts != nil {
			t.Fatalf("GetTextParts() = %v, want nil", texts)
		}
	})
}

func TestMustToTask(t *testing.T) {
	t.Parallel()
	task := &a2a.Task{ID: a2a.NewTaskID()}
	got := a2atest.MustToTask(t, task)
	if got != task {
		t.Fatal("MustToTask returned wrong pointer")
	}
}

func TestMustToMessage(t *testing.T) {
	t.Parallel()
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hi"))
	got := a2atest.MustToMessage(t, msg)
	if got != msg {
		t.Fatal("MustToMessage returned wrong pointer")
	}
}

func TestFromEvents(t *testing.T) {
	t.Parallel()

	want := []a2a.Event{
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("one")),
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("two")),
	}
	exec := a2atest.ExecutorFromEvents(want...)

	var got []a2a.Event
	for event, err := range exec.Execute(t.Context(), nil) {
		if err != nil {
			t.Fatalf("exec.Execute() error = %v, want nil", err)
		}
		got = append(got, event)
	}

	opts := []cmp.Option{
		cmpopts.IgnoreFields(a2a.Message{}, "ID"),
	}
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		t.Fatalf("FromEvents() mismatch (-want +got):\n%s", diff)
	}
}
