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
// as `opencode acp`, Zed's claude-agent-acp or `gemini --experimental-acp`:
//
//	exec, err := a2aacp.NewExecutor(ctx, a2aacp.Process{Argv: []string{"opencode", "acp"}}, nil)
//
// The executor acts as the ACP client: it starts the agent with an [Agent],
// talks JSON-RPC to it over the agent's stdio and translates between A2A tasks
// and ACP sessions.
//
// # Concepts
//
// The A2A concepts map to ACP as follows:
//
//   - Executor: one agent process serves all contexts, unless [Config.Pool]
//     groups contexts into processes, for example one per context or tenant.
//     Agents are started on demand and restarted if they exit.
//   - Context: an ACP session, created with session/new on the first message.
//     [Config.SessionStore] remembers the session, so that a restarted agent
//     can restore it with session/load if it supports that.
//     [Config.Session] sets the session's working directory and MCP servers.
//   - Task: the agent's work on a message, an ACP session/prompt. Tasks in the
//     same context run one at a time, in arrival order, or fail while the
//     context is busy if [Config.RejectBusyContext] is set.
//
// # Translation
//
// Message parts become prompt content: text as text, data parts as JSON text,
// file URLs as resource links, and raw files as images, audio or embedded
// resources, depending on what the agent accepts. [Config.Prompt] changes what
// is sent and [Config.Settings] selects the session's mode, model or other
// config options before each message.
//
// Session updates of the agent become task events through [Config.Converter],
// by default [ConvertUpdate]: the agent's reply streams into the
// [ResponseArtifact] artifact, tool calls become working status updates with a
// "toolCall" data part, and other updates are dropped. The stop reason of the
// prompt ends the task: end_turn completes it, cancelled cancels it, and
// anything else, or an agent error, fails it. Canceling a task sends
// session/cancel.
//
// # Questions
//
// Permission requests and form elicitations pause the task in input-required
// with a [Question] asked as a request of the Structured Input extension (see
// package [github.com/a2aproject/a2a-go/v2/structuredinput]; advertise it in
// the agent card with structuredinput.NewExtension), unless [Config.AutoAnswer]
// decides them, for example with [ToolRules].
//
// The caller answers with an [Answer] as structured input, or by typing an option.
// A message which isn't an answer at all interrupts the agent and is sent as a new
// instruction, like typing instead of approving does in coding agents; an invalid
// answer is asked again. Unanswered questions are rejected after [Config.QuestionTimeout]
// and the agent stops working on the task; a later reply to the task fails it.
//
// Questions are tied to the agent process which asked them, so in a cluster
// every message of a context must reach the node which runs its agent. See
// [PoolConfig.Key], [ErrNotOwner] and [Executor.Shutdown].
package a2aacp
