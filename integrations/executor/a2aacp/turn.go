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
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
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

	mu         sync.Mutex
	pending    *pendingQuestion
	canceled   bool
	ended      bool
	inactivity *time.Timer
	timeout    time.Duration // of inactivity
	inactive   bool          // the inactivity timeout expired
	// paused is the request which paused the task for an answer while no
	// request consumes the turn's events, so that the end of the turn is
	// reported to Config.Abandoned.
	paused  *a2asrv.ExecutorContext
	expired bool
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
	// failure fails the task whatever the agent responded, because the turn
	// was canceled for it.
	failure error
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

// setPending records p as waiting for the caller of execCtx and calls expire
// if nobody answers within timeout. It returns false if the turn was canceled
// or ended.
func (t *turn) setPending(execCtx *a2asrv.ExecutorContext, p *pendingQuestion, timeout time.Duration, expire func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.canceled || t.ended {
		return false
	}
	t.pending = p
	t.paused = execCtx
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

// resume is called when a request continues or cancels a paused turn, so
// that the request reports how the turn ends.
func (t *turn) resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paused = nil
}

// expire marks p, if it is still pending, as unanswered in time.
func (t *turn) expire(p *pendingQuestion) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != p {
		return false
	}
	t.clearPendingLocked()
	t.expired = true
	return true
}

// end is called when the agent responded to the prompt: questions of the
// turn can no longer be answered. If the turn was paused, it returns the
// request which paused it, since no request reports how the turn ended, and
// whether the question expired.
func (t *turn) end() (paused *a2asrv.ExecutorContext, expired bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = true
	t.clearPendingLocked()
	if t.inactivity != nil {
		t.inactivity.Stop()
	}
	return t.paused, t.expired
}

// watchInactivity calls cancel once the agent sends nothing for the turn
// for timeout, except while a question waits for an answer.
func (t *turn) watchInactivity(timeout time.Duration, cancel func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.timeout = timeout
	t.inactivity = time.AfterFunc(timeout, func() {
		t.mu.Lock()
		expired := !t.canceled && !t.ended && t.pending == nil
		t.inactive = t.inactive || expired
		t.mu.Unlock()
		if expired {
			cancel()
		}
	})
}

// touch restarts the inactivity timeout, also after it expired while a
// question was pending.
func (t *turn) touch() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inactivity == nil || t.canceled || t.ended {
		return
	}
	t.inactivity.Reset(t.timeout)
}

// failure returns the error which fails the task whatever the agent
// responds to the prompt, if any.
func (t *turn) failure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inactive {
		return fmt.Errorf("%w for %s", ErrAgentInactive, t.timeout)
	}
	return nil
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

// outcome returns the state the task ends in, or the error which fails it.
func (r *promptResult) outcome() (a2a.TaskState, error) {
	switch {
	case r.failure != nil:
		return "", r.failure
	case r.err != nil && r.canceled:
		return a2a.TaskStateCanceled, nil
	case r.err != nil:
		return "", fmt.Errorf("acp agent error: %w", r.err)
	}
	switch r.reason {
	case acp.StopEndTurn:
		return a2a.TaskStateCompleted, nil
	case acp.StopCancelled:
		return a2a.TaskStateCanceled, nil
	case acp.StopMaxTokens:
		return "", errors.New("the agent reached its token limit")
	case acp.StopMaxTurnRequests:
		return "", errors.New("the agent reached its request limit for the turn")
	case acp.StopRefusal:
		return "", errors.New("the agent refused to continue")
	default:
		return "", fmt.Errorf("the agent stopped: %s", r.reason)
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

// rawBlock converts a file to an image, audio or embedded resource. Agents
// such as opencode keep embedded resources in the session and send them to the
// model with every later message, so a type the model rejects would fail the
// rest of the conversation: only text and PDF files are embedded, and an
// untyped file only if it is text.
func rawBlock(i int, p *a2a.Part, data a2a.Raw, caps acp.PromptCapabilities) (acp.ContentBlock, error) {
	mimeType := p.MediaType
	if mimeType == "" || mediaType(mimeType) == "application/octet-stream" {
		mimeType = "application/octet-stream"
		if isText(data) {
			mimeType = "text/plain"
		}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	base := mediaType(mimeType)
	refuse := fmt.Errorf("the agent does not accept %s attachments", mimeType)
	switch {
	case strings.HasPrefix(base, "image/") && caps.Image:
		return acp.ContentBlock{Type: "image", Data: encoded, MimeType: mimeType}, nil
	case strings.HasPrefix(base, "audio/") && caps.Audio:
		return acp.ContentBlock{Type: "audio", Data: encoded, MimeType: mimeType}, nil
	case !caps.EmbeddedContext:
		return acp.ContentBlock{}, refuse
	}
	name := p.Filename
	if name == "" {
		name = fmt.Sprintf("part-%d", i)
	}
	res := &acp.EmbeddedResource{URI: "attachment:" + name, MimeType: mimeType}
	switch {
	case isTextType(base):
		if !utf8.Valid(data) {
			return acp.ContentBlock{}, fmt.Errorf("the agent does not accept %s attachments which are not UTF-8", mimeType)
		}
		res.Text = string(data)
	case base == "application/pdf":
		res.Blob = encoded
	default:
		return acp.ContentBlock{}, refuse
	}
	return acp.ContentBlock{Type: "resource", Resource: res}, nil
}

// mediaType returns the media type without parameters, in lower case.
func mediaType(mimeType string) string {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(mimeType))
	}
	return base
}

var textTypes = []string{
	"application/json", "application/xml", "application/yaml", "application/x-yaml", "application/toml",
	"application/javascript", "application/x-javascript", "application/ecmascript", "application/typescript",
	"application/x-sh", "application/x-shellscript", "application/sql", "application/graphql",
	"application/x-ndjson", "application/jsonl", "application/csv", "application/x-httpd-php",
}

func isTextType(base string) bool {
	if strings.HasPrefix(base, "text/") || slices.Contains(textTypes, base) {
		return true
	}
	for _, suffix := range []string{"+json", "+xml", "+yaml"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// isText reports whether untyped data is UTF-8 text.
func isText(data []byte) bool {
	return utf8.Valid(data) && strings.HasPrefix(http.DetectContentType(data), "text/plain")
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
