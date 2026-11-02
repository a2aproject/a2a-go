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
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	defaultStartTimeout = time.Minute
	minStartBackoff     = time.Second
	maxStartBackoff     = time.Minute
)

// agent is one running ACP agent process.
type agent struct {
	key    string
	client *acp.Client
	caps   acp.AgentCapabilities

	mu       sync.Mutex
	sessions map[string]*sessionInfo // sessions created or loaded by this process
}

type sessionInfo struct {
	options map[string]string // config option ID -> type
	modes   bool
	models  bool
}

// exited reports whether the agent process's output ended. Its turns end
// shortly after, once the pending prompt calls observe it.
func (a *agent) exited() bool {
	select {
	case <-a.client.Done():
		return true
	default:
		return false
	}
}

func (a *agent) hasSession(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[sessionID] != nil
}

func (a *agent) addSession(sessionID string, st acp.SessionState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[sessionID] = &sessionInfo{modes: st.Modes != nil, models: st.Models != nil}
	a.setOptionsLocked(sessionID, st.ConfigOptions)
}

func (a *agent) setOptions(sessionID string, opts []acp.SessionConfigOption) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setOptionsLocked(sessionID, opts)
}

func (a *agent) setOptionsLocked(sessionID string, opts []acp.SessionConfigOption) {
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
		return acp.MethodSetConfigOption, typ, true
	}
	switch {
	case configID == "mode" && info.modes:
		return acp.MethodSetMode, "", true
	case configID == "model" && info.models:
		return acp.MethodSetModel, "", true
	}
	return "", "", false
}

// ErrNotOwner is returned, wrapped, by [Agent.Start] when another node owns
// the agent of the key, for example because it holds the lease of the key's
// sandbox. The task fails with the error, but unlike other start failures it
// doesn't delay the next start.
var ErrNotOwner = errors.New("the agent is owned by another node")

var errDraining = errors.New("the executor is shutting down, send the message again later")

// agentSlot holds the agent process of one key.
type agentSlot struct {
	key string
	// startMu serializes starting and stopping the agent of the key, so that
	// at most one process per key exists, for example to reuse a sandbox with
	// a fixed name.
	startMu sync.Mutex

	// Guarded by Executor.mu.
	agent    *agent
	refs     int // turns and turn setups using the agent
	lastUsed time.Time
	idle     *time.Timer
	startErr error     // the last failed start
	retryAt  time.Time // when a start may be tried again after startErr
	backoff  time.Duration
}

// acquireAgent returns the running agent of key, starting it if needed, and
// a func to call when the caller stops using it. An agent in use is never
// stopped for being idle.
func (e *Executor) acquireAgent(ctx context.Context, key string) (*agent, func(), error) {
	e.mu.Lock()
	switch {
	case e.closed:
		e.mu.Unlock()
		return nil, nil, errClosed
	case e.draining:
		e.mu.Unlock()
		return nil, nil, errDraining
	}
	s := e.agents[key]
	if s == nil {
		s = &agentSlot{key: key}
		e.agents[key] = s
	}
	s.refs++
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	e.mu.Unlock()

	ag, err := e.ensureAgent(ctx, s)
	if err != nil {
		e.releaseAgent(s)
		return nil, nil, err
	}
	var once sync.Once
	return ag, func() { once.Do(func() { e.releaseAgent(s) }) }, nil
}

func (e *Executor) releaseAgent(s *agentSlot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s.refs--
	s.lastUsed = time.Now()
	if s.refs > 0 {
		return
	}
	if s.agent == nil && s.startErr == nil {
		e.dropSlotLocked(s)
		return
	}
	if s.agent != nil && e.idleTimeout > 0 {
		s.idle = time.AfterFunc(e.idleTimeout, func() { e.stopIdle(s) })
	}
}

func (e *Executor) dropSlotLocked(s *agentSlot) {
	if e.agents[s.key] == s {
		delete(e.agents, s.key)
	}
}

// stopIdle stops the agent of s if nobody used it since it became idle.
func (e *Executor) stopIdle(s *agentSlot) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !e.stopIfIdle(s) {
		return
	}
	log.Info(context.Background(), "stopped idle acp agent", "key", s.key)
}

// stopIfIdle stops the agent of s if it is unused. The caller holds s.startMu.
func (e *Executor) stopIfIdle(s *agentSlot) bool {
	e.mu.Lock()
	ag := s.agent
	if s.refs > 0 || ag == nil {
		e.mu.Unlock()
		return false
	}
	s.agent = nil
	e.mu.Unlock()

	if err := ag.client.Close(); err != nil {
		log.Warn(context.Background(), "failed to stop acp agent", "key", s.key, "cause", err)
	}
	e.mu.Lock()
	if s.refs == 0 && s.agent == nil {
		e.dropSlotLocked(s)
	}
	e.mu.Unlock()
	return true
}

// Close stops all agents. Tasks in progress fail.
func (e *Executor) Close() error {
	e.mu.Lock()
	e.closed = true
	slots := slices.Collect(maps.Values(e.agents))
	e.mu.Unlock()

	var errs []error
	for _, s := range slots {
		s.startMu.Lock()
		e.mu.Lock()
		ag := s.agent
		s.agent = nil
		if s.idle != nil {
			s.idle.Stop()
		}
		e.mu.Unlock()
		if ag != nil {
			errs = append(errs, ag.client.Close())
		}
		s.startMu.Unlock()
	}
	return errors.Join(errs...)
}

// Shutdown stops accepting new tasks, waits for the tasks in progress to
// finish or pause for input, and stops all agents. Questions can still be
// answered meanwhile. If ctx ends first, the remaining tasks are canceled and
// Shutdown returns ctx's error after stopping the agents.
func (e *Executor) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.draining = true
	drained := e.drained
	e.checkDrainedLocked()
	e.mu.Unlock()

	var err error
	select {
	case <-drained:
	case <-ctx.Done():
		err = ctx.Err()
		e.mu.Lock()
		turns := slices.Collect(maps.Values(e.turns))
		waiting := slices.Collect(maps.Values(e.waiting))
		e.mu.Unlock()
		for _, stop := range waiting {
			stop(errDraining)
		}
		for _, t := range turns {
			e.cancelTurn(t)
		}
	}
	return errors.Join(err, e.Close())
}

// checkDrainedLocked signals Shutdown once no turn runs or waits to start.
func (e *Executor) checkDrainedLocked() {
	if !e.draining || len(e.turns) > 0 || len(e.waiting) > 0 {
		return
	}
	select {
	case <-e.drained:
	default:
		close(e.drained)
	}
}

// ensureAgent returns the running agent of s, starting a new one if there is
// none or the previous one exited. After a failed start, starts are refused
// for a growing backoff period, so that a broken agent isn't started for every
// message.
func (e *Executor) ensureAgent(ctx context.Context, s *agentSlot) (*agent, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	e.mu.Lock()
	ag, startErr, retryAt := s.agent, s.startErr, s.retryAt
	e.mu.Unlock()
	if ag != nil {
		if !ag.exited() {
			return ag, nil
		}
		log.Warn(ctx, "acp agent exited, restarting", "key", s.key, "cause", ag.client.Err())
		if err := ag.client.Close(); err != nil {
			log.Warn(ctx, "failed to stop acp agent", "key", s.key, "cause", err)
		}
		e.mu.Lock()
		s.agent = nil
		e.mu.Unlock()
	}
	if startErr != nil && time.Now().Before(retryAt) {
		return nil, fmt.Errorf("the agent failed to start, retrying after %s: %w", retryAt.Format(time.TimeOnly), startErr)
	}
	if err := e.makeRoom(s); err != nil {
		return nil, err
	}

	ag, err := e.startAgent(ctx, s.key)
	e.mu.Lock()
	defer e.mu.Unlock()
	if errors.Is(err, ErrNotOwner) {
		return nil, err
	}
	if err != nil {
		s.backoff = min(max(2*s.backoff, minStartBackoff), maxStartBackoff)
		s.startErr, s.retryAt = err, time.Now().Add(s.backoff)
		return nil, err
	}
	s.agent, s.startErr, s.backoff = ag, nil, 0
	return ag, nil
}

// makeRoom stops the least recently used idle agent if starting the agent of
// s would exceed PoolConfig.MaxAgents.
func (e *Executor) makeRoom(s *agentSlot) error {
	if e.maxAgents <= 0 {
		return nil
	}
	e.mu.Lock()
	running := 0
	var victim *agentSlot
	for _, other := range e.agents {
		if other == s || other.agent == nil {
			continue
		}
		running++
		if other.refs == 0 && (victim == nil || other.lastUsed.Before(victim.lastUsed)) {
			victim = other
		}
	}
	e.mu.Unlock()
	if running < e.maxAgents {
		return nil
	}
	// TryLock: the victim is idle, so its startMu is only held by an idle stop
	// or a turn which just started using it, and then it isn't a victim.
	if victim != nil && victim.startMu.TryLock() {
		stopped := e.stopIfIdle(victim)
		victim.startMu.Unlock()
		if stopped {
			log.Info(context.Background(), "stopped idle acp agent to start another", "key", victim.key, "for", s.key)
			return nil
		}
	}
	return fmt.Errorf("all %d agents are busy, send the message again later", e.maxAgents)
}

// startAgent starts the agent and negotiates the protocol version within Config.StartTimeout.
func (e *Executor) startAgent(ctx context.Context, key string) (*agent, error) {
	if e.startTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.startTimeout)
		defer cancel()
	}
	rwc, err := e.agentStarter.Start(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("starting acp agent: %w", err)
	}
	ag := &agent{key: key, sessions: map[string]*sessionInfo{}}
	ag.client = acp.NewClient(rwc, &agentHandler{e: e, ag: ag})
	ag.client.Start()

	resp, err := ag.client.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion,
		ClientInfo:      &acp.Implementation{Name: "a2a-go", Version: "2"},
	})
	if err == nil && resp.ProtocolVersion != acp.ProtocolVersion {
		err = fmt.Errorf("unsupported acp protocol version %d, want %d", resp.ProtocolVersion, acp.ProtocolVersion)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("initializing acp agent: %w", err), ag.client.Close())
	}
	ag.caps = resp.AgentCapabilities
	return ag, nil
}

// stopAgent stops ag if it is still the running agent of its key. Its turns fail.
func (e *Executor) stopAgent(ag *agent) {
	e.mu.Lock()
	s := e.agents[ag.key]
	e.mu.Unlock()
	if s == nil {
		return
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	e.mu.Lock()
	running := s.agent == ag
	if running {
		s.agent = nil
	}
	e.mu.Unlock()
	if !running {
		return
	}
	if err := ag.client.Close(); err != nil {
		log.Warn(context.Background(), "failed to stop acp agent", "key", ag.key, "cause", err)
	}
}
