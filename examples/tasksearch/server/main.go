// Copyright 2025 The A2A Authors
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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	tasksearchext "github.com/a2aproject/a2a-go/examples/tasksearch/extension"
	"github.com/a2aproject/a2a-go/internal/taskstore"
	a2alog "github.com/a2aproject/a2a-go/log"
)

var (
	port = flag.Int("port", 9001, "Port for an A2A server to listen on.")
)

var userName = "user"

type agentExecutor struct{}

func (*agentExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return q.Write(ctx, a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: "Hello, world!"}))
}

func (*agentExecutor) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, q eventqueue.Queue) error {
	return nil
}

type authInterceptor struct {
}

func (i *authInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, error) {
	callCtx.User = &a2asrv.AuthenticatedUser{UserName: userName}
	return ctx, nil
}

func (i *authInterceptor) After(ctx context.Context, callCtx *a2asrv.CallContext, resp *a2asrv.Response) error {
	a2alog.Info(ctx, "request served", "method", callCtx.Method())
	return nil
}

func main() {
	flag.Parse()

	agentCard := &a2a.AgentCard{
		Name:               "Task Search Extension Host",
		URL:                fmt.Sprintf("http://127.0.0.1:%d/invoke", *port),
		PreferredTransport: a2a.TransportProtocolJSONRPC,
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to bind to a port: %v", err)
	}
	log.Printf("Starting a JSONRPC server on 127.0.0.1:%d", *port)

	store := setupTaskStore(context.Background())
	taskSearchExt := tasksearchext.NewForServer(store)
	requestHandler := a2asrv.NewHandler(
		&agentExecutor{},
		a2asrv.WithTaskStore(store),
		a2asrv.WithCallInterceptor(&authInterceptor{}),
		a2asrv.WithExtensionMethod(taskSearchExt.Method),
	)

	mux := http.NewServeMux()
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(requestHandler))
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	err = http.Serve(listener, mux)

	log.Printf("Server stopped: %v", err)
}

func setupTaskStore(ctx context.Context) a2asrv.TaskStore {
	store := taskstore.NewMem(taskstore.WithAuthenticator(func(ctx context.Context) (taskstore.UserName, bool) {
		if callCtx, ok := a2asrv.CallContextFrom(ctx); ok {
			return taskstore.UserName(callCtx.User.Name()), true
		}
		return "", false
	}))
	authenticatedCtx, callCtx := a2asrv.WithCallContext(ctx, nil)
	callCtx.User = &a2asrv.AuthenticatedUser{UserName: userName}
	for _, status := range []string{"Hello, world!", "Foo", "FooBar"} {
		task := &a2a.Task{
			ID:        a2a.NewTaskID(),
			ContextID: a2a.NewContextID(),
			Status: a2a.TaskStatus{
				State:   a2a.TaskStateCompleted,
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.TextPart{Text: status}),
			},
		}
		if err := store.Save(authenticatedCtx, task); err != nil {
			log.Fatalf("Failed to save a task: %v", err)
		}
	}
	return store
}
