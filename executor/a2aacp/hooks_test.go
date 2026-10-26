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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/internal/acp"
	"github.com/google/go-cmp/cmp"
)

func TestToolRules(t *testing.T) {
	t.Parallel()
	permission := func(kind string) Question {
		return Question{Tool: &ToolCall{ID: "t1", Kind: kind}, Options: []string{OptionOnce, OptionReject}}
	}
	tests := []struct {
		name   string
		rules  ToolRules
		q      Question
		want   Answer
		wantOK bool
	}{
		{name: "exact", rules: ToolRules{Allow: []string{"read"}}, q: permission("read"), want: Answer{Option: OptionOnce}, wantOK: true},
		{name: "prefix", rules: ToolRules{Allow: []string{"exec*"}}, q: permission("execute"), want: Answer{Option: OptionOnce}, wantOK: true},
		{name: "suffix", rules: ToolRules{Allow: []string{"*ute"}}, q: permission("execute"), want: Answer{Option: OptionOnce}, wantOK: true},
		{name: "infix", rules: ToolRules{Allow: []string{"e*c*e"}}, q: permission("execute"), want: Answer{Option: OptionOnce}, wantOK: true},
		{name: "question mark is literal", rules: ToolRules{Allow: []string{"rea?"}}, q: permission("read")},
		{name: "no match", rules: ToolRules{Allow: []string{"exec"}}, q: permission("execute")},
		{name: "deny wins", rules: ToolRules{Allow: []string{"*"}, Deny: []string{"execute"}}, q: permission("execute"), want: Answer{Option: OptionReject}, wantOK: true},
		{
			name:   "deny without reject option cancels",
			rules:  ToolRules{Deny: []string{"*"}},
			q:      Question{Tool: &ToolCall{Kind: "read"}, Options: []string{OptionOnce}},
			want:   Answer{Option: OptionCancel},
			wantOK: true,
		},
		{name: "forms are asked", rules: ToolRules{Allow: []string{"*"}}, q: Question{Options: []string{OptionAccept}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.rules.AutoAnswer(t.Context(), nil, tc.q)

			if ok != tc.wantOK {
				t.Fatalf("ToolRules.AutoAnswer() ok = %v, want %v", ok, tc.wantOK)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("ToolRules.AutoAnswer() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestExecutor_AutoAnswerWithUnofferedOptionAsksCaller(t *testing.T) {
	t.Parallel()
	h := &fakeHarness{onPrompt: func(s *promptScript) {
		s.askPermission(rmToolCall)
		s.end(acp.StopEndTurn)
	}}
	e := newTestExecutor(t, h, Config{AutoAnswer: AutoAnswerFunc(func(context.Context, *a2asrv.ExecutorContext, Question) (Answer, bool) {
		return Answer{Option: "maybe"}, true
	})})
	execCtx := newExecCtx("ctx", a2a.NewTextPart("delete x.txt"))

	got := run(t, execute(e, execCtx))

	if diff := cmp.Diff([]eventSummary{submitted, working, rmInputRequired("")}, got); diff != "" {
		t.Fatalf("Executor.Execute() wrong result (-want +got) diff = %s", diff)
	}
	run(t, execute(e, followUp(execCtx, a2a.NewTextPart("once"))))
}
