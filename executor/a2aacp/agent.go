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
	client *acp.Client
	caps   acp.AgentCapabilities

	mu       sync.Mutex
	sessions map[string]*sessionInfo // sessions created or loaded by this process
}

type sessionInfo struct {
	options    map[string]string // config option ID -> type
	modes      bool
	models     bool
	configured bool // Config.SessionConfig was applied
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

func (a *agent) isConfigured(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := a.sessions[sessionID]
	return info != nil && info.configured
}

func (a *agent) markConfigured(sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if info := a.sessions[sessionID]; info != nil {
		info.configured = true
	}
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
	return ag.client.Close()
}

// ensureAgent returns the running agent, starting a new one if there is none
// or the previous one exited. After a failed start, starts are refused for a
// growing backoff period, so that a broken agent isn't started for every message.
func (e *Executor) ensureAgent(ctx context.Context) (*agent, error) {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	ag, closed, startErr, retryAt := e.agent, e.closed, e.startErr, e.retryAt
	e.mu.Unlock()
	if closed {
		return nil, errClosed
	}
	if ag != nil {
		if !ag.exited() {
			return ag, nil
		}
		log.Warn(ctx, "acp agent exited, restarting", "cause", ag.client.Err())
		if err := ag.client.Close(); err != nil {
			log.Warn(ctx, "failed to stop acp agent", "cause", err)
		}
	}
	if startErr != nil && time.Now().Before(retryAt) {
		return nil, fmt.Errorf("the agent failed to start, retrying after %s: %w", retryAt.Format(time.TimeOnly), startErr)
	}

	ag, err := e.startAgent(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.agent = nil
		e.backoff = min(max(2*e.backoff, minStartBackoff), maxStartBackoff)
		e.startErr, e.retryAt = err, time.Now().Add(e.backoff)
		return nil, err
	}
	e.agent, e.startErr, e.backoff = ag, nil, 0
	return ag, nil
}

// startAgent starts the agent and negotiates the protocol version within Config.StartTimeout.
func (e *Executor) startAgent(ctx context.Context) (*agent, error) {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.StartTimeout)
	defer cancel()
	rwc, err := e.start(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting acp agent: %w", err)
	}
	ag := &agent{sessions: map[string]*sessionInfo{}}
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

// stopAgent stops ag if it is still the running agent. Its turns fail.
func (e *Executor) stopAgent(ag *agent) {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	running := e.agent == ag
	if running {
		e.agent = nil
	}
	e.mu.Unlock()
	if !running {
		return
	}
	if err := ag.client.Close(); err != nil {
		log.Warn(context.Background(), "failed to stop acp agent", "cause", err)
	}
}
