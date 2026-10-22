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
// context maps to an ACP session and every task to an ACP prompt turn. Tasks
// in the same context run one at a time, in arrival order. Agent text streams
// into a "response" artifact and tool calls become working status updates.
//
// Permission requests and form elicitations pause the task in input-required.
// The status message has a text part for people and a data part
// {"hitl": {"id", "type", "title", "detail", "options", "schema"}} for
// programs. The caller answers by sending a message on the same task: an
// option ID or label, yes/no for permissions, a JSON object of form fields,
// or a data part {"hitl": {"id": "<question id>", "answer": <answer>}}.
// Unanswered questions are rejected after [Config.QuestionTimeout] and the
// turn is canceled; a later reply to such a task fails it.
//
// A message can change session settings, such as the mode or the model, with
// metadata {"acpConfig": {"<config id>": <value>}}. Config IDs are those the
// agent reports in configOptions; "mode" and "model" also work with agents
// which only support session/set_mode and session/set_model.
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
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/log"
)

// Config configures an [Executor].
type Config struct {
	// Profile selects the command of a known agent: "opencode", "claude", "codex" or "gemini".
	Profile string
	// Command is the agent command line. It takes precedence over Profile.
	Command []string
	// Dir is the working directory of the agent and its sessions. Defaults to the current directory.
	Dir string
	// Env is added to the environment of the agent.
	Env []string
	// Stderr receives the agent's stderr. Discarded if nil.
	Stderr io.Writer

	// AllowTools and DenyTools answer permission requests without asking the
	// caller. They hold glob patterns matched against the ACP tool kind: read,
	// edit, delete, move, search, execute, think, fetch or other. DenyTools
	// takes precedence.
	AllowTools []string
	DenyTools  []string
	// QuestionTimeout is how long a question waits for the caller before it is
	// rejected and the turn canceled. Zero means 24 hours, negative means forever.
	QuestionTimeout time.Duration

	// SessionConfig is applied to every session the agent starts or loads, for
	// example {"model": "anthropic/claude-sonnet-4"}. See the package documentation.
	SessionConfig map[string]any
	// Sessions maps A2A contexts to ACP sessions. Defaults to an in-memory store.
	Sessions SessionStore
	// MCPServers provides the MCP servers of new sessions. Optional.
	MCPServers MCPServerProvider

	// start overrides how the agent is started, for tests.
	start startFunc
}

// MCPServerProvider decides which MCP servers the agent connects to in the
// session of an A2A context.
type MCPServerProvider interface {
	// MCPServers returns servers in the JSON form of the ACP McpServer schema,
	// for example {"name": "fs", "command": "mcp-fs", "args": [], "env": []} or
	// {"type": "http", "name": "api", "url": "https://...", "headers": []}.
	MCPServers(ctx context.Context, execCtx *a2asrv.ExecutorContext) ([]json.RawMessage, error)
}

// Executor implements [a2asrv.AgentExecutor] for an ACP agent.
type Executor struct {
	cfg   Config
	start startFunc
	rules toolRules

	startMu sync.Mutex

	mu     sync.Mutex
	agent  *agent
	closed bool
	routes map[string]*turn     // ACP session ID -> turn in progress
	turns  map[a2a.TaskID]*turn // task ID -> turn in progress
	locks  map[string]*ctxLock  // A2A context ID -> turn lock
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

const (
	defaultQuestionTimeout = 24 * time.Hour
	configMetadataKey      = "acpConfig"
)

var errClosed = errors.New("acp executor is closed")

// errQuestionGone is reported to a caller who answers a question the executor no longer tracks.
const errQuestionGone = "the question is no longer pending: it timed out or the agent restarted. Send a new message to continue"

// agent is one running ACP agent process.
type agent struct {
	conn *conn
	caps agentCapabilities

	mu       sync.Mutex
	sessions map[string]*sessionInfo // sessions created or loaded by this process
}

type sessionInfo struct {
	options map[string]string // config option ID -> type
	modes   bool
	models  bool
}

func (a *agent) hasSession(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[sessionID] != nil
}

func (a *agent) addSession(sessionID string, st sessionState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[sessionID] = &sessionInfo{modes: st.Modes != nil, models: st.Models != nil}
	a.setOptionsLocked(sessionID, st.ConfigOptions)
}

func (a *agent) setOptions(sessionID string, opts []sessionConfigOption) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setOptionsLocked(sessionID, opts)
}

func (a *agent) setOptionsLocked(sessionID string, opts []sessionConfigOption) {
	info := a.sessions[sessionID]
	if info == nil || opts == nil {
		return
	}
	info.options = map[string]string{}
	for _, o := range opts {
		info.options[o.ID] = o.Type
	}
}

// configMethod returns the ACP method which changes a session setting, and
// the type of the config option for session/set_config_option.
func (a *agent) configMethod(sessionID, configID string) (string, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.sessions[sessionID]
	if info == nil {
		return "", "", false
	}
	if typ, ok := info.options[configID]; ok {
		return methodSetConfigOption, typ, true
	}
	switch {
	case configID == "mode" && info.modes:
		return methodSetMode, "", true
	case configID == "model" && info.models:
		return methodSetModel, "", true
	}
	return "", "", false
}

// ctxLock serializes turns of one A2A context. refs counts holders and
// waiters, so that the lock can be dropped when nobody uses it.
type ctxLock struct {
	ch   chan struct{}
	refs int
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
	start := cfg.start
	if start == nil {
		argv, err := agentCommand(cfg)
		if err != nil {
			return nil, err
		}
		start = localStart(argv, cfg.Dir, cfg.Env, cfg.Stderr)
	}
	e := &Executor{
		cfg:    cfg,
		start:  start,
		rules:  toolRules{allow: cfg.AllowTools, deny: cfg.DenyTools},
		routes: map[string]*turn{},
		turns:  map[a2a.TaskID]*turn{},
		locks:  map[string]*ctxLock{},
	}
	if _, err := e.ensureAgent(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// Close stops the agent. Turns in progress fail.
func (e *Executor) Close() error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	e.closed = true
	ag := e.agent
	e.agent = nil
	e.mu.Unlock()
	if ag == nil {
		return nil
	}
	return ag.conn.Close()
}

// ensureAgent returns the running agent, starting a new one if there is none
// or the previous one exited.
func (e *Executor) ensureAgent(ctx context.Context) (*agent, error) {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	ag, closed := e.agent, e.closed
	e.mu.Unlock()
	if closed {
		return nil, errClosed
	}
	if ag != nil {
		select {
		case <-ag.conn.Done():
			log.Warn(ctx, "acp agent exited, restarting", "cause", ag.conn.Err())
			if err := ag.conn.Close(); err != nil {
				log.Warn(ctx, "failed to stop acp agent", "cause", err)
			}
		default:
			return ag, nil
		}
	}

	rwc, err := e.start(ctx)
	if err != nil {
		return nil, err
	}
	ag = &agent{sessions: map[string]*sessionInfo{}}
	ag.conn = newConn(rwc, func(m *rpcMessage) { e.handle(ag, m) })
	ag.conn.start()

	var resp initializeResponse
	err = ag.conn.call(ctx, methodInitialize, initializeRequest{
		ProtocolVersion: protocolVersion,
		ClientInfo:      &implementation{Name: "a2a-go", Version: "2"},
	}, &resp)
	if err == nil && resp.ProtocolVersion != protocolVersion {
		err = fmt.Errorf("unsupported acp protocol version %d, want %d", resp.ProtocolVersion, protocolVersion)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initializing acp agent: %w", err), ag.conn.Close())
	}
	ag.caps = resp.AgentCapabilities

	e.mu.Lock()
	e.agent = ag
	e.mu.Unlock()
	return ag, nil
}

// handle processes requests and notifications from the agent. It runs on the
// read goroutine of the connection and must not block.
func (e *Executor) handle(ag *agent, m *rpcMessage) {
	ctx := context.Background()
	in := &incomingQuestion{rpcID: m.ID}
	var sessionID string
	switch m.Method {
	case methodSessionUpdate:
		var n sessionNotification
		if err := json.Unmarshal(m.Params, &n); err != nil {
			log.Warn(ctx, "malformed acp session update", "cause", err)
			return
		}
		if n.Update.SessionUpdate == updateConfigOption {
			ag.setOptions(n.SessionID, n.Update.ConfigOptions)
		}
		if t := e.route(ag, n.SessionID); t != nil {
			t.events.push(turnEvent{update: &n.Update})
		}
		return

	case methodRequestPermission:
		in.permission = &requestPermissionRequest{}
		if !decodeRequest(ag, m, in.permission) {
			return
		}
		sessionID = in.permission.SessionID

	case methodElicitationCreate:
		in.elicitation = &createElicitationRequest{}
		if !decodeRequest(ag, m, in.elicitation) {
			return
		}
		if in.elicitation.Mode != "form" {
			respondError(ag, m.ID, &rpcError{Code: codeInvalidParams, Message: "only form elicitation is supported"})
			return
		}
		sessionID = in.elicitation.SessionID

	default:
		if len(m.ID) > 0 {
			respondError(ag, m.ID, &rpcError{Code: codeMethodNotFound, Message: "method not supported by client: " + m.Method})
		}
		return
	}

	t := e.route(ag, sessionID)
	if t == nil || t.isCanceled() {
		in.cancel(ag)
		return
	}
	t.events.push(turnEvent{question: in})
}

func decodeRequest(ag *agent, m *rpcMessage, v any) bool {
	if len(m.ID) == 0 {
		return false
	}
	if err := json.Unmarshal(m.Params, v); err != nil {
		respondError(ag, m.ID, &rpcError{Code: codeInvalidParams, Message: err.Error()})
		return false
	}
	return true
}

func respondError(ag *agent, id json.RawMessage, rpcErr *rpcError) {
	if err := ag.conn.respondError(id, rpcErr); err != nil {
		log.Warn(context.Background(), "failed to respond to acp agent", "cause", err)
	}
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
			if !e.answer(execCtx, t, p, yield) {
				return
			}
		case execCtx.StoredTask != nil && execCtx.StoredTask.Status.State == a2a.TaskStateInputRequired:
			yield(failed(execCtx, errQuestionGone), nil)
			return
		default:
			var err error
			if t, err = e.startTurn(ctx, execCtx); err != nil {
				yield(failed(execCtx, err.Error()), nil)
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
		e.mu.Unlock()
		if t != nil {
			e.cancelTurn(t)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled, nil), nil)
	}
}

// answer forwards the caller's reply to the pending question. It returns
// false if Execute should stop, for example because the reply didn't answer
// the question and the task stays in input-required.
func (e *Executor) answer(execCtx *a2asrv.ExecutorContext, t *turn, p *pendingQuestion, yield func(a2a.Event, error) bool) bool {
	ans, err := parseAnswer(p.q, execCtx.Message)
	if err != nil {
		preamble := "I didn't understand the reply. "
		if !errors.Is(err, errNoAnswer) {
			preamble = fmt.Sprintf("I couldn't use the reply: %v. ", err)
		}
		yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, inputRequiredMessage(execCtx, p.q, preamble)), nil)
		return false
	}
	if !t.takePending(p) {
		yield(failed(execCtx, errQuestionGone), nil)
		return false
	}
	if err := p.reply(t.agent, ans); err != nil {
		e.cancelTurn(t)
		yield(failed(execCtx, fmt.Sprintf("forwarding answer to acp agent: %v", err)), nil)
		return false
	}
	return true
}

// startTurn waits for the previous turn in the context to finish and sends
// the message as a new prompt.
func (e *Executor) startTurn(ctx context.Context, execCtx *a2asrv.ExecutorContext) (*turn, error) {
	unlock, err := e.lockContext(ctx, execCtx.ContextID)
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

func (e *Executor) prompt(ctx context.Context, execCtx *a2asrv.ExecutorContext, unlock func()) (*turn, error) {
	ag, err := e.ensureAgent(ctx)
	if err != nil {
		return nil, err
	}
	prompt, err := toPrompt(execCtx.Message, ag.caps.PromptCapabilities)
	if err != nil {
		return nil, err
	}
	sessionID, started, err := e.session(ctx, ag, execCtx)
	if err != nil {
		return nil, err
	}
	settings := map[string]any{}
	if started {
		maps.Copy(settings, e.cfg.SessionConfig)
	}
	maps.Copy(settings, metadataConfig(execCtx.Metadata))
	maps.Copy(settings, metadataConfig(execCtx.Message.Metadata))
	if err := configure(ctx, ag, sessionID, settings); err != nil {
		return nil, fmt.Errorf("configuring acp session: %w", err)
	}

	t := newTurn(execCtx.TaskID, sessionID, ag)
	e.mu.Lock()
	e.routes[sessionID] = t
	e.turns[execCtx.TaskID] = t
	e.mu.Unlock()

	go func() {
		var resp promptResponse
		err := ag.conn.call(context.WithoutCancel(ctx), methodSessionPrompt, promptRequest{SessionID: sessionID, Prompt: prompt}, &resp)
		t.end()
		t.events.push(turnEvent{result: &promptResult{stopReason: resp.StopReason, err: err}})
		e.mu.Lock()
		if e.routes[sessionID] == t {
			delete(e.routes, sessionID)
		}
		if e.turns[t.taskID] == t {
			delete(e.turns, t.taskID)
		}
		e.mu.Unlock()
		unlock()
	}()
	return t, nil
}

// session returns the ACP session of the A2A context. It reuses a session the
// agent already knows, loads a session created by a previous agent process,
// or creates a new one. started reports whether the session was loaded or
// created by this call.
func (e *Executor) session(ctx context.Context, ag *agent, execCtx *a2asrv.ExecutorContext) (id string, started bool, err error) {
	id, ok, err := e.cfg.Sessions.Get(ctx, execCtx.ContextID)
	if err != nil {
		return "", false, err
	}
	if ok && ag.hasSession(id) {
		return id, false, nil
	}
	servers, err := e.mcpServers(ctx, execCtx)
	if err != nil {
		return "", false, err
	}
	if ok && ag.caps.LoadSession {
		var resp sessionState
		err := ag.conn.call(ctx, methodSessionLoad, loadSessionRequest{SessionID: id, Cwd: e.cfg.Dir, MCPServers: servers}, &resp)
		if err == nil {
			ag.addSession(id, resp)
			return id, true, nil
		}
		log.Warn(ctx, "failed to load acp session, starting a new one", "session", id, "cause", err)
	}

	var resp newSessionResponse
	if err := ag.conn.call(ctx, methodSessionNew, newSessionRequest{Cwd: e.cfg.Dir, MCPServers: servers}, &resp); err != nil {
		return "", false, fmt.Errorf("creating acp session: %w", err)
	}
	ag.addSession(resp.SessionID, resp.sessionState)
	if err := e.cfg.Sessions.Put(ctx, execCtx.ContextID, resp.SessionID); err != nil {
		return "", false, err
	}
	return resp.SessionID, true, nil
}

func (e *Executor) mcpServers(ctx context.Context, execCtx *a2asrv.ExecutorContext) ([]json.RawMessage, error) {
	if e.cfg.MCPServers == nil {
		return []json.RawMessage{}, nil
	}
	servers, err := e.cfg.MCPServers.MCPServers(ctx, execCtx)
	if err != nil {
		return nil, fmt.Errorf("resolving mcp servers: %w", err)
	}
	if servers == nil {
		return []json.RawMessage{}, nil
	}
	return servers, nil
}

func metadataConfig(md map[string]any) map[string]any {
	cfg, _ := md[configMetadataKey].(map[string]any)
	return cfg
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
		case methodSetConfigOption:
			req := setConfigOptionRequest{SessionID: sessionID, ConfigID: id, Value: value}
			if typ == "boolean" {
				req.Type = typ
			}
			var resp setConfigOptionResponse
			if err = ag.conn.call(ctx, method, req, &resp); err == nil {
				ag.setOptions(sessionID, resp.ConfigOptions)
			}
		case methodSetMode, methodSetModel:
			s, isText := value.(string)
			if !isText {
				return fmt.Errorf("the session setting %q must be a string", id)
			}
			var req any = setModeRequest{SessionID: sessionID, ModeID: s}
			if method == methodSetModel {
				req = setModelRequest{SessionID: sessionID, ModelID: s}
			}
			err = ag.conn.call(ctx, method, req, nil)
		}
		if err != nil {
			return fmt.Errorf("setting %q: %w", id, err)
		}
	}
	return nil
}

func (e *Executor) lockContext(ctx context.Context, contextID string) (func(), error) {
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
	select {
	case l.ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-l.ch
				release()
			})
		}, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
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
			for _, out := range t.onUpdate(execCtx, ev.update) {
				if !yield(out, nil) {
					e.cancelTurn(t)
					return
				}
			}

		case ev.question != nil:
			p := t.newPending(ev.question)
			if ans, ok := e.rules.decide(p.q); ok {
				if _, offered := p.q.option(ans.option); offered {
					if err := p.reply(t.agent, ans); err != nil {
						e.cancelTurn(t)
						yield(failed(execCtx, fmt.Sprintf("forwarding answer to acp agent: %v", err)), nil)
						return
					}
					continue
				}
			}
			if !t.setPending(p, e.cfg.QuestionTimeout, func() { e.expire(t, p) }) {
				p.in.cancel(t.agent)
				continue
			}
			yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired, inputRequiredMessage(execCtx, p.q, "")), nil)
			return

		case ev.result != nil:
			yield(ev.result.event(execCtx), nil)
			return
		}
	}
}

// expire rejects a question nobody answered in time and cancels its turn,
// since nobody would see what the agent does next.
func (e *Executor) expire(t *turn, p *pendingQuestion) {
	if !t.takePending(p) {
		return
	}
	log.Info(context.Background(), "acp question timed out", "task", t.taskID, "question", p.q.id)
	p.reject(t.agent)
	e.cancelTurn(t)
}

// cancelTurn asks the agent to stop the turn and cancels its open questions,
// as ACP requires from the client. The turn ends when the agent responds to
// the prompt.
func (e *Executor) cancelTurn(t *turn) {
	p, ok := t.cancel()
	if !ok {
		return
	}
	if err := t.agent.conn.notify(methodSessionCancel, cancelNotification{SessionID: t.sessionID}); err != nil {
		log.Warn(context.Background(), "failed to cancel acp turn", "session", t.sessionID, "cause", err)
	}
	if p != nil {
		p.in.cancel(t.agent)
	}
	for _, ev := range t.events.removeIf(func(ev turnEvent) bool { return ev.question != nil }) {
		ev.question.cancel(t.agent)
	}
}

func failed(info a2a.TaskInfoProvider, text string) *a2a.TaskStatusUpdateEvent {
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, info, a2a.NewTextPart(text))
	return a2a.NewStatusUpdateEvent(info, a2a.TaskStateFailed, msg)
}
