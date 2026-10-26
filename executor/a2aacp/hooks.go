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

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/executor/a2aacp/hitl"
)

// ReplyAction is what happens to a message on an input-required task which
// doesn't answer its question. See [Config.OnInvalidReply].
type ReplyAction int

const (
	// ReplyAskAgain keeps the task in input-required and repeats the question.
	ReplyAskAgain ReplyAction = iota
	// ReplyInterrupt rejects the question, cancels the turn and sends the
	// message as a new prompt in the same task.
	ReplyInterrupt
)

// InterruptUnrecognized is a [Config.OnInvalidReply] which treats a reply
// that isn't an answer at all as a new instruction, like typing instead of
// approving does in coding agents. Answers with invalid values, such as a
// form value which isn't one of its choices, are asked again.
func InterruptUnrecognized(_ context.Context, _ *a2asrv.ExecutorContext, _ hitl.Question, err error) ReplyAction {
	if errors.Is(err, hitl.ErrNoAnswer) {
		return ReplyInterrupt
	}
	return ReplyAskAgain
}

// QuestionOutcome is why a question stopped being pending. See [Config.OnQuestionDone].
type QuestionOutcome string

const (
	// QuestionAnswered means the caller answered the question.
	QuestionAnswered QuestionOutcome = "answered"
	// QuestionTimedOut means nobody answered within [Config.QuestionTimeout].
	QuestionTimedOut QuestionOutcome = "timed_out"
	// QuestionCanceled means the task was canceled.
	QuestionCanceled QuestionOutcome = "canceled"
	// QuestionInterrupted means a reply interrupted the turn, see [ReplyInterrupt].
	QuestionInterrupted QuestionOutcome = "interrupted"
	// QuestionAbandoned means the turn ended without an answer, for example
	// because the agent exited.
	QuestionAbandoned QuestionOutcome = "abandoned"
)

// ConfigMetadataKey is the metadata key [MetadataConfig] reads session settings from.
const ConfigMetadataKey = "acpConfig"

// MetadataConfig returns a [Config.TurnConfig] which takes session settings
// from {"acpConfig": {"<config id>": <value>}} in the request and message
// metadata, the latter taking precedence. A turn which changes a setting
// whose ID matches none of the allowed glob patterns fails.
//
// Settings can weaken the agent's safeguards: some agents have modes which
// skip permission requests. Only allow what every caller may change.
func MetadataConfig(allowed ...string) func(context.Context, *a2asrv.ExecutorContext) (map[string]any, error) {
	return func(_ context.Context, execCtx *a2asrv.ExecutorContext) (map[string]any, error) {
		settings := map[string]any{}
		maps.Copy(settings, metadataConfig(execCtx.Metadata))
		if execCtx.Message != nil {
			maps.Copy(settings, metadataConfig(execCtx.Message.Metadata))
		}
		for _, id := range slices.Sorted(maps.Keys(settings)) {
			if !matchAny(allowed, id) {
				return nil, fmt.Errorf("the session setting %q can't be changed in a message", id)
			}
		}
		return settings, nil
	}
}

func metadataConfig(md map[string]any) map[string]any {
	cfg, _ := md[ConfigMetadataKey].(map[string]any)
	return cfg
}
