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
	"encoding/json"
	"flag"
	"log"

	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/examples/tasksearch/extension"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of AgentCard server.")
	query   = flag.String("query", "", "Query to search for tasks.")
)

func main() {
	flag.Parse()
	ctx := context.Background()
	card, err := agentcard.DefaultResolver.Resolve(ctx, *cardURL)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}

	withInsecureGRPC := a2aclient.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials()))
	client, err := a2aclient.NewFromCard(ctx, card, withInsecureGRPC)
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}

	search, err := a2aclient.Invoke(
		ctx,
		client,
		tasksearchext.ClientTaskSearch,
		&tasksearchext.Request{Query: *query},
	)
	if err != nil {
		log.Fatalf("Failed to invoke a method: %v", err)
	}
	str, err := json.MarshalIndent(search, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal a response: %v", err)
	}
	log.Printf("Server responded with: %s", str)
}
