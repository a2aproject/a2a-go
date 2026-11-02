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

package a2aacp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

// turn is one ACP prompt. It outlives an Execute call when the task pauses in
// input-required, and ends when the agent responds to the prompt.
type turn struct {
	taskID    a2a.TaskID
	sessionID string
	agent     *agent
	events    *queue[turnEvent]

	// Accessed only by the goroutine running Execute.
	tools map[string]*acp.ToolCall
	out   *taskOutput

	mu       sync.Mutex
	pending  *pendingQuestion
	canceled bool
	ended    bool
}

type turnEvent struct {
	update   *acp.SessionUpdate
	question *incomingQuestion
	result   *promptResult
}

type promptResult struct {
	reason acp.StopReason
	err    error
	// canceled reports that the turn was canceled, so that an agent stopped
	// for not ending a canceled turn doesn't fail the task.
	canceled bool
}

func newTurn(taskID a2a.TaskID, sessionID string, ag *agent) *turn {
	return &turn{
		taskID:    taskID,
		sessionID: sessionID,
		agent:     ag,
		events:    newQueue[turnEvent](),
		tools:     map[string]*acp.ToolCall{},
		out:       newTaskOutput(),
	}
}

func (t *turn) pendingQuestion() *pendingQuestion {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending
}

// setPending records p as waiting for the caller and calls expire if nobody
// answers within timeout. It returns false if the turn was canceled or ended.
func (t *turn) setPending(p *pendingQuestion, timeout time.Duration, expire func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.canceled || t.ended {
		return false
	}
	t.pending = p
	if timeout > 0 {
		p.timer = time.AfterFunc(timeout, expire)
	}
	return true
}

// takePending clears p if it is still pending, so that it is answered once.
func (t *turn) takePending(p *pendingQuestion) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != p {
		return false
	}
	t.clearPendingLocked()
	return true
}

func (t *turn) clearPendingLocked() *pendingQuestion {
	p := t.pending
	if p != nil && p.timer != nil {
		p.timer.Stop()
	}
	t.pending = nil
	return p
}

// cancel marks the turn canceled and returns the pending question. It returns
// false if the turn was already canceled.
func (t *turn) cancel() (*pendingQuestion, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.canceled {
		return nil, false
	}
	t.canceled = true
	return t.clearPendingLocked(), true
}

// end is called when the agent responded to the prompt: questions of the
// turn can no longer be answered.
func (t *turn) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = true
	t.clearPendingLocked()
}

func (t *turn) hasEnded() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ended
}

func (t *turn) isCanceled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.canceled
}

func (r *promptResult) event(info a2a.TaskInfoProvider) a2a.Event {
	if r.err != nil && r.canceled {
		return a2a.NewStatusUpdateEvent(info, a2a.TaskStateCanceled, nil)
	}
	if r.err != nil {
		return failed(info, fmt.Sprintf("acp agent error: %v", r.err))
	}
	switch r.reason {
	case acp.StopEndTurn:
		return a2a.NewStatusUpdateEvent(info, a2a.TaskStateCompleted, nil)
	case acp.StopCancelled:
		return a2a.NewStatusUpdateEvent(info, a2a.TaskStateCanceled, nil)
	case acp.StopMaxTokens:
		return failed(info, "the agent reached its token limit")
	case acp.StopMaxTurnRequests:
		return failed(info, "the agent reached its request limit for the turn")
	case acp.StopRefusal:
		return failed(info, "the agent refused to continue")
	default:
		return failed(info, fmt.Sprintf("the agent stopped: %s", r.reason))
	}
}

// update returns the Update passed to the converter, after merging the tool
// call state a tool call update carries.
func (t *turn) update(su *acp.SessionUpdate) Update {
	u := Update{Type: su.SessionUpdate, Raw: su.Raw, update: su}
	if su.SessionUpdate != acp.UpdateToolCall && su.SessionUpdate != acp.UpdateToolCallUpdate {
		return u
	}
	tc := t.mergeTool(su.ToolCall)
	if su.Status != "" && su.Status != tc.Status {
		tc.Status = su.Status
		snapshot := *tc
		u.tool = &snapshot
	}
	return u
}

// mergeTool applies the fields set in patch to the known state of the tool
// call, except the status. Agents send partial tool calls in updates and in
// permission requests, for example an empty input already reported earlier.
func (t *turn) mergeTool(patch acp.ToolCall) *acp.ToolCall {
	tc := t.tools[patch.ToolCallID]
	if tc == nil {
		tc = &acp.ToolCall{ToolCallID: patch.ToolCallID}
		t.tools[patch.ToolCallID] = tc
	}
	if patch.Title != "" {
		tc.Title = patch.Title
	}
	if patch.Kind != "" {
		tc.Kind = patch.Kind
	}
	if m, ok := patch.RawInput.(map[string]any); patch.RawInput != nil && (!ok || len(m) > 0) {
		tc.RawInput = patch.RawInput
	}
	return tc
}

// toPrompt converts message parts to prompt content.
func toPrompt(ctx context.Context, parts []*a2a.Part, caps acp.PromptCapabilities) ([]acp.ContentBlock, error) {
	var out []acp.ContentBlock
	for i, p := range parts {
		switch c := p.Content.(type) {
		case a2a.Text:
			out = append(out, acp.ContentBlock{Type: "text", Text: string(c)})
		case a2a.Data:
			b, err := json.Marshal(c.Value)
			if err != nil {
				return nil, fmt.Errorf("encoding data part: %w", err)
			}
			out = append(out, acp.ContentBlock{Type: "text", Text: string(b)})
		case a2a.URL:
			name := p.Filename
			if name == "" {
				name = string(c)
			}
			out = append(out, acp.ContentBlock{Type: "resource_link", URI: string(c), Name: name, MimeType: p.MediaType})
		case a2a.Raw:
			block, err := rawBlock(i, p, c, caps)
			if err != nil {
				return nil, err
			}
			out = append(out, block)
		default:
			log.Warn(ctx, "dropped prompt part the agent can't receive", "index", i, "type", fmt.Sprintf("%T", c))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the message has no content the agent accepts")
	}
	return out, nil
}

func rawBlock(i int, p *a2a.Part, data a2a.Raw, caps acp.PromptCapabilities) (acp.ContentBlock, error) {
	mime := p.MediaType
	if mime == "" {
		mime = "application/octet-stream"
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	switch {
	case strings.HasPrefix(mime, "image/") && caps.Image:
		return acp.ContentBlock{Type: "image", Data: encoded, MimeType: mime}, nil
	case strings.HasPrefix(mime, "audio/") && caps.Audio:
		return acp.ContentBlock{Type: "audio", Data: encoded, MimeType: mime}, nil
	case caps.EmbeddedContext:
		name := p.Filename
		if name == "" {
			name = fmt.Sprintf("part-%d", i)
		}
		res := &acp.EmbeddedResource{URI: "attachment:" + name, MimeType: mime}
		if strings.HasPrefix(mime, "text/") {
			res.Text = string(data)
		} else {
			res.Blob = encoded
		}
		return acp.ContentBlock{Type: "resource", Resource: res}, nil
	}
	return acp.ContentBlock{}, fmt.Errorf("the agent does not accept %s attachments", mime)
}

// queue is an unbounded FIFO, so that the connection's read goroutine never
// blocks on a turn nobody is consuming, for example while it waits for input.
type queue[T any] struct {
	mu     sync.Mutex
	items  []T
	signal chan struct{}
}

func newQueue[T any]() *queue[T] {
	return &queue[T]{signal: make(chan struct{}, 1)}
}

func (q *queue[T]) push(v T) {
	q.mu.Lock()
	q.items = append(q.items, v)
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *queue[T]) pop(ctx context.Context) (T, error) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			v := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return v, nil
		}
		q.mu.Unlock()
		select {
		case <-q.signal:
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

func (q *queue[T]) removeIf(pred func(T) bool) []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	var removed, kept []T
	for _, v := range q.items {
		if pred(v) {
			removed = append(removed, v)
		} else {
			kept = append(kept, v)
		}
	}
	q.items = kept
	return removed
}
