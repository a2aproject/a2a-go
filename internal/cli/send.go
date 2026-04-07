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

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func newSendCmd(cfg *globalConfig) *cobra.Command {
	var (
		stream    bool
		immediate bool
		jsonBody  string
		partsJSON string
		file      string
		taskID    string
		contextID string
		history   int
	)

	cmd := &cobra.Command{
		Use:   "send <url> [message]",
		Short: "Send a message to an agent",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			msg, err := buildMessage(args[1:], jsonBody, partsJSON, file)
			if err != nil {
				return err
			}
			if taskID != "" {
				msg.TaskID = a2a.TaskID(taskID)
			}
			if contextID != "" {
				msg.ContextID = contextID
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.timeout)
			defer cancel()
			ctx = withServiceParams(ctx, cfg)

			client, err := newClient(ctx, cfg, args[0])
			if err != nil {
				return fmt.Errorf("failed to create a client: %w", err)
			}
			defer func() {
				if err := client.Destroy(); err != nil {
					cfg.logf("failed to destroy client: %v", err)
				}
			}()

			req := &a2a.SendMessageRequest{Message: msg, Tenant: cfg.tenant}
			if immediate || cmd.Flags().Changed("history") {
				req.Config = &a2a.SendMessageConfig{}
				if immediate {
					req.Config.ReturnImmediately = true
				}
				if cmd.Flags().Changed("history") {
					req.Config.HistoryLength = &history
				}
			}

			if stream {
				for event, err := range client.SendStreamingMessage(ctx, req) {
					if err != nil {
						return fmt.Errorf("streaming error: %w", err)
					}
					if err := cfg.printEvent(event); err != nil {
						return fmt.Errorf("failed to print event: %w", err)
					}
				}
				return nil
			}

			result, err := client.SendMessage(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to send message: %w", err)
			}
			if err := cfg.printSendResult(result); err != nil {
				return fmt.Errorf("failed to print result: %w", err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&stream, "stream", false, "Use streaming response")
	f.BoolVar(&immediate, "immediate", false, "Return immediately (fire-and-forget)")
	f.StringVar(&jsonBody, "json", "", "Raw JSON Message object")
	f.StringVar(&partsJSON, "parts", "", "Raw JSON parts array")
	f.StringVarP(&file, "file", "f", "", "Read message from a JSON file")
	f.StringVar(&taskID, "task", "", "Task ID to continue an existing task")
	f.StringVar(&contextID, "context", "", "Context ID")
	f.IntVar(&history, "history", 0, "Request n history messages in the response")

	return cmd
}

func buildMessage(positional []string, jsonBody, partsJSON, file string) (*a2a.Message, error) {
	switch {
	case jsonBody != "":
		msg := new(a2a.Message)
		if err := json.Unmarshal([]byte(jsonBody), msg); err != nil {
			return nil, fmt.Errorf("parsing --json: %w", err)
		}
		if msg.ID == "" {
			msg.ID = a2a.NewMessageID()
		}
		return msg, nil

	case partsJSON != "":
		var parts a2a.ContentParts
		if err := json.Unmarshal([]byte(partsJSON), &parts); err != nil {
			return nil, fmt.Errorf("parsing --parts: %w", err)
		}
		return a2a.NewMessage(a2a.MessageRoleUser, parts...), nil

	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading message file: %w", err)
		}
		msg := new(a2a.Message)
		if err := json.Unmarshal(data, msg); err != nil {
			return nil, fmt.Errorf("parsing message file: %w", err)
		}
		if msg.ID == "" {
			msg.ID = a2a.NewMessageID()
		}
		return msg, nil

	case len(positional) > 0:
		text := strings.Join(positional, " ")
		return a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(text)), nil

	default:
		return nil, fmt.Errorf("provide a message as text, --json, --parts, or -f")
	}
}
