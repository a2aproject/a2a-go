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
	"fmt"
	"path"
	"time"

	"github.com/a2aproject/a2a-go/v2/executor/a2aacp/hitl"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/a2aproject/a2a-go/v2/log"
)

// incomingQuestion is a permission request or a form elicitation from the agent.
type incomingQuestion struct {
	rpcID       json.RawMessage
	permission  *acp.RequestPermissionRequest
	elicitation *acp.CreateElicitationRequest
}

// cancel responds to the request with the cancelled outcome.
func (in *incomingQuestion) cancel(ag *agent) {
	var err error
	if in.elicitation != nil {
		err = ag.client.RespondElicitation(in.rpcID, acp.CreateElicitationResponse{Action: acp.ElicitationCancel})
	} else {
		err = ag.client.RespondPermission(in.rpcID, acp.PermissionOutcome{Outcome: acp.OutcomeCancelled})
	}
	if err != nil {
		log.Warn(context.Background(), "failed to cancel acp request", "cause", err)
	}
}

// pendingQuestion is an ACP request translated to a question for the caller.
type pendingQuestion struct {
	in        *incomingQuestion
	q         hitl.Question
	optionIDs map[string]string // question option ID -> ACP option ID
	timer     *time.Timer
}

var optionIDByKind = map[string]string{
	acp.PermissionAllowOnce:    hitl.OptionOnce,
	acp.PermissionAllowAlways:  hitl.OptionAlways,
	acp.PermissionRejectOnce:   hitl.OptionReject,
	acp.PermissionRejectAlways: hitl.OptionRejectAlways,
}

var formOptions = []hitl.Option{{ID: hitl.ActionDecline, Label: "Decline"}, {ID: hitl.ActionCancel, Label: "Cancel"}}

// newPending translates an agent request. elicitationID identifies a form,
// since the JSON-RPC ID of the request repeats after the agent restarts.
func (t *turn) newPending(in *incomingQuestion, elicitationID string) *pendingQuestion {
	p := &pendingQuestion{in: in, optionIDs: map[string]string{}}
	if el := in.elicitation; el != nil {
		p.q = hitl.Question{
			ID:      elicitationID,
			Type:    hitl.TypeForm,
			Title:   el.Message,
			Schema:  el.RequestedSchema,
			Options: formOptions,
		}
		if len(p.q.Fields()) == 0 {
			p.q.Options = append([]hitl.Option{{ID: hitl.ActionAccept, Label: "Accept"}}, formOptions...)
		}
		return p
	}

	tc := t.mergeTool(in.permission.ToolCall)
	title := tc.Title
	if title == "" {
		title = tc.Kind
	}
	p.q = hitl.Question{ID: tc.ToolCallID, Type: hitl.TypePermission, Tool: tc.Kind, Title: title, Input: tc.RawInput}
	for _, o := range in.permission.Options {
		id, ok := optionIDByKind[o.Kind]
		if _, taken := p.optionIDs[id]; !ok || taken {
			id = o.OptionID
		}
		p.optionIDs[id] = o.OptionID
		p.q.Options = append(p.q.Options, hitl.Option{ID: id, Label: o.Name})
	}
	return p
}

func (p *pendingQuestion) reply(ag *agent, a hitl.Answer) error {
	if p.q.Type == hitl.TypeForm {
		return ag.client.RespondElicitation(p.in.rpcID, acp.CreateElicitationResponse{Action: a.Option, Content: a.Content})
	}
	acpID, ok := p.optionIDs[a.Option]
	if !ok {
		return fmt.Errorf("option %q was not offered", a.Option)
	}
	return ag.client.RespondPermission(p.in.rpcID, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: acpID})
}

// reject declines a form, or rejects a permission request if the agent offers
// a way to, and cancels it otherwise.
func (p *pendingQuestion) reject(ag *agent) error {
	a := hitl.Answer{Option: hitl.ActionDecline}
	if p.q.Type == hitl.TypePermission {
		a.Option = firstOffered(p.q, hitl.OptionReject, hitl.OptionRejectAlways)
		if a.Option == "" {
			p.in.cancel(ag)
			return nil
		}
	}
	return p.reply(ag, a)
}

func firstOffered(q hitl.Question, ids ...string) string {
	for _, id := range ids {
		if _, ok := q.Option(id); ok {
			return id
		}
	}
	return ""
}

// toolRules answer permission requests by tool kind. Patterns are globs, and
// deny takes precedence.
type toolRules struct {
	allow []string
	deny  []string
}

type ruleDecision int

const (
	ruleAsk ruleDecision = iota
	ruleAllow
	ruleDeny
)

func (r toolRules) decide(q hitl.Question) ruleDecision {
	if q.Type != hitl.TypePermission {
		return ruleAsk
	}
	if matchAny(r.deny, q.Tool) {
		return ruleDeny
	}
	if matchAny(r.allow, q.Tool) {
		return ruleAllow
	}
	return ruleAsk
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, s); ok {
			return true
		}
	}
	return false
}
