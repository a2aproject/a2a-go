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

// Package main demonstrates how to install an a2aplugin plugin on an A2A client.
//
// It registers a single [a2aclient.ClientPlugin] as the process-wide default
// with [a2aplugin.SetGlobal]. The plugin:
//
//   - stamps every outgoing request with a monotonically increasing
//     X-Request-ID service parameter (the server-side plugin in
//     examples/plugins/server logs the same ID, giving an end-to-end trace);
//   - times every call and logs the outcome in [a2aclient.ClientPlugin.AfterClientRequest].
//
// Run against the server example with: go run .
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2aplugin"
)

var (
	cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of the A2A server's AgentCard.")
	calls   = flag.Int("calls", 3, "Number of SendMessage calls to issue.")
)

const requestIDHeader = "X-Request-ID"

func main() {
	flag.Parse()
	ctx := context.Background()

	// Register the plugin as the process-wide default. Factory and Client
	// constructors below capture Global() when they run.
	a2aplugin.SetGlobal(a2aplugin.Sequential(&tracePlugin{}))

	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve agent card: %v", err)
	}

	client, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	for i := 1; i <= *calls; i++ {
		msg := a2a.NewMessage(a2a.MessageRoleUser,
			a2a.NewTextPart(fmt.Sprintf("Hello #%d", i)))
		resp, err := client.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			log.Fatalf("SendMessage failed: %v", err)
		}
		log.Printf("Server responded with: %+v", resp)
	}
}

// tracePlugin tags every outgoing call with a request ID and times it.
type tracePlugin struct {
	counter atomic.Int64
}

var _ a2aclient.ClientPlugin = (*tracePlugin)(nil)

// Name implements [a2aplugin.Plugin].
func (*tracePlugin) Name() string { return "trace" }

// startKey is a context key used to propagate the start time from
// BeforeClientRequest to AfterClientRequest.
type startKey struct{}

// requestIDKey is a context key used to recover the request ID that was
// stamped on the outgoing call when logging the response.
type requestIDKey struct{}

// BeforeClientRequest implements [a2aclient.ClientPlugin]. It mutates the
// request's service parameters, which the transport will forward (for
// JSON-RPC and REST that means an HTTP header; for gRPC it becomes metadata).
func (p *tracePlugin) BeforeClientRequest(ctx context.Context, req *a2aclient.ClientRequest) (context.Context, error) {
	id := fmt.Sprintf("req-%d", p.counter.Add(1))
	req.ServiceParams.Append(requestIDHeader, id)
	log.Printf("[trace] -> %s request_id=%s url=%s", req.Method, id, req.BaseURL)
	ctx = context.WithValue(ctx, startKey{}, time.Now())
	ctx = context.WithValue(ctx, requestIDKey{}, id)
	return ctx, nil
}

// AfterClientRequest implements [a2aclient.ClientPlugin]. For a unary call it
// is invoked once with IsFinal=true; for streaming calls it also runs once
// per event (IsStreamEvent=true), which this example ignores.
func (p *tracePlugin) AfterClientRequest(ctx context.Context, resp *a2aclient.ClientResponse) (context.Context, error) {
	if !resp.IsFinal {
		return ctx, nil
	}
	start, _ := ctx.Value(startKey{}).(time.Time)
	id, _ := ctx.Value(requestIDKey{}).(string)
	outcome := "ok"
	if resp.Err != nil {
		outcome = "error=" + resp.Err.Error()
	}
	log.Printf("[trace] <- %s request_id=%s elapsed=%s %s",
		resp.Method, id, time.Since(start), outcome)
	return ctx, nil
}
