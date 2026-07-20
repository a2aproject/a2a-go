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

// Package a2atest provides utilities for testing A2A agents, including an
// in-process server, data accessor helpers, and test executors.
//
// # Server
//
// The Server type creates an in-process A2A request handler with sensible
// defaults (in-memory task store, work queue, event queue) so you can test
// agent implementations without setting up a real server.
//
//	server := a2atest.NewServer(t, executor)
//	result, err := server.SendMessage(ctx, &a2a.SendMessageRequest{...})
//
// # Executor
//
// This package re-exports test executor helpers for convenience.
//
//	exec := a2atest.ExecutorFromFunction(func(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
//	    return func(yield func(a2a.Event, error) bool) {
//	        yield(a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")), nil)
//	    }
//	})
//
// # Data Accessors
//
// Helper functions for inspecting events in tests:
//
//	parts := a2atest.GetTextParts(event)
//	task := a2atest.MustToTask(t, event)
package a2atest

// Note: the Server type is defined in server.go.
// Executor helpers are re-exported from executor.go.
// Data accessors are defined in helpers.go.
