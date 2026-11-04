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

package plugincall

import (
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-cmp/cmp"
)

type afterCall struct {
	Payload       any
	Err           error
	IsStreamEvent bool
}

func TestCall(t *testing.T) {
	t.Parallel()

	msg := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hi"))
	errFailed := errors.New("failed")

	testCases := []struct {
		name string
		run  func(c *Call)
		want []afterCall
	}{
		{
			name: "finish once",
			run: func(c *Call) {
				c.Finish(msg, nil)
				c.Finish(msg, nil)
			},
			want: []afterCall{{Payload: msg}},
		},
		{
			name: "finish drops payload on error",
			run:  func(c *Call) { c.Finish(msg, errFailed) },
			want: []afterCall{{Err: errFailed}},
		},
		{
			name: "stream",
			run: func(c *Call) {
				yield := c.WrapStreamYield(func(a2a.Event, error) bool { return true })
				yield(msg, nil)
				yield(msg, errFailed)
				c.FinishStream()
			},
			want: []afterCall{
				{Payload: msg, IsStreamEvent: true},
				{Err: errFailed, IsStreamEvent: true},
				{Err: errFailed},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []afterCall
			tc.run(New(func(payload any, err error, isStreamEvent bool) {
				got = append(got, afterCall{Payload: payload, Err: err, IsStreamEvent: isStreamEvent})
			}))
			if diff := cmp.Diff(tc.want, got, cmp.Comparer(func(a, b error) bool { return a == b })); diff != "" {
				t.Fatalf("Call wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

func TestNilCall(t *testing.T) {
	t.Parallel()

	var c *Call
	c.Finish(nil, nil)
	c.FinishStream()
	yielded := false
	c.WrapStreamYield(func(a2a.Event, error) bool { yielded = true; return true })(nil, nil)
	if !yielded {
		t.Fatalf("WrapStreamYield() did not call yield")
	}
}
