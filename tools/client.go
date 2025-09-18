package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	ctx := context.Background()
	switch command {
	case "card":
		if err := handleCard(ctx, os.Args[2:]); err != nil {
			fmt.Println("Usage: cli card [--path] {customPath} {baseURL}")
			fmt.Printf("Failed with: %v\n", err)
			os.Exit(1)
		}
	case "send":
		if err := handleSend(ctx, os.Args[2:]); err != nil {
			fmt.Println("Usage: cli send --text {text} [--grpc] {url}")
			fmt.Printf("Failed with: %v\n", err)
			os.Exit(1)
		}
	case "help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func handleCard(ctx context.Context, args []string) error {
	cardFlags := flag.NewFlagSet("card", flag.ExitOnError)

	path := cardFlags.String("path", "", "Custom path to query instread of /.well-known/agent-card.json")

	err := cardFlags.Parse(args)
	if err != nil {
		return fmt.Errorf("flag parsing failed: %w", err)
	}

	remaining := cardFlags.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("expected single URL argument, got %v", remaining)
	}
	url := remaining[0]

	var card *a2a.AgentCard
	resolver := agentcard.Resolver{BaseURL: url}
	if *path == "" {
		card, err = resolver.Resolve(ctx)
	} else {
		card, err = resolver.Resolve(ctx, agentcard.WithPath(*path))
	}

	fmt.Println(card)

	return nil
}

func handleSend(ctx context.Context, args []string) error {
	sendFlags := flag.NewFlagSet("send", flag.ExitOnError)

	text := sendFlags.String("text", "", "Text to send (required)")
	grpc := sendFlags.Bool("grpc", false, "Use gRPC protocol")

	err := sendFlags.Parse(args)
	if err != nil {
		return fmt.Errorf("flag parsing failed: %w", err)
	}

	remaining := sendFlags.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("expected single URL argument, got %v", remaining)
	}
	url := remaining[0]

	var client *a2aclient.Client
	if *grpc {
		client, err = createClientFromCard(ctx, url)
	} else {
		client, err = createClientGRPC(ctx, url)
	}
	if err != nil {
		return fmt.Errorf("client creation failed: %w", err)
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: *text})
	resp, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: msg})
	if err != nil {
		return err
	}

	fmt.Println(resp)

	return nil
}

func createClientFromCard(ctx context.Context, url string) (*a2aclient.Client, error) {
	factory := a2aclient.NewFactory()
	resolver := agentcard.Resolver{BaseURL: url}
	card, err := resolver.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	return factory.CreateFromCard(ctx, card)
}

func createClientGRPC(ctx context.Context, url string) (*a2aclient.Client, error) {
	factory := a2aclient.NewFactory()
	return factory.CreateFromEndpoints(ctx, []a2a.AgentInterface{{Transport: a2a.TransportProtocolGRPC, URL: url}})
}

func printHelp() {
	fmt.Println(
		"A2A client cli:",
		"USAGE:",
		"",
		"  cli <command> [flags] url",
		"",
		"COMMANDS:",
		"  card     Process a card for the given URL",
		"  send     Send text to a URL with optional gRPC",
		"  help     Show this help message",
		"",
		"EXAMPLES:",
		"  cli card https://example.com",
		"  cli send --text 'Hello World' https://api.example.com",
		"  cli send --text 'Hello --grpc https://grpc.example.com",
	)
}
