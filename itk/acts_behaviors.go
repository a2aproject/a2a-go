// ACTS SUT behaviour contract (ACTS spec §11).
//
// ACTS tests are declarative — they say what to send and what to expect — so
// the agent under test has to produce a deterministic reply for each case.
// §11 does that with a message-prefix convention rather than a side-channel
// API: the text of the first user message names the behaviour. Execute routes
// here when it sees a "tck-" prefix and otherwise runs the ITK instruction
// path, so one binary serves both suites.
//
// acts/sut-behaviors.yaml is what this SDK claims; this file is what it does.
// Nothing here restates the list of names — the prefix is read out of the
// message, and one that reaches actsDispatch without a branch fails the task
// rather than completing it, so a gap shows up in the conformance report
// instead of passing quietly.

package main

import (
	"context"
	"fmt"
	"iter"
	"regexp"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	actsMultiTurnDone = "done"

	// Short enough not to dominate a run, long enough that a test polling for
	// a non-terminal state sees one: the corpus polls every 2s, 15 times.
	actsLongRunningDelay = time.Second
)

// Greedy to the word boundary, which gives longest-match for free:
// "tck-artifact-file-url" beats "tck-artifact-file" with no ordered table.
var actsNamePattern = regexp.MustCompile(`^(tck-[a-z0-9]+(?:-[a-z0-9]+)*)`)

var actsTerminalStates = map[string]a2a.TaskState{
	"tck-complete-task":  a2a.TaskStateCompleted,
	"tck-task-failure":   a2a.TaskStateFailed,
	"tck-reject-task":    a2a.TaskStateRejected,
	"tck-input-required": a2a.TaskStateInputRequired,
	"tck-auth-required":  a2a.TaskStateAuthRequired,
}

func actsFirstText(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}
	for _, part := range msg.Parts {
		if text := part.Text(); text != "" {
			return text
		}
	}
	return ""
}

// actsBehaviorIn names an asserted behaviour, not necessarily an implemented
// one: an unknown "tck-" still routes to ACTS and is reported as
// unimplemented, which beats handing a message plainly meant for ACTS to the
// traversal decoder.
func actsBehaviorIn(text string) string {
	return actsNamePattern.FindString(strings.TrimSpace(text))
}

// actsBehaviorFor resolves the behaviour from the incoming message, falling
// back to the task's history. A multi-turn test opens with the prefix and then
// sends plain "here is more input" and "done", so a continuation has to
// recover the contract from where it was declared.
func actsBehaviorFor(execCtx *a2asrv.ExecutorContext) string {
	if named := actsBehaviorIn(actsFirstText(execCtx.Message)); named != "" {
		return named
	}
	if execCtx.StoredTask == nil {
		return ""
	}
	for _, historical := range execCtx.StoredTask.History {
		if found := actsBehaviorIn(actsFirstText(historical)); found != "" {
			return found
		}
	}
	return ""
}

func actsStatus(execCtx *a2asrv.ExecutorContext, state a2a.TaskState, text string) a2a.Event {
	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text))
	return a2a.NewStatusUpdateEvent(execCtx, state, msg)
}

func actsArtifact(execCtx *a2asrv.ExecutorContext, name string, parts ...*a2a.Part) a2a.Event {
	evt := a2a.NewArtifactEvent(execCtx, parts...)
	evt.Artifact.Name = name
	evt.LastChunk = true
	return evt
}

func actsRun(ctx context.Context, execCtx *a2asrv.ExecutorContext, behavior string) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		log.Info(ctx, "Serving ACTS behaviour", "behavior", behavior, "taskId", string(execCtx.TaskID))

		// This one must open no task at all: A2A lets an agent answer with a
		// bare Message, and a server that created a task first would turn the
		// reply into a task update, which is what CORE-SEND-003 checks.
		if behavior == "tck-message-response" {
			yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("tck message response")), nil)
			return
		}

		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}
		actsDispatch(ctx, execCtx, behavior, yield)
	}
}

func actsDispatch(ctx context.Context, execCtx *a2asrv.ExecutorContext, behavior string, yield func(a2a.Event, error) bool) {
	if behavior == actsClientParseBehavior {
		actsClientParse(ctx, execCtx, yield)
		return
	}

	if strings.HasPrefix(behavior, "tck-artifact-") {
		parts := actsArtifactParts(behavior)
		if parts == nil {
			actsUnimplemented(execCtx, behavior, yield)
			return
		}
		if !yield(actsArtifact(execCtx, behavior, parts...), nil) {
			return
		}
		yield(actsStatus(execCtx, a2a.TaskStateCompleted, behavior+" ok"), nil)
		return
	}

	switch behavior {
	case "tck-multi-turn":
		actsMultiTurn(execCtx, yield)

	case "tck-cancel":
		// Hold in WORKING. The framework cancels this context once the
		// terminal event from Cancel has been processed; yielding afterwards
		// would be rejected, so there is nothing to do but return.
		<-ctx.Done()

	case "tck-long-running":
		actsLongRunning(ctx, execCtx, yield)

	case "tck-stream-basic", "tck-stream-chunked":
		actsStream(execCtx, behavior, yield)

	default:
		state, ok := actsTerminalStates[behavior]
		if !ok {
			actsUnimplemented(execCtx, behavior, yield)
			return
		}
		yield(actsStatus(execCtx, state, behavior+" ok"), nil)
	}
}

// actsUnimplemented fails the task rather than completing it: a silent success
// would report conformance the agent never demonstrated.
func actsUnimplemented(execCtx *a2asrv.ExecutorContext, behavior string, yield func(a2a.Event, error) bool) {
	yield(actsStatus(execCtx, a2a.TaskStateFailed, fmt.Sprintf("unimplemented ACTS behaviour %q", behavior)), nil)
}

func actsMultiTurn(execCtx *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) {
	said := strings.ToLower(strings.TrimSpace(actsFirstText(execCtx.Message)))
	if strings.HasPrefix(said, actsMultiTurnDone) {
		yield(actsStatus(execCtx, a2a.TaskStateCompleted, "multi-turn complete"), nil)
		return
	}
	yield(actsStatus(execCtx, a2a.TaskStateInputRequired, "more input please"), nil)
}

func actsLongRunning(ctx context.Context, execCtx *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(actsLongRunningDelay):
	}

	// CORE-EXEC-001 polls to completion and then asserts the finished task
	// carries at least one artifact, so the work has to leave one behind even
	// though §11.2 describes this behaviour only as delayed completion.
	if !yield(actsArtifact(execCtx, "long-running", a2a.NewTextPart("long running result")), nil) {
		return
	}
	yield(actsStatus(execCtx, a2a.TaskStateCompleted, "long running work finished"), nil)
}

func actsArtifactParts(behavior string) []*a2a.Part {
	switch behavior {
	case "tck-artifact-text":
		return []*a2a.Part{a2a.NewTextPart("generated text content")}

	case "tck-artifact-data":
		return []*a2a.Part{a2a.NewDataPart(map[string]any{"key": "value", "count": 1})}

	case "tck-artifact-file":
		part := a2a.NewRawPart([]byte("file bytes"))
		part.Filename = "document.txt"
		part.MediaType = "text/plain"
		return []*a2a.Part{part}

	case "tck-artifact-file-url":
		part := a2a.NewFileURLPart(a2a.URL("https://example.com/document.txt"), "text/plain")
		part.Filename = "document.txt"
		return []*a2a.Part{part}
	}
	return nil
}

// actsStream emits working -> artifact(s) -> completed as separate events, so
// each becomes its own SSE frame; a single combined update would satisfy
// min_count only by accident.
func actsStream(execCtx *a2asrv.ExecutorContext, behavior string, yield func(a2a.Event, error) bool) {
	if !yield(actsStatus(execCtx, a2a.TaskStateWorking, "streaming started"), nil) {
		return
	}

	if behavior == "tck-stream-chunked" {
		chunks := []string{"chunk one ", "chunk two ", "chunk three"}

		// The first chunk must go out with Append=false; an update naming an
		// artifact the task has not seen fails the whole task.
		first := a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(chunks[0]))
		first.Artifact.Name = "chunked"
		if !yield(first, nil) {
			return
		}
		for i, chunk := range chunks[1:] {
			evt := a2a.NewArtifactUpdateEvent(execCtx, first.Artifact.ID, a2a.NewTextPart(chunk))
			evt.LastChunk = i == len(chunks)-2
			if !yield(evt, nil) {
				return
			}
		}
	} else if !yield(actsArtifact(execCtx, "streamed", a2a.NewTextPart("streamed content")), nil) {
		return
	}

	yield(actsStatus(execCtx, a2a.TaskStateCompleted, behavior+" ok"), nil)
}
