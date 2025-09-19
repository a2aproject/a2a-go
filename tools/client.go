package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
			fmt.Println("Usage: cli send --text {text} [--grpc] [--no-stream] {url}")
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

	if err != nil {
		return err
	}

	jbytes, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(jbytes))

	return nil
}

func handleSend(ctx context.Context, args []string) error {
	sendFlags := flag.NewFlagSet("send", flag.ExitOnError)

	text := sendFlags.String("text", "", "Text to send (required)")
	useGRPC := sendFlags.Bool("grpc", false, "Use gRPC protocol")
	noStream := sendFlags.Bool("no-stream", false, "Use gRPC protocol")

	err := sendFlags.Parse(args)
	if err != nil {
		return fmt.Errorf("flag parsing failed: %w", err)
	}

	remaining := sendFlags.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("expected single URL argument, got %v", remaining)
	}
	url := remaining[0]

	client, err := createClient(ctx, url, *useGRPC)
	if err != nil {
		return fmt.Errorf("client creation failed: %w", err)
	}

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: *text})
	if *noStream {
		return sendMessage(ctx, client, msg)
	}

	return sendStreamingMessage(ctx, client, msg)
}

func sendMessage(ctx context.Context, client *a2aclient.Client, msg *a2a.Message) error {
	req := &a2a.MessageSendParams{Message: msg, Config: &a2a.MessageSendConfig{Blocking: true}}
	resp, err := client.SendMessage(ctx, req)
	if err != nil {
		return err
	}
	if err := printResult(resp); err != nil {
		return err
	}
	return nil
}

func sendStreamingMessage(ctx context.Context, client *a2aclient.Client, msg *a2a.Message) error {
	for resp, err := range client.SendStreamingMessage(ctx, &a2a.MessageSendParams{Message: msg}) {
		if err != nil {
			return err
		}

		if update, ok := resp.(*a2a.TaskStatusUpdateEvent); ok {
			if text, ok := makeShortUpdateText(update); ok {
				fmt.Println(text)
				continue
			}
		}

		if err := printResult(resp); err != nil {
			return err
		}
	}
	return nil
}

func makeShortUpdateText(update *a2a.TaskStatusUpdateEvent) (string, bool) {
	if update.Status.Message == nil {
		return fmt.Sprintf("[status=%s]", update.Status.State), true
	}

	if len(update.Status.Message.Parts) != 1 {
		return "", false
	}

	part, ok := update.Status.Message.Parts[0].(a2a.TextPart)
	if !ok {
		return "", false
	}

	return fmt.Sprintf("[status=%s] message: %s", update.Status.State, part.Text), true
}

func printResult(result any) error {
	bytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(bytes))
	return nil
}

func createClient(ctx context.Context, url string, useGRPC bool) (*a2aclient.Client, error) {
	if useGRPC {
		creds := grpc.WithTransportCredentials(insecure.NewCredentials())
		return a2aclient.CreateFromEndpoints(
			ctx,
			[]a2a.AgentInterface{{Transport: a2a.TransportProtocolGRPC, URL: url}},
			a2aclient.WithGRPCTransport(creds),
		)
	} else {
		resolver := agentcard.Resolver{BaseURL: url}
		card, err := resolver.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		return a2aclient.CreateFromCard(ctx, card)
	}
}

func printHelp() {
	fmt.Println(
		"A2A client cli:",
		"USAGE:",
		"",
		"  cli <command> [flags] url",
		"",
		"COMMANDS:",
		"  card     Fetch a card from the provided URL",
		"  send     Send text to an A2A server with the provided URL",
		"  help     Show this help message",
		"",
		"EXAMPLES:",
		"  cli card https://example.com",
		"  cli send --text 'Hello World' https://api.example.com",
		"  cli send --text 'Hello' --grpc https://grpc.example.com",
	)
}
