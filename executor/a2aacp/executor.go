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

// Package a2aacp implements an [a2asrv.AgentExecutor] on top of an agent
// speaking the Agent Client Protocol (https://agentclientprotocol.com), such
// as `opencode acp`, Zed's claude-agent-acp or `gemini --experimental-acp`.
//
// One agent process serves all tasks and is restarted if it exits. Every A2A
// context maps to an ACP session and every task to an ACP prompt turn. Tasks in the same context run one at a time, in arrival
// order. Agent text streams into a "response" artifact and tool calls become
// working status updates.
//
// Permission requests and form elicitations pause the task in input-required
// with a question in the format of package [hitl], unless [Config.AutoAnswer],
// [Config.AllowTools] or [Config.DenyTools] decide them. The caller answers by
// sending a message on the same task; [Config.OnInvalidReply] decides what
// happens to a message which isn't an answer. Unanswered questions are
// rejected after [Config.QuestionTimeout] and the turn is canceled; a later
// reply to such a task fails it.
//
// Session settings, such as the mode or the model, come from
// [Config.SessionConfig] and [Config.TurnConfig]. Callers can't change them
// unless TurnConfig allows it, for example with [MetadataConfig].
package a2aacp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/executor/a2aacp/hitl"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Config configures an [Executor].
type Config struct {
	// Profile selects the command of a known agent: "opencode", "claude", "codex" or "gemini".
	Profile string
	// Command is the agent command line. It takes precedence over Profile.
	Command []string
	// Dir is the working directory of the agent, and of its sessions unless
	// Session sets one. Defaults to the current directory.
	Dir string
	// Env is added to the environment of the agent started from Command or Profile.
	Env []string
	// Stderr receives the stderr of the agent started from Command or Profile. Discarded if nil.
	Stderr io.Writer
	// Start starts the agent and returns its stdio: reads come from the
	// agent's output, writes go to its input, and Close stops the agent. It
	// takes precedence over Command and Profile, for example to run the agent
	// in a sandbox; see [StartProcess], [WithCleanup] and [ProfileCommand].
	// The agent must outlive ctx, which ends once the agent is initialized.
	Start func(ctx context.Context) (io.ReadWriteCloser, error)
	// StartTimeout limits starting and initializing the agent. Zero means one minute.
	StartTimeout time.Duration
	// CancelTimeout is how long a canceled turn may run before the agent is
	// stopped, which fails all its turns. Zero waits for the agent forever,
	// and the context of the turn accepts no new turns meanwhile.
	CancelTimeout time.Duration
	// BusyContext decides what happens to a new task in a context which has a
	// turn in progress. Defaults to [BusyQueue]. A reply which interrupts a
	// question (see [ReplyInterrupt]) always waits for its canceled turn.
	BusyContext BusyPolicy

	// AllowTools and DenyTools answer permission requests without asking the
	// caller. They hold glob patterns matched against the ACP tool kind: read,
	// edit, delete, move, search, execute, think, fetch or other. DenyTools
	// takes precedence. An allowed request is approved once, or always if the
	// agent offers no other way to approve it.
	AllowTools []string
	DenyTools  []string
	// AutoAnswer answers questions without asking the caller. It is consulted
	// before AllowTools and DenyTools; returning false leaves the question to
	// them. An answer with an option the question doesn't offer fails the turn.
	AutoAnswer func(ctx context.Context, execCtx *a2asrv.ExecutorContext, q hitl.Question) (hitl.Answer, bool)
	// QuestionTimeout is how long a question waits for the caller before it is
	// rejected and the turn canceled. Zero means 24 hours, negative means forever.
	QuestionTimeout time.Duration
	// OnInvalidReply decides what happens to a message on an input-required
	// task which doesn't answer its question. err is the [hitl.ParseAnswer]
	// error. If nil, the question is asked again. See [InterruptUnrecognized].
	OnInvalidReply func(ctx context.Context, execCtx *a2asrv.ExecutorContext, q hitl.Question, err error) ReplyAction
	// OnQuestionDone is called when a question asked from the caller stops
	// being pending, for example to withdraw notifications about it. No task
	// event reports a question which timed out or was abandoned. It is called
	// from executor goroutines and must not block.
	OnQuestionDone func(ctx context.Context, taskID a2a.TaskID, q hitl.Question, outcome QuestionOutcome)

	// SessionConfig is applied to every session the agent starts or loads, for
	// example {"model": "anthropic/claude-sonnet-4"}. Keys are the config IDs
	// the agent reports in configOptions; "mode" and "model" also work with
	// agents which only support session/set_mode and session/set_model.
	SessionConfig map[string]any
	// TurnConfig returns session settings to apply before a turn, over
	// SessionConfig. Settings stay in effect for later turns of the session.
	// See [MetadataConfig].
	TurnConfig func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error)
	// Prompt returns the parts sent to the agent for the message of a new
	// turn, for example to add context the caller didn't write. The task keeps
	// the original message. Defaults to the message parts.
	Prompt func(ctx context.Context, execCtx *a2asrv.ExecutorContext) ([]*a2a.Part, error)
	// Sessions maps A2A contexts to ACP sessions. Defaults to an in-memory store.
	Sessions SessionStore
	// Session returns the parameters of the session of an A2A context. It is
	// called when the agent creates the session or, after a restart, loads it,
	// for the message which caused it, so parameters which depend on the caller
	// apply to everyone in the context. Optional.
	Session func(ctx context.Context, execCtx *a2asrv.ExecutorContext) (SessionParams, error)
}

// SessionParams are the parameters of an ACP session, fixed for its lifetime
// in an agent process.
type SessionParams struct {
	// Cwd is the working directory of the session. A relative path is
	// resolved against [Config.Dir], which is also the default.
	Cwd string
	// MCPServers are the MCP servers the agent connects to, in the JSON form
	// of the ACP McpServer schema, for example
	// {"name": "fs", "command": "mcp-fs", "args": [], "env": []} or
	// {"type": "http", "name": "api", "url": "https://...", "headers": []}.
	MCPServers []json.RawMessage
}

// Executor implements [a2asrv.AgentExecutor] for an ACP agent.
type Executor struct {
	cfg   Config
	start func(ctx context.Context) (io.ReadWriteCloser, error)
	rules toolRules

	elicitations atomic.Int64

	// startMu serializes starting and stopping agents, so that at most one
	// agent process runs at a time.
	startMu sync.Mutex

	mu       sync.Mutex
	agent    *agent
	closed   bool
	routes   map[string]*turn                       // ACP session ID -> turn in progress
	turns    map[a2a.TaskID]*turn                   // task ID -> turn in progress
	waiting  map[a2a.TaskID]context.CancelCauseFunc // task ID -> turn being started
	locks    map[string]*ctxLock                    // A2A context ID -> turn lock
	startErr error                                  // the last failed start
	retryAt  time.Time                              // when a start may be tried again after startErr
	backoff  time.Duration
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

const defaultQuestionTimeout = 24 * time.Hour

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

// NewExecutor starts the agent and negotiates the protocol version. Call
// [Executor.Close] to stop the agent.
func NewExecutor(ctx context.Context, cfg Config) (*Executor, error) {
	if cfg.Dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg.Dir = wd
	}
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	cfg.Dir = dir
	if cfg.Sessions == nil {
		cfg.Sessions = newMemorySessions()
	}
	if cfg.QuestionTimeout == 0 {
		cfg.QuestionTimeout = defaultQuestionTimeout
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = defaultStartTimeout
	}
	start := cfg.Start
	if start == nil {
		argv, err := agentCommand(cfg)
		if err != nil {
			return nil, err
		}
		p := Process{Argv: argv, Dir: cfg.Dir, Env: cfg.Env, Stderr: cfg.Stderr}
		start = func(ctx context.Context) (io.ReadWriteCloser, error) { return StartProcess(ctx, p) }
	}
	e := &Executor{
		cfg:     cfg,
		start:   start,
		rules:   toolRules{allow: cfg.AllowTools, deny: cfg.DenyTools},
		routes:  map[string]*turn{},
		turns:   map[a2a.TaskID]*turn{},
		waiting: map[a2a.TaskID]context.CancelCauseFunc{},
		locks:   map[string]*ctxLock{},
	}
	if _, err := e.ensureAgent(ctx); err != nil {
		return nil, err
	}
	return e, nil
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
	t := e.routes[sessionID]
	if t == nil || t.agent != ag {
		return nil
	}
	return t
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
			if t, err = e.startTurn(ctx, execCtx, e.cfg.BusyContext); err != nil {
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
	ans, err := hitl.ParseAnswer(p.q, execCtx.Message)
	if err != nil {
		if e.cfg.OnInvalidReply != nil && e.cfg.OnInvalidReply(ctx, execCtx, p.q, err) == ReplyInterrupt {
			return e.interrupt(ctx, execCtx, t, p, yield)
		}
		preamble := "I didn't understand the reply. "
		if !errors.Is(err, hitl.ErrNoAnswer) {
			preamble = fmt.Sprintf("I couldn't use the reply: %v. ", err)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, hitl.NewStatusMessage(execCtx, p.q, preamble)), nil)
		return nil, false
	}
	if t.agent.exited() || !t.takePending(p) {
		yield(failed(execCtx, errQuestionGone), nil)
		return nil, false
	}
	if err := p.reply(t.agent, ans); err != nil {
		e.questionDone(ctx, t, p, QuestionAbandoned)
		e.cancelTurn(t)
		msg := fmt.Sprintf("forwarding answer to acp agent: %v", err)
		if t.agent.exited() {
			msg = errQuestionGone
		}
		yield(failed(execCtx, msg), nil)
		return nil, false
	}
	e.questionDone(ctx, t, p, QuestionAnswered)
	return t, true
}

// interrupt rejects the pending question, cancels its turn and starts a new
// turn with the caller's message once the agent ended the canceled one.
func (e *Executor) interrupt(ctx context.Context, execCtx *a2asrv.ExecutorContext, t *turn, p *pendingQuestion, yield func(a2a.Event, error) bool) (*turn, bool) {
	if t.takePending(p) {
		if err := p.reject(t.agent); err != nil {
			log.Warn(ctx, "failed to reject acp request", "cause", err)
		}
		e.questionDone(ctx, t, p, QuestionInterrupted)
	}
	e.cancelTurn(t)
	next, err := e.startTurn(ctx, execCtx, BusyQueue)
	if err != nil {
		if !errors.Is(err, errTaskCanceled) {
			yield(failed(execCtx, err.Error()), nil)
		}
		return nil, false
	}
	return next, true
}

// startTurn waits for the previous turn in the context to finish, or fails
// with [busyError] if policy is [BusyReject], and sends the message as a new
// prompt. Until the turn is registered, [Executor.Cancel] stops it with
// errTaskCanceled.
func (e *Executor) startTurn(ctx context.Context, execCtx *a2asrv.ExecutorContext, policy BusyPolicy) (*turn, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	e.mu.Lock()
	e.waiting[execCtx.TaskID] = cancel
	e.mu.Unlock()
	defer e.stopWaiting(execCtx.TaskID)

	t, err := e.lockAndPrompt(ctx, execCtx, policy)
	if err != nil && errors.Is(context.Cause(ctx), errTaskCanceled) {
		return nil, errTaskCanceled
	}
	return t, err
}

func (e *Executor) lockAndPrompt(ctx context.Context, execCtx *a2asrv.ExecutorContext, policy BusyPolicy) (*turn, error) {
	unlock, err := e.lockContext(ctx, execCtx.ContextID, execCtx.TaskID, policy == BusyQueue)
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
	turnSettings, err := e.turnConfig(ctx, execCtx)
	if err != nil {
		return nil, err
	}
	ag, err := e.ensureAgent(ctx)
	if err != nil {
		return nil, err
	}
	prompt, err := toPrompt(ctx, parts, ag.caps.PromptCapabilities)
	if err != nil {
		return nil, err
	}
	sessionID, err := e.session(ctx, ag, execCtx)
	if err != nil {
		return nil, err
	}
	settings := map[string]any{}
	configured := ag.isConfigured(sessionID)
	if !configured {
		maps.Copy(settings, e.cfg.SessionConfig)
	}
	maps.Copy(settings, turnSettings)
	if err := configure(ctx, ag, sessionID, settings); err != nil {
		return nil, fmt.Errorf("configuring acp session: %w", err)
	}
	if !configured {
		ag.markConfigured(sessionID)
	}

	t := newTurn(execCtx.TaskID, sessionID, ag)
	e.mu.Lock()
	if err := context.Cause(ctx); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	delete(e.waiting, execCtx.TaskID)
	e.routes[sessionID] = t
	e.turns[execCtx.TaskID] = t
	e.mu.Unlock()

	go func() {
		resp, err := ag.client.Prompt(context.WithoutCancel(ctx), acp.PromptRequest{SessionID: sessionID, Prompt: prompt})
		result := &promptResult{err: err, canceled: t.isCanceled()}
		if err == nil {
			result.reason = resp.StopReason
		}
		if p := t.end(); p != nil {
			e.questionDone(context.WithoutCancel(ctx), t, p, QuestionAbandoned)
		}
		t.events.push(turnEvent{result: result})
		e.mu.Lock()
		if e.routes[sessionID] == t {
			delete(e.routes, sessionID)
		}
		if e.turns[t.taskID] == t {
			delete(e.turns, t.taskID)
		}
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

func (e *Executor) turnConfig(ctx context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error) {
	if e.cfg.TurnConfig == nil {
		return nil, nil
	}
	return e.cfg.TurnConfig(ctx, execCtx)
}

// session returns the ACP session of the A2A context. It reuses a session the
// agent already knows, loads a session created by a previous agent process,
// or creates a new one.
func (e *Executor) session(ctx context.Context, ag *agent, execCtx *a2asrv.ExecutorContext) (string, error) {
	id, ok, err := e.cfg.Sessions.Get(ctx, execCtx.ContextID)
	if err != nil {
		return "", err
	}
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
	if err := e.cfg.Sessions.Put(ctx, execCtx.ContextID, resp.SessionID); err != nil {
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
		params.Cwd = filepath.Join(e.cfg.Dir, params.Cwd)
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
			for _, out := range t.onUpdate(ctx, execCtx, ev.update) {
				if !yield(out, nil) {
					e.cancelTurn(t)
					return
				}
			}

		case ev.question != nil:
			p := t.newPending(ev.question, fmt.Sprintf("elicitation-%d", e.elicitations.Add(1)))
			decided, err := e.autoAnswer(ctx, execCtx, t.agent, p)
			if err != nil {
				e.cancelTurn(t)
				yield(failed(execCtx, fmt.Sprintf("forwarding answer to acp agent: %v", err)), nil)
				return
			}
			if decided {
				continue
			}
			if !t.setPending(p, e.cfg.QuestionTimeout, func() { e.expire(t, p) }) {
				p.in.cancel(t.agent)
				continue
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, hitl.NewStatusMessage(execCtx, p.q, "")), nil)
			return

		case ev.result != nil:
			yield(ev.result.event(execCtx), nil)
			return
		}
	}
}

// autoAnswer answers p with Config.AutoAnswer, AllowTools and DenyTools. A
// denied request is never passed to the caller, even if the agent offers no
// way to reject it.
func (e *Executor) autoAnswer(ctx context.Context, execCtx *a2asrv.ExecutorContext, ag *agent, p *pendingQuestion) (bool, error) {
	if e.cfg.AutoAnswer != nil {
		if ans, ok := e.cfg.AutoAnswer(ctx, execCtx, p.q); ok {
			return true, p.reply(ag, ans)
		}
	}
	switch e.rules.decide(p.q) {
	case ruleDeny:
		return true, p.reject(ag)
	case ruleAllow:
		if id := firstOffered(p.q, hitl.OptionOnce, hitl.OptionAlways); id != "" {
			return true, p.reply(ag, hitl.Answer{Option: id})
		}
	}
	return false, nil
}

func (e *Executor) questionDone(ctx context.Context, t *turn, p *pendingQuestion, outcome QuestionOutcome) {
	if e.cfg.OnQuestionDone != nil {
		e.cfg.OnQuestionDone(ctx, t.taskID, p.q, outcome)
	}
}

// expire rejects a question nobody answered in time and cancels its turn,
// since nobody would see what the agent does next.
func (e *Executor) expire(t *turn, p *pendingQuestion) {
	if !t.takePending(p) {
		return
	}
	ctx := context.Background()
	log.Info(ctx, "acp question timed out", "task", t.taskID, "question", p.q.ID)
	if err := p.reject(t.agent); err != nil {
		log.Warn(ctx, "failed to reject acp request", "cause", err)
	}
	e.questionDone(ctx, t, p, QuestionTimedOut)
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
		e.questionDone(context.Background(), t, p, QuestionCanceled)
	}
	for _, ev := range t.events.removeIf(func(ev turnEvent) bool { return ev.question != nil }) {
		ev.question.cancel(t.agent)
	}
	if e.cfg.CancelTimeout > 0 {
		time.AfterFunc(e.cfg.CancelTimeout, func() {
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
