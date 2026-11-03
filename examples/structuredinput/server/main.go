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

// Package main provides a JSON-RPC server example of an agent requesting structured input
// for a deployment approval using the Structured Input extension.
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"net"
	"net/http"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aext/structuredinput"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/jsonschema-go/jsonschema"
)

var port = flag.Int("port", 9001, "Port for a JSONRPC A2A server to listen on.")

type approval struct {
	Status     string `json:"status"`
	TicketID   string `json:"ticket_id"`
	ApproverID string `json:"approver_id"`
}

var approvalSchema = &jsonschema.Schema{
	Type:  "object",
	Title: "request_deployment_approval_response",
	Properties: map[string]*jsonschema.Schema{
		"status":      {Type: "string", Enum: []any{"approved", "rejected"}},
		"ticket_id":   {Type: "string"},
		"approver_id": {Type: "string"},
	},
	Required: []string{"status", "ticket_id", "approver_id"},
}

type deploymentAgent struct {
	processor structuredinput.Processor
}

func (a *deploymentAgent) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if ec.StoredTask == nil {
			a.requestApproval(ec, yield)
			return
		}
		a.handleContinuation(ctx, ec, yield)
	}
}

func (a *deploymentAgent) requestApproval(ec *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) {
	if !yield(a2a.NewSubmittedTask(ec, ec.Message), nil) {
		return
	}
	// The ID is derived from the ticket, so that a late answer to a different deployment can't be mistaken for this one.
	req := &structuredinput.Request{ID: "approval-TICKET-4092", Schema: approvalSchema}
	yield(structuredinput.NewRequestEvent(ec, req,
		a2a.NewTextPart("A deployment requires manual verification. I have initiated a call to request_deployment_approval:"),
		a2a.NewDataPart(map[string]any{"service": "payment-processing", "version": "v2.5.0-rc1", "region": "us-east-1"}),
		a2a.NewTextPart("Please provide the approval response matching the following schema:"),
	))
}

func (a *deploymentAgent) handleContinuation(ctx context.Context, ec *a2asrv.ExecutorContext, yield func(a2a.Event, error) bool) {
	req, err := structuredinput.PendingRequest(ec.StoredTask)
	if err != nil {
		yield(nil, err)
		return
	}
	if req == nil {
		yield(nil, fmt.Errorf("task is not waiting for an approval: %w", a2a.ErrInvalidParams))
		return
	}

	result, err := a.processor.Process(ctx, req, ec.Message)
	if err != nil {
		yield(nil, err)
		return
	}

	if !result.Accepted() {
		yield(result.NewRetryEvent(ec, a2a.NewTextPart(explainRejection(result))))
		return
	}

	var resp approval
	if err := result.Decode(&resp); err != nil {
		yield(nil, err)
		return
	}
	text := fmt.Sprintf("Deployment of payment-processing v2.5.0-rc1 was rejected by %s.", resp.ApproverID)
	if resp.Status == "approved" {
		text = fmt.Sprintf("Deployment of payment-processing v2.5.0-rc1 approved by %s initiated successfully.", resp.ApproverID)
	}
	msg := a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewTextPart(text))
	yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCompleted, msg), nil)
}

func explainRejection(result *structuredinput.ProcessorResult) string {
	switch result.Outcome {
	case structuredinput.OutcomeInvalid:
		return "Your response did not match the schema."
	case structuredinput.OutcomeIDMismatch:
		return "Your response was meant for a different approval request."
	case structuredinput.OutcomeMultipleInputs:
		return "Please provide a single approval response."
	}
	if result.Errors != nil {
		return "Your data did not match the schema."
	}
	if len(result.Context) > 0 {
		return "The deployment updates the payment-processing service in us-east-1 to v2.5.0-rc1. I can only proceed once the approval is provided."
	}
	return "I can only proceed once the approval is provided."
}

func (a *deploymentAgent) Cancel(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(a2a.NewStatusUpdateEvent(ec, a2a.TaskStateCanceled, nil), nil)
	}
}

func main() {
	flag.Parse()

	addr := fmt.Sprintf("http://127.0.0.1:%d/invoke", *port)
	agentCard := &a2a.AgentCard{
		Name:        "Deployment Agent",
		Description: "Deploys services after a manual approval",
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(addr, a2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Capabilities: a2a.AgentCapabilities{
			Extensions: []a2a.AgentExtension{structuredinput.NewExtension(structuredinput.Draft202012)},
		},
		Skills: []a2a.AgentSkill{
			{
				ID:          "deploy",
				Name:        "Deploy",
				Description: "Deploys a service to production after a manual approval",
				Tags:        []string{"deployment"},
				Examples:    []string{"deploy payment-processing"},
			},
		},
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting a JSONRPC server on 127.0.0.1:%d", *port)

	requestHandler := a2asrv.NewHandler(&deploymentAgent{})

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = http.Serve(listener, mux)
	log.Printf("Server stopped: %v", err)
}
