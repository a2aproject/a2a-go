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

package a2aplugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
)

func TestPassthrough(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	ctx := context.WithValue(t.Context(), ctxKey{}, "marker")
	p := a2aplugin.Passthrough{}
	keepCtx := func(got context.Context, err error) error {
		if got != ctx {
			return errors.New("context changed")
		}
		return err
	}

	hooks := map[string]error{
		"BeforeHandlerRun":    keepCtx(p.BeforeHandlerRun(ctx, &a2asrv.ServerRequest{})),
		"AfterHandlerRun":     keepCtx(p.AfterHandlerRun(ctx, &a2asrv.ServerResponse{})),
		"BeforeClientRequest": keepCtx(p.BeforeClientRequest(ctx, &a2aclient.ClientRequest{})),
		"AfterClientRequest":  keepCtx(p.AfterClientRequest(ctx, &a2aclient.ClientResponse{})),
		"BeforeExecution":     keepCtx(p.BeforeExecution(ctx, &a2asrv.ExecutorContext{})),
		"OnExecutionEvent":    p.OnExecutionEvent(ctx, &a2asrv.ExecutorContext{}, &a2a.Task{}),
		"AfterExecution":      p.AfterExecution(ctx, &a2asrv.ExecutorContext{}, nil),
		"OnTaskEvent":         p.OnTaskEvent(ctx, &a2asrv.TaskEventInfo{}),
		"BeforePushSend":      keepCtx(p.BeforePushSend(ctx, &push.Delivery{})),
		"OnPushSent":          p.OnPushSent(ctx, &push.DeliveryResult{}),
	}
	for hook, err := range hooks {
		if err != nil {
			t.Errorf("Passthrough.%s() error = %v, want nil and the input context", hook, err)
		}
	}
	if got := p.Name(); got != "unnamed" {
		t.Errorf("Passthrough.Name() = %q, want %q", got, "unnamed")
	}
}
