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
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Config configures an [Executor]. The zero value is valid. Durations of zero
// mean the default, negative durations disable the limit.
type Config struct {
	// Pool runs agent processes per key instead of one process for all
	// contexts. Without it, [NewExecutor] starts the agent, so that a broken
	// configuration fails early; pooled agents start on their first task.
	Pool *PoolConfig
	// StartTimeout limits starting and initializing an agent. Defaults to one minute.
	StartTimeout time.Duration
	// CancelTimeout is how long the agent may keep working on a canceled task
	// before it is stopped, which fails all its tasks. Meanwhile the context
	// accepts no new tasks. Defaults to one minute.
	CancelTimeout time.Duration

	// AutoAnswer answers questions without asking the caller, for example
	// [ToolRules]. If nil, every question is asked.
	AutoAnswer AutoAnswerer
	// QuestionTimeout is how long a question waits for the caller before it is
	// rejected and the agent stops working on the task. Defaults to 24 hours.
	QuestionTimeout time.Duration

	// Session returns the parameters of the ACP session of an A2A context. It
	// is called when the agent creates the session or, after a restart, loads
	// it, for the message which caused it, so parameters which depend on the
	// caller apply to everyone in the context. If nil, the default parameters
	// are used.
	Session func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (SessionParams, error)
	// SessionStore maps A2A contexts to ACP sessions. Defaults to an in-memory store.
	SessionStore SessionStore
	// Settings returns the session settings to apply before the agent works on
	// a message, for example {"model": "anthropic/claude-sonnet-4"}. Keys are
	// the config IDs the agent reports in configOptions; "mode" and "model"
	// also work with agents which only support session/set_mode and
	// session/set_model. Settings stay in effect for later messages of the
	// session. See [MetadataSettings].
	Settings func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error)
	// Prompt returns the parts sent to the agent for a message, for example to
	// add context the caller didn't write. The task keeps the original
	// message. Defaults to the message parts.
	Prompt func(ctx context.Context, execCtx *a2asrv.ExecutorContext) ([]*a2a.Part, error)
	// RejectBusyContext fails a new task in a context which has a task in
	// progress, including one waiting for an answer, instead of queueing it.
	RejectBusyContext bool

	// Converter decides how the agent's session updates appear in the task.
	// Defaults to [ConvertUpdate].
	Converter Converter
}

// PoolConfig configures agent processes per key. See [Config.Pool].
type PoolConfig struct {
	// Key selects the agent process of a new A2A context: contexts with the
	// same key share a process, contexts with different keys never do. For
	// example, return a tenant from the call context in ctx for a process per
	// tenant. It is called when a context has no session yet; later tasks use
	// the agent whose key is stored with the session, since a session lives in
	// one agent's storage. Defaults to a process per context.
	Key func(ctx context.Context, contextID string) (string, error)
	// IdleTimeout stops an agent which has no tasks in progress, including
	// tasks waiting for an answer, for this long. Its sessions are loaded
	// again by the next agent of the key if the agent supports session/load,
	// and lost otherwise. Defaults to 10 minutes.
	IdleTimeout time.Duration
	// MaxAgents limits the number of running agents. At the limit, the least
	// recently used idle agent is stopped to start another; if all are busy,
	// the task fails. Zero means no limit.
	MaxAgents int
}

// SessionParams are the parameters of an ACP session, fixed for its lifetime
// in an agent process.
type SessionParams struct {
	// Cwd is the working directory of the session. A relative path is
	// resolved against the default: [Process.Dir] for a Process, the current
	// directory otherwise. Set it when the agent runs elsewhere, for example
	// in a sandbox.
	Cwd string
	// MCPServers are the MCP servers the agent connects to, in the JSON form
	// of the ACP McpServer schema, for example
	// {"name": "fs", "command": "mcp-fs", "args": [], "env": []} or
	// {"type": "http", "name": "api", "url": "https://...", "headers": []}.
	MCPServers []json.RawMessage
}

// Executor implements [a2asrv.AgentExecutor] for an ACP agent.
type Executor struct {
	cfg          Config
	agentStarter Agent
	dir          string // default session working directory
	converter    Converter
	validator    structuredinput.Validator
	processor    *structuredinput.Processor
	agentKey     func(ctx context.Context, contextID string) (string, error)

	startTimeout    time.Duration
	cancelTimeout   time.Duration
	questionTimeout time.Duration
	idleTimeout     time.Duration
	maxAgents       int

	elicitations atomic.Int64

	mu       sync.Mutex
	agents   map[string]*agentSlot // agent key -> agent
	closed   bool
	draining bool
	drained  chan struct{}                          // closed when draining and no turn runs or waits
	routes   map[routeKey]*turn                     // agent session -> turn in progress
	turns    map[a2a.TaskID]*turn                   // task ID -> turn in progress
	waiting  map[a2a.TaskID]context.CancelCauseFunc // task ID -> turn being started
	locks    map[string]*ctxLock                    // A2A context ID -> turn lock
}

// routeKey identifies a session in an agent process: processes of different
// keys may use the same session IDs.
type routeKey struct {
	agent     *agent
	sessionID string
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

const (
	defaultCancelTimeout   = time.Minute
	defaultQuestionTimeout = 24 * time.Hour
	defaultIdleTimeout     = 10 * time.Minute
)

var errClosed = errors.New("acp executor is closed")

// errQuestionGone is reported to a caller who answers a question the executor no longer tracks.
const errQuestionGone = "the question is no longer pending: it timed out or the agent restarted. Send a new message to continue"

// errTaskCanceled interrupts starting a turn whose task was canceled.
var errTaskCanceled = errors.New("the task was canceled")

// ctxLock serializes turns of one A2A context. refs counts holders and
// waiters, so that the lock can be dropped when nobody uses it.
type ctxLock struct {
	ch     chan struct{}
	refs   int
	holder a2a.TaskID // guarded by Executor.mu
}

type busyError struct{ holder a2a.TaskID }

func (e *busyError) Error() string {
	if e.holder == "" {
		return "the context is busy with another task, send the message after it finishes"
	}
	return fmt.Sprintf("the context is busy with task %s, send the message after it finishes", e.holder)
}

// NewExecutor returns an executor of the agent which agent starts. cfg may be
// nil. Without [Config.Pool] it starts the agent and negotiates the protocol
// version. Call [Executor.Shutdown] or [Executor.Close] to stop the agents.
func NewExecutor(ctx context.Context, agent Agent, cfg *Config) (*Executor, error) {
	if agent == nil {
		return nil, errors.New("a2aacp: agent is required")
	}
	var c Config
	if cfg != nil {
		c = *cfg
	}
	dir, err := defaultDir(agent)
	if err != nil {
		return nil, err
	}
	if c.SessionStore == nil {
		c.SessionStore = newMemorySessions()
	}
	e := &Executor{
		cfg:             c,
		agentStarter:    agent,
		dir:             dir,
		converter:       c.Converter,
		startTimeout:    duration(c.StartTimeout, defaultStartTimeout),
		cancelTimeout:   duration(c.CancelTimeout, defaultCancelTimeout),
		questionTimeout: duration(c.QuestionTimeout, defaultQuestionTimeout),
		agents:          map[string]*agentSlot{},
		drained:         make(chan struct{}),
		routes:          map[routeKey]*turn{},
		turns:           map[a2a.TaskID]*turn{},
		waiting:         map[a2a.TaskID]context.CancelCauseFunc{},
		locks:           map[string]*ctxLock{},
	}
	if e.converter == nil {
		e.converter = ConverterFunc(ConvertUpdate)
	}
	e.validator = structuredinput.NewValidator(structuredinput.DefaultDraft)
	e.processor = &structuredinput.Processor{Validator: e.validator}
	if c.Pool == nil {
		e.agentKey = func(context.Context, string) (string, error) { return "", nil }
		_, release, err := e.acquireAgent(ctx, "")
		if err != nil {
			return nil, err
		}
		release()
		return e, nil
	}
	e.agentKey = c.Pool.Key
	if e.agentKey == nil {
		e.agentKey = func(_ context.Context, contextID string) (string, error) { return contextID, nil }
	}
	e.idleTimeout = duration(c.Pool.IdleTimeout, defaultIdleTimeout)
	e.maxAgents = c.Pool.MaxAgents
	return e, nil
}

// duration applies the Config convention: zero means def, negative disables.
func duration(d, def time.Duration) time.Duration {
	switch {
	case d == 0:
		return def
	case d < 0:
		return 0
	}
	return d
}

// defaultDir returns the absolute default working directory of sessions.
func defaultDir(agent Agent) (string, error) {
	var dir string
	switch p := agent.(type) {
	case Process:
		dir = p.Dir
	case *Process:
		dir = p.Dir
	}
	if dir == "" {
		return os.Getwd()
	}
	return filepath.Abs(dir)
}

// agentHandler routes what an agent process sends to the turn of its session.
// It runs on the read goroutine of the connection and must not block.
type agentHandler struct {
	e  *Executor
	ag *agent
}

var _ acp.Handler = (*agentHandler)(nil)

func (h *agentHandler) SessionUpdate(n *acp.SessionNotification) {
	if n.Update.SessionUpdate == acp.UpdateConfigOption {
		h.ag.setOptions(n.SessionID, n.Update.ConfigOptions)
	}
	if t := h.e.route(h.ag, n.SessionID); t != nil {
		t.events.push(turnEvent{update: &n.Update})
	}
}

func (h *agentHandler) RequestPermission(id json.RawMessage, req *acp.RequestPermissionRequest) {
	h.ask(req.SessionID, &incomingQuestion{rpcID: id, permission: req})
}

func (h *agentHandler) CreateElicitation(id json.RawMessage, req *acp.CreateElicitationRequest) {
	if req.Mode != "form" {
		rpcErr := &acp.Error{Code: acp.CodeInvalidParams, Message: "only form elicitation is supported"}
		if err := h.ag.client.RespondError(id, rpcErr); err != nil {
			log.Warn(context.Background(), "failed to respond to acp agent", "cause", err)
		}
		return
	}
	h.ask(req.SessionID, &incomingQuestion{rpcID: id, elicitation: req})
}

func (h *agentHandler) ask(sessionID string, in *incomingQuestion) {
	t := h.e.route(h.ag, sessionID)
	if t == nil || t.isCanceled() {
		in.cancel(h.ag)
		return
	}
	t.events.push(turnEvent{question: in})
}

func (e *Executor) route(ag *agent, sessionID string) *turn {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.routes[routeKey{ag, sessionID}]
}

// Execute implements [a2asrv.AgentExecutor].
func (e *Executor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		e.mu.Lock()
		t := e.turns[execCtx.TaskID]
		e.mu.Unlock()

		switch p := t.pendingQuestion(); {
		case p != nil:
			var ok bool
			if t, ok = e.answer(ctx, execCtx, t, p, yield); !ok {
				return
			}
		case execCtx.StoredTask != nil && execCtx.StoredTask.Status.State == a2a.TaskStateInputRequired:
			yield(failed(execCtx, errQuestionGone), nil)
			return
		default:
			var err error
			if t, err = e.startTurn(ctx, execCtx, !e.cfg.RejectBusyContext); err != nil {
				if !errors.Is(err, errTaskCanceled) {
					yield(failed(execCtx, err.Error()), nil)
				}
				return
			}
		}

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			e.cancelTurn(t)
			return
		}
		e.pump(ctx, execCtx, t, yield)
	}
}

// Cancel implements [a2asrv.AgentExecutor].
func (e *Executor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		e.mu.Lock()
		t := e.turns[execCtx.TaskID]
		stopWaiting := e.waiting[execCtx.TaskID]
		e.mu.Unlock()
		if stopWaiting != nil {
			stopWaiting(errTaskCanceled)
		}
		if t != nil {
			e.cancelTurn(t)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// answer forwards the caller's reply to the pending question and returns the
// turn to resume. It returns false if Execute should stop, for example because
// the reply didn't answer the question and the task stays in input-required.
func (e *Executor) answer(ctx context.Context, execCtx *a2asrv.ExecutorContext, t *turn, p *pendingQuestion, yield func(a2a.Event, error) bool) (*turn, bool) {
	ans, retry, err := e.parseReply(ctx, execCtx, p)
	switch {
	case err != nil:
		yield(failed(execCtx, fmt.Sprintf("processing the reply: %v", err)), nil)
		return nil, false
	case retry != nil:
		yield(retry, nil)
		return nil, false
	case ans == nil:
		return e.interrupt(ctx, execCtx, t, p, yield)
	}
	if t.agent.exited() || !t.takePending(p) {
		yield(failed(execCtx, errQuestionGone), nil)
		return nil, false
	}
	if err := p.reply(t.agent, *ans); err != nil {
		e.cancelTurn(t)
		msg := fmt.Sprintf("forwarding answer to acp agent: %v", err)
		if t.agent.exited() {
			msg = errQuestionGone
		}
		yield(failed(execCtx, msg), nil)
		return nil, false
	}
	return t, true
}

// parseReply returns the answer in the caller's message, nil if the message
// isn't an answer at all, or the event which asks again if the answer can't be
// used. Typed replies are read for clients which can't send structured input.
func (e *Executor) parseReply(ctx context.Context, execCtx *a2asrv.ExecutorContext, p *pendingQuestion) (*Answer, *a2a.TaskStatusUpdateEvent, error) {
	res, err := e.processor.Process(ctx, p.q.Request, execCtx.Message)
	if err != nil {
		return nil, nil, err
	}
	askAgain := func(res *structuredinput.ProcessorResult, reason string) (*Answer, *a2a.TaskStatusUpdateEvent, error) {
		evt, err := res.NewRetryEvent(execCtx, p.parts(fmt.Sprintf("I couldn't use the reply: %s. ", reason))...)
		return nil, evt, err
	}
	switch res.Outcome {
	case structuredinput.OutcomeAccepted:
		var ans Answer
		if err := res.Decode(&ans); err != nil {
			return nil, nil, err
		}
		return &ans, nil, nil
	case structuredinput.OutcomeInvalid:
		return askAgain(res, describe(res.Errors))
	case structuredinput.OutcomeIDMismatch:
		return askAgain(res, "it answers a different question")
	case structuredinput.OutcomeMultipleInputs:
		return askAgain(res, "it has more than one answer")
	}
	if res.Errors != nil {
		return askAgain(res, describe(res.Errors))
	}
	ans, ok, err := p.matchText(messageText(execCtx.Message))
	if err != nil {
		return askAgain(res, err.Error())
	}
	if !ok {
		return nil, nil, nil
	}
	out, err := e.validate(ctx, p, ans)
	if err != nil {
		return nil, nil, err
	}
	if out != nil {
		res.Errors = out
		return askAgain(res, describe(out))
	}
	return &ans, nil, nil
}

// validate returns the validation errors of ans, or nil if it satisfies the request schema.
func (e *Executor) validate(ctx context.Context, p *pendingQuestion, ans Answer) (*structuredinput.ValidationOutput, error) {
	var value any
	if err := convertJSON(ans, &value); err != nil {
		return nil, err
	}
	err := e.validator.Validate(ctx, p.q.Request.Schema, value)
	var verr *structuredinput.ValidationError
	if !errors.As(err, &verr) {
		return nil, err
	}
	if verr.Output == nil {
		return &structuredinput.ValidationOutput{}, nil
	}
	return verr.Output, nil
}

func describe(out *structuredinput.ValidationOutput) string {
	var msgs []string
	for _, item := range out.Errors {
		msgs = append(msgs, strings.TrimPrefix(item.Error, "validating root: "))
	}
	if len(msgs) == 0 {
		return "it doesn't match the requested schema"
	}
	return strings.Join(msgs, "; ")
}

func messageText(msg *a2a.Message) string {
	var texts []string
	for _, part := range msg.Parts {
		if text := strings.TrimSpace(part.Text()); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

// interrupt rejects the pending question, cancels its turn and starts a new
// turn with the caller's message once the agent ended the canceled one.
func (e *Executor) interrupt(ctx context.Context, execCtx *a2asrv.ExecutorContext, t *turn, p *pendingQuestion, yield func(a2a.Event, error) bool) (*turn, bool) {
	if t.takePending(p) {
		if err := p.reject(t.agent); err != nil {
			log.Warn(ctx, "failed to reject acp request", "cause", err)
		}
	}
	e.cancelTurn(t)
	next, err := e.startTurn(ctx, execCtx, true)
	if err != nil {
		if !errors.Is(err, errTaskCanceled) {
			yield(failed(execCtx, err.Error()), nil)
		}
		return nil, false
	}
	next.out = t.out
	return next, true
}

// startTurn waits for the previous turn in the context to finish, or fails
// with [busyError] unless wait is set, and sends the message as a new prompt.
// Until the turn is registered, [Executor.Cancel] stops it with errTaskCanceled.
func (e *Executor) startTurn(ctx context.Context, execCtx *a2asrv.ExecutorContext, wait bool) (*turn, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	e.mu.Lock()
	if e.draining {
		e.mu.Unlock()
		return nil, errDraining
	}
	e.waiting[execCtx.TaskID] = cancel
	e.mu.Unlock()
	defer e.stopWaiting(execCtx.TaskID)

	t, err := e.lockAndPrompt(ctx, execCtx, wait)
	if err != nil && errors.Is(context.Cause(ctx), errTaskCanceled) {
		return nil, errTaskCanceled
	}
	return t, err
}

func (e *Executor) lockAndPrompt(ctx context.Context, execCtx *a2asrv.ExecutorContext, wait bool) (*turn, error) {
	unlock, err := e.lockContext(ctx, execCtx.ContextID, execCtx.TaskID, wait)
	if err != nil {
		return nil, err
	}
	t, err := e.prompt(ctx, execCtx, unlock)
	if err != nil {
		unlock()
		return nil, err
	}
	return t, nil
}

func (e *Executor) stopWaiting(taskID a2a.TaskID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.waiting, taskID)
	e.checkDrainedLocked()
}

func (e *Executor) isWaiting(taskID a2a.TaskID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.waiting[taskID] != nil
}

// prompt sends the message as a prompt and returns its turn. done is called
// when the agent responds to the prompt.
func (e *Executor) prompt(ctx context.Context, execCtx *a2asrv.ExecutorContext, done func()) (*turn, error) {
	parts, err := e.promptParts(ctx, execCtx)
	if err != nil {
		return nil, err
	}
	settings, err := e.settings(ctx, execCtx)
	if err != nil {
		return nil, err
	}
	stored, hasSession, err := e.cfg.SessionStore.Get(ctx, execCtx.ContextID)
	if err != nil {
		return nil, err
	}
	key := stored.AgentKey
	if !hasSession {
		if key, err = e.agentKey(ctx, execCtx.ContextID); err != nil {
			return nil, fmt.Errorf("selecting the agent: %w", err)
		}
	}
	ag, release, err := e.acquireAgent(ctx, key)
	if err != nil {
		return nil, err
	}
	t, err := e.promptAgent(ctx, execCtx, ag, parts, settings, stored, hasSession, func() {
		release()
		done()
	})
	if err != nil {
		release()
		return nil, err
	}
	return t, nil
}

func (e *Executor) promptAgent(ctx context.Context, execCtx *a2asrv.ExecutorContext, ag *agent, parts []*a2a.Part, settings map[string]any, stored Session, hasSession bool, done func()) (*turn, error) {
	prompt, err := toPrompt(ctx, parts, ag.caps.PromptCapabilities)
	if err != nil {
		return nil, err
	}
	sessionID, err := e.session(ctx, ag, execCtx, stored, hasSession)
	if err != nil {
		return nil, err
	}
	if err := configure(ctx, ag, sessionID, settings); err != nil {
		return nil, fmt.Errorf("configuring acp session: %w", err)
	}

	t := newTurn(execCtx.TaskID, sessionID, ag)
	route := routeKey{ag, sessionID}
	e.mu.Lock()
	if err := context.Cause(ctx); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	delete(e.waiting, execCtx.TaskID)
	e.routes[route] = t
	e.turns[execCtx.TaskID] = t
	e.mu.Unlock()

	go func() {
		resp, err := ag.client.Prompt(context.WithoutCancel(ctx), acp.PromptRequest{SessionID: sessionID, Prompt: prompt})
		result := &promptResult{err: err, canceled: t.isCanceled()}
		if err == nil {
			result.reason = resp.StopReason
		}
		t.end()
		t.events.push(turnEvent{result: result})
		e.mu.Lock()
		if e.routes[route] == t {
			delete(e.routes, route)
		}
		if e.turns[t.taskID] == t {
			delete(e.turns, t.taskID)
		}
		e.checkDrainedLocked()
		e.mu.Unlock()
		done()
	}()
	return t, nil
}

func (e *Executor) promptParts(ctx context.Context, execCtx *a2asrv.ExecutorContext) ([]*a2a.Part, error) {
	if e.cfg.Prompt != nil {
		return e.cfg.Prompt(ctx, execCtx)
	}
	if execCtx.Message == nil {
		return nil, errors.New("no message to send to the agent")
	}
	return execCtx.Message.Parts, nil
}

func (e *Executor) settings(ctx context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error) {
	if e.cfg.Settings == nil {
		return nil, nil
	}
	return e.cfg.Settings(ctx, execCtx)
}

// session returns the ACP session of the A2A context. It reuses a session the
// agent already knows, loads a session created by a previous agent process,
// or creates a new one.
func (e *Executor) session(ctx context.Context, ag *agent, execCtx *a2asrv.ExecutorContext, stored Session, ok bool) (string, error) {
	id := stored.ID
	if ok && ag.hasSession(id) {
		return id, nil
	}
	params, err := e.sessionParams(ctx, execCtx)
	if err != nil {
		return "", err
	}
	if ok && ag.caps.LoadSession {
		resp, err := ag.client.LoadSession(ctx, acp.LoadSessionRequest{SessionID: id, Cwd: params.Cwd, MCPServers: params.MCPServers})
		if err == nil {
			ag.addSession(id, *resp)
			return id, nil
		}
		log.Warn(ctx, "failed to load acp session, starting a new one", "session", id, "cause", err)
	}

	resp, err := ag.client.NewSession(ctx, acp.NewSessionRequest{Cwd: params.Cwd, MCPServers: params.MCPServers})
	if err != nil {
		return "", fmt.Errorf("creating acp session: %w", err)
	}
	ag.addSession(resp.SessionID, resp.SessionState)
	if err := e.cfg.SessionStore.Put(ctx, execCtx.ContextID, Session{ID: resp.SessionID, AgentKey: ag.key}); err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// sessionParams returns the parameters of a session with defaults applied:
// ACP requires an absolute working directory and a non-null server list.
func (e *Executor) sessionParams(ctx context.Context, execCtx *a2asrv.ExecutorContext) (SessionParams, error) {
	var params SessionParams
	if e.cfg.Session != nil {
		var err error
		if params, err = e.cfg.Session(ctx, execCtx); err != nil {
			return SessionParams{}, fmt.Errorf("resolving session parameters: %w", err)
		}
	}
	if !filepath.IsAbs(params.Cwd) {
		params.Cwd = filepath.Join(e.dir, params.Cwd)
	}
	if params.MCPServers == nil {
		params.MCPServers = []json.RawMessage{}
	}
	return params, nil
}

// configure applies session settings in a stable order.
func configure(ctx context.Context, ag *agent, sessionID string, settings map[string]any) error {
	for _, id := range slices.Sorted(maps.Keys(settings)) {
		value := settings[id]
		method, typ, ok := ag.configMethod(sessionID, id)
		if !ok {
			return fmt.Errorf("the agent has no session setting %q", id)
		}
		var err error
		switch method {
		case acp.MethodSetConfigOption:
			req := acp.SetConfigOptionRequest{SessionID: sessionID, ConfigID: id, Value: value}
			if typ == "boolean" {
				req.Type = typ
			}
			var resp *acp.SetConfigOptionResponse
			if resp, err = ag.client.SetConfigOption(ctx, req); err == nil {
				ag.setOptions(sessionID, resp.ConfigOptions)
			}
		case acp.MethodSetMode, acp.MethodSetModel:
			s, isText := value.(string)
			if !isText {
				return fmt.Errorf("the session setting %q must be a string", id)
			}
			if method == acp.MethodSetMode {
				err = ag.client.SetMode(ctx, acp.SetModeRequest{SessionID: sessionID, ModeID: s})
			} else {
				err = ag.client.SetModel(ctx, acp.SetModelRequest{SessionID: sessionID, ModelID: s})
			}
		}
		if err != nil {
			return fmt.Errorf("setting %q: %w", id, err)
		}
	}
	return nil
}

// lockContext takes the turn lock of the context for the task. If wait is
// false and the lock is held, it fails with [busyError].
func (e *Executor) lockContext(ctx context.Context, contextID string, taskID a2a.TaskID, wait bool) (func(), error) {
	e.mu.Lock()
	l := e.locks[contextID]
	if l == nil {
		l = &ctxLock{ch: make(chan struct{}, 1)}
		e.locks[contextID] = l
	}
	l.refs++
	e.mu.Unlock()

	release := func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		l.refs--
		if l.refs == 0 {
			delete(e.locks, contextID)
		}
	}
	acquired := func() (func(), error) {
		e.mu.Lock()
		l.holder = taskID
		e.mu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				e.mu.Lock()
				l.holder = ""
				e.mu.Unlock()
				<-l.ch
				release()
			})
		}, nil
	}
	if !wait {
		select {
		case l.ch <- struct{}{}:
			return acquired()
		default:
			e.mu.Lock()
			err := &busyError{holder: l.holder}
			e.mu.Unlock()
			release()
			return nil, err
		}
	}
	select {
	case l.ch <- struct{}{}:
		return acquired()
	case <-ctx.Done():
		release()
		return nil, context.Cause(ctx)
	}
}

// pump translates turn events to A2A events until the turn finishes or
// pauses for input.
func (e *Executor) pump(ctx context.Context, execCtx *a2asrv.ExecutorContext, t *turn, yield func(a2a.Event, error) bool) {
	for {
		ev, err := t.events.pop(ctx)
		if err != nil {
			e.cancelTurn(t)
			return
		}

		switch {
		case ev.update != nil:
			res, err := e.converter.Convert(ctx, execCtx, t.update(ev.update))
			if err != nil {
				e.cancelTurn(t)
				yield(failed(execCtx, fmt.Sprintf("converting acp update: %v", err)), nil)
				return
			}
			if out := t.out.emit(execCtx, ev.update.MessageID, res); out != nil && !yield(out, nil) {
				e.cancelTurn(t)
				return
			}

		case ev.question != nil:
			p, err := t.newPending(ev.question, fmt.Sprintf("elicitation-%d", e.elicitations.Add(1)))
			if err != nil {
				log.Warn(ctx, "canceled acp request which can't be asked", "cause", err)
				ev.question.cancel(t.agent)
				continue
			}
			decided, err := e.autoAnswer(ctx, execCtx, t.agent, p)
			if err != nil {
				e.cancelTurn(t)
				yield(failed(execCtx, fmt.Sprintf("forwarding answer to acp agent: %v", err)), nil)
				return
			}
			if decided {
				continue
			}
			if !t.setPending(p, e.questionTimeout, func() { e.expire(t, p) }) {
				p.in.cancel(t.agent)
				continue
			}
			evt, err := structuredinput.NewRequestEvent(execCtx, p.q.Request, p.parts("")...)
			if err != nil {
				e.cancelTurn(t)
				yield(failed(execCtx, fmt.Sprintf("asking the question: %v", err)), nil)
				return
			}
			yield(evt, nil)
			return

		case ev.result != nil:
			yield(ev.result.event(execCtx), nil)
			return
		}
	}
}

// autoAnswer answers p with Config.AutoAnswer. An answer which doesn't
// satisfy the request schema is a configuration bug the caller can't fix, so
// it is logged and the caller is asked instead.
func (e *Executor) autoAnswer(ctx context.Context, execCtx *a2asrv.ExecutorContext, ag *agent, p *pendingQuestion) (bool, error) {
	if e.cfg.AutoAnswer == nil {
		return false, nil
	}
	ans, ok := e.cfg.AutoAnswer.AutoAnswer(ctx, execCtx, p.q)
	if !ok {
		return false, nil
	}
	if ans.Option != OptionCancel || p.offers(OptionCancel) {
		out, err := e.validate(ctx, p, ans)
		if err != nil || out != nil {
			reason := err
			if out != nil {
				reason = errors.New(describe(out))
			}
			log.Warn(ctx, "auto answer doesn't satisfy the request schema, asking the caller", "question", p.q.Request.ID, "cause", reason)
			return false, nil
		}
	}
	return true, p.reply(ag, ans)
}

// expire rejects a question nobody answered in time and cancels its turn,
// since nobody would see what the agent does next.
func (e *Executor) expire(t *turn, p *pendingQuestion) {
	if !t.takePending(p) {
		return
	}
	ctx := context.Background()
	log.Info(ctx, "acp question timed out", "task", t.taskID, "question", p.q.Request.ID)
	if err := p.reject(t.agent); err != nil {
		log.Warn(ctx, "failed to reject acp request", "cause", err)
	}
	e.cancelTurn(t)
}

// cancelTurn asks the agent to stop the turn and cancels its open questions,
// as ACP requires from the client. The turn ends when the agent responds to
// the prompt, or when the agent is stopped after Config.CancelTimeout.
func (e *Executor) cancelTurn(t *turn) {
	p, ok := t.cancel()
	if !ok {
		return
	}
	if err := t.agent.client.Cancel(t.sessionID); err != nil {
		log.Warn(context.Background(), "failed to cancel acp turn", "session", t.sessionID, "cause", err)
	}
	if p != nil {
		p.in.cancel(t.agent)
	}
	for _, ev := range t.events.removeIf(func(ev turnEvent) bool { return ev.question != nil }) {
		ev.question.cancel(t.agent)
	}
	if e.cancelTimeout > 0 {
		time.AfterFunc(e.cancelTimeout, func() {
			if t.hasEnded() {
				return
			}
			log.Warn(context.Background(), "acp agent didn't stop a canceled turn, stopping the agent", "session", t.sessionID)
			e.stopAgent(t.agent)
		})
	}
}

func failed(info a2a.TaskInfoProvider, text string) *a2a.TaskStatusUpdateEvent {
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, info, a2a.NewTextPart(text))
	return a2a.NewStatusUpdateEvent(info, a2a.TaskStateFailed, msg)
}
