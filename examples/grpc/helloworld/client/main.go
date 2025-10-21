package main

import (
	"context"
	"flag"
	"log"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var cardURL = flag.String("card-url", "http://127.0.0.1:9001", "Base URL of AgentCard server.")

func main() {
	flag.Parse()
	ctx := context.Background()

	// Resolve an AgentCard
	cardResolver := agentcard.Resolver{BaseURL: *cardURL}
	card, err := cardResolver.Resolve(ctx)
	if err != nil {
		log.Fatalf("Failed to resolve an AgentCard: %v", err)
	}

	// Insecure connection is used for example purposes
	withInsecureGRPC := a2aclient.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials()))

	// Create a client connected to one of the interfaces specified in the AgentCard.
	client, err := a2aclient.NewFromCard(ctx, card, withInsecureGRPC)
	if err != nil {
		log.Fatalf("Failed to create a client: %v", err)
	}

	// Send a message and log the response.
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello, world"})
	resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if err != nil {
		log.Fatalf("Failed to send a message: %v", err)
	}

	log.Printf("Server responded with: %+v", resp)
}
