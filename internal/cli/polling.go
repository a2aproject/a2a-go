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
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
)

func handlePolling(ctx context.Context, client *a2aclient.Client, original *a2a.SendMessageRequest, interval time.Duration) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		req := *original
		if req.Config == nil {
			req.Config = &a2a.SendMessageConfig{ReturnImmediately: true}
		} else if !req.Config.ReturnImmediately {
			config := *original.Config
			req.Config = &config
			req.Config.ReturnImmediately = true
		}

		result, err := client.SendMessage(ctx, &req)
		if err != nil {
			yield(nil, fmt.Errorf("failed to send a message: %w", err))
			return
		}
		if !yield(result, nil) {
			return
		}
		if _, ok := result.(*a2a.Message); ok {
			return
		}
		prevState, ok := result.(*a2a.Task)
		if !ok {
			yield(nil, fmt.Errorf("unexpected type: %T", result))
			return
		}
		tid := prevState.ID

		successiveFailures := 0
		for !prevState.Status.State.Terminal() && prevState.Status.State != a2a.TaskStateInputRequired {
			time.Sleep(interval)

			task, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: tid})
			if err != nil {
				successiveFailures++
				if successiveFailures == 3 {
					yield(nil, fmt.Errorf("successive polling failure threshold exceeded for task %q", tid))
					return
				}
				continue
			}
			successiveFailures = 0

			var events []a2a.Event
			if task.Status.State.Terminal() || task.Status.State == a2a.TaskStateInputRequired {
				events = append(events, task)
			} else {
				events = a2aevent.Recover(prevState, task)
			}

			for _, event := range events {
				if !yield(event, nil) {
					return
				}
			}
			prevState = task
		}
	}
}
