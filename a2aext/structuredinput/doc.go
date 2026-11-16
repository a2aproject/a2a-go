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

/*
Package structuredinput implements the Structured Input A2A extension.

The extension standardizes how agents use the input-required task state to request input
matching a JSON Schema, and how clients respond with data satisfying it.
See https://github.com/a2aproject/experimental-ext-structured-input for the specification.

# Agent

Advertise the extension in the agent card:

	card.Capabilities.Extensions = append(
		card.Capabilities.Extensions,
		structuredinput.NewExtension(structuredinput.Draft202012),
	)

Request input from an [a2asrv.AgentExecutor]:

	req := &structuredinput.Request{ID: "approval-1", Schema: schema}
	event, err := structuredinput.NewRequestEvent(execCtx, req, a2a.NewTextPart("Please approve the deployment"))
	if err != nil {
		return err
	}
	if !yield(event, nil) {
		return nil
	}

Process the continuation request:

	req, err := structuredinput.PendingRequest(execCtx.StoredTask)
	if err != nil {
		return err
	}
	var processor structuredinput.Processor
	result, err := processor.Process(ctx, req, execCtx.Message)
	if err != nil {
		return err
	}
	if !result.Accepted() {
		event, err := result.NewRetryEvent(execCtx)
		...
	}
	var approval Approval
	if err := result.Decode(&approval); err != nil {
		return err
	}

# Client

Activate the extension using [a2aext.NewActivator]:

	activator := a2aext.NewActivator(structuredinput.URI)
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithCallInterceptors(activator))

Respond to an input request:

	req, err := structuredinput.PendingRequest(task)
	if err != nil || req == nil {
		return err
	}
	resp, err := structuredinput.NewResponse(task, req, approval)
	if err != nil {
		return err
	}
	result, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: resp})
*/
package structuredinput
