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

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestAgentFirstOutputTimeout_ModelSmoke(t *testing.T) {
	t.Parallel()
	if os.Getenv("A2A_RUN_MODEL_SMOKE") != "1" {
		t.Skip("set A2A_RUN_MODEL_SMOKE=1, OPENAI_BASE_URL, OPENAI_API_KEY, and MODEL_NAME to run")
	}
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "MODEL_NAME"} {
		if os.Getenv(key) == "" {
			t.Fatalf("%s must be set", key)
		}
	}
	for _, suppress := range []bool{false, true} {
		name := "output_disarms_deadline"
		if suppress {
			name = "withheld_output_times_out_despite_progress"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			const budget = 30 * time.Second
			causes := make(chan error, 1)
			connected := make(chan struct{})
			executor := a2asrv.AgentExecutorFunc(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
				return func(yield func(a2a.Event, error) bool) {
					defer func() { causes <- context.Cause(ctx) }()
					if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
						return
					}
					modelCtx, cancel := context.WithCancel(ctx)
					chunks := make(chan string)
					done := make(chan error, 1)
					go func() {
						done <- streamSmokeModel(modelCtx, suppress, connected, func(text string) error {
							select {
							case chunks <- text:
								return nil
							case <-modelCtx.Done():
								return context.Cause(modelCtx)
							}
						})
					}()
					defer func() {
						cancel()
						if done != nil {
							<-done
						}
					}()
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					var finish <-chan time.Time
					for {
						select {
						case <-ctx.Done():
							yield(nil, context.Cause(ctx))
							return
						case text := <-chunks:
							if !yield(a2a.NewArtifactEvent(ec, a2a.NewTextPart(text)), nil) {
								return
							}
						case err := <-done:
							done = nil
							if err != nil {
								yield(nil, err)
								return
							}
							finish = time.After(budget + time.Second)
						case <-finish:
							yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, nil), nil)
							return
						case <-ticker.C:
							if !yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateWorking, nil), nil) {
								return
							}
						}
					}
				}
			})
			handler := a2asrv.NewHandler(executor, a2asrv.WithAgentFirstOutputTimeout(budget, matchesArtifact), a2asrv.WithAgentInactivityTimeout(5*time.Second))
			client := firstOutputHTTPClient(t, handler, a2a.TransportProtocolJSONRPC)
			ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
			defer cancel()
			start := time.Now()
			var firstOutput time.Duration
			var taskID a2a.TaskID
			var state a2a.TaskState
			var artifacts, progress int
			for event, err := range client.SendStreamingMessage(ctx, &a2a.SendMessageRequest{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("Reply with exactly the word hello."))}) {
				if err != nil {
					t.Fatalf("client.SendStreamingMessage() error = %v, want nil", err)
				}
				switch e := event.(type) {
				case *a2a.Task:
					taskID, state = e.ID, e.Status.State
				case *a2a.TaskStatusUpdateEvent:
					state = e.Status.State
					progress++
				case *a2a.TaskArtifactUpdateEvent:
					if artifacts == 0 {
						firstOutput = time.Since(start)
					}
					artifacts++
				}
			}
			cause := <-causes
			select {
			case <-connected:
			default:
				t.Fatal("model request did not establish a successful HTTP response")
			}
			want := a2a.TaskStateCompleted
			if suppress {
				want = a2a.TaskStateFailed
				if !errors.Is(cause, a2asrv.ErrAgentFirstOutputTimeout) || artifacts != 0 {
					t.Fatalf("execution cause = %v, artifacts = %d, want first-output timeout and no artifacts", cause, artifacts)
				}
			} else if artifacts == 0 || time.Since(start) <= budget {
				t.Fatalf("artifacts = %d, duration = %v, want model output followed by a stream longer than %v", artifacts, time.Since(start), budget)
			}
			task, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: taskID})
			if err != nil || state != want || task.Status.State != want || progress == 0 {
				t.Fatalf("GetTask() = %v, %v, stream state = %v, progress = %d, want state %v and progress events", task, err, state, progress, want)
			}
			t.Logf("state=%v artifacts=%d progress=%d first_output=%v duration=%v", state, artifacts, progress, firstOutput, time.Since(start))
		})
	}
}

func streamSmokeModel(ctx context.Context, suppress bool, connected chan<- struct{}, onText func(string) error) (err error) {
	body := map[string]any{
		"model": os.Getenv("MODEL_NAME"), "stream": true, "max_tokens": 64,
		"messages": []map[string]string{{"role": "user", "content": "Reply with exactly the word hello."}},
	}
	if extra := os.Getenv("A2A_MODEL_EXTRA_BODY"); extra != "" {
		if err := json.Unmarshal([]byte(extra), &body); err != nil {
			return fmt.Errorf("invalid A2A_MODEL_EXTRA_BODY: %w", err)
		}
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/")+"/chat/completions", bytes.NewReader(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("model HTTP status = %d, want 200", resp.StatusCode)
	}
	close(connected)
	if suppress {
		<-ctx.Done()
		return context.Cause(ctx)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct{ Content string }
			}
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if err := onText(choice.Delta.Content); err != nil {
					return err
				}
			}
		}
	}
	return scanner.Err()
}
