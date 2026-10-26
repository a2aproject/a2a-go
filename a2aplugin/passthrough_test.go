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
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

func TestPassthroughSatisfiesCompletePlugin(t *testing.T) {
	t.Parallel()

	var _ a2aplugin.CompletePlugin = a2aplugin.Passthrough{}
}

func TestPassthroughHooksAreNoOps(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	ctx := context.WithValue(t.Context(), ctxKey{}, "marker")
	p := a2aplugin.Passthrough{}

	beforeHandlerCtx, err := p.BeforeHandlerRun(ctx, &a2asrv.ServerRequest{Method: "SendMessage"})
	if err != nil {
		t.Errorf("Passthrough.BeforeHandlerRun() error = %v, want nil", err)
	}
	if beforeHandlerCtx != ctx {
		t.Errorf("Passthrough.BeforeHandlerRun() returned a different context, want the input context unchanged")
	}

	afterHandlerCtx, err := p.AfterHandlerRun(ctx, &a2asrv.ServerResponse{Method: "SendMessage", IsFinal: true})
	if err != nil {
		t.Errorf("Passthrough.AfterHandlerRun() error = %v, want nil", err)
	}
	if afterHandlerCtx != ctx {
		t.Errorf("Passthrough.AfterHandlerRun() returned a different context, want the input context unchanged")
	}

	beforeClientCtx, err := p.BeforeClientRequest(ctx, &a2aclient.ClientRequest{Method: "SendMessage"})
	if err != nil {
		t.Errorf("Passthrough.BeforeClientRequest() error = %v, want nil", err)
	}
	if beforeClientCtx != ctx {
		t.Errorf("Passthrough.BeforeClientRequest() returned a different context, want the input context unchanged")
	}

	afterClientCtx, err := p.AfterClientRequest(ctx, &a2aclient.ClientResponse{Method: "SendMessage", IsFinal: true})
	if err != nil {
		t.Errorf("Passthrough.AfterClientRequest() error = %v, want nil", err)
	}
	if afterClientCtx != ctx {
		t.Errorf("Passthrough.AfterClientRequest() returned a different context, want the input context unchanged")
	}
}

func TestPassthroughDefaultName(t *testing.T) {
	t.Parallel()

	if got := (a2aplugin.Passthrough{}).Name(); got != "unnamed" {
		t.Errorf("Passthrough{}.Name() = %q, want %q", got, "unnamed")
	}
}
