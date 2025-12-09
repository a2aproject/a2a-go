// Copyright 2025 The A2A Authors
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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/log"
)

type agentExecutor struct {
	exitFunc func(int)
	workerID string
}

func newAgentExecutor(workerID string) *agentExecutor {
	return &agentExecutor{
		workerID: workerID,
		exitFunc: func(code int) {
			os.Exit(code)
		},
	}
}

func (a *agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	log.Info(ctx, "agent received task", "task_id", reqCtx.TaskID)

	text := reqCtx.Message.Parts[0].(a2a.TextPart).Text

	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	countTo := fs.Int("count-to", 0, "number to count to")
	dieEvery := fs.Int("die-every", 0, "die every N steps")
	interval := fs.Duration("interval", 1*time.Second, "interval in ms")

	if err := fs.Parse(strings.Fields(text)); err != nil {
		log.Info(ctx, "failed to interpret task", "task_id", reqCtx.TaskID)

		return q.Write(
			ctx,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: fmt.Sprintf("failed to interpret task: %v", err)}),
		)
	}

	if *countTo <= 0 {
		log.Info(ctx, "failed to interpret task", "task_id", reqCtx.TaskID)

		return q.Write(
			ctx,
			a2a.NewMessage(
				a2a.MessageRoleAgent,
				a2a.TextPart{Text: "Hello, world! Use --count-to=N --die-every=M --interval=I to test me."},
			))
	}

	start := 1
	if reqCtx.StoredTask == nil {
		log.Info(ctx, "task submitted", "task_id", reqCtx.TaskID)

		if err := q.Write(ctx, a2a.NewSubmittedTask(reqCtx, reqCtx.Message)); err != nil {
			return fmt.Errorf("queue write failed: %w", err)
		}
	} else if len(reqCtx.StoredTask.Artifacts) > 0 {
		lastArtifact := reqCtx.StoredTask.Artifacts[len(reqCtx.StoredTask.Artifacts)-1]
		if len(lastArtifact.Parts) > 0 {
			lastCount := lastArtifact.Parts[len(lastArtifact.Parts)-1].(a2a.TextPart).Text
			countPart := strings.Split(lastCount, ": ")[1]
			if val, err := strconv.Atoi(countPart); err == nil {
				start = val + 1
			}

			log.Info(ctx, "resuming from artifact checkpoint", "task_id", reqCtx.TaskID)
		}
	}

	log.Info(ctx, "agent starting", "start", start, "countTo", *countTo, "dieEvery", *dieEvery)

	dieMod := *dieEvery

	var artifactID a2a.ArtifactID
	for i := 0; i+start <= *countTo; i++ {
		if i > 0 && dieMod > 0 && i%dieMod == 0 {
			log.Info(ctx, "rebooting...", "iterations", i, "dieEvery", dieMod)
			a.exitFunc(1)
		}

		time.Sleep(*interval)

		log.Info(ctx, "counting", "value", i+start)

		chunk := fmt.Sprintf("%s: %d", a.workerID, i+start)
		var event *a2a.TaskArtifactUpdateEvent
		if artifactID == "" {
			event = a2a.NewArtifactEvent(reqCtx, a2a.TextPart{Text: chunk})
		} else {
			event = a2a.NewArtifactUpdateEvent(reqCtx, artifactID, a2a.TextPart{Text: chunk})
		}
		if err := q.Write(ctx, event); err != nil {
			return err
		}
	}

	taskCompleted := a2a.NewStatusUpdateEvent(
		reqCtx,
		a2a.TaskStateCompleted,
		a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Done!"}),
	)
	taskCompleted.Final = true
	return q.Write(ctx, taskCompleted)
}

func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return q.Write(
		ctx,
		a2a.NewStatusUpdateEvent(
			reqCtx,
			a2a.TaskStateCanceled,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Task cancelled"}),
		),
	)
}
