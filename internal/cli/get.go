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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func newGetCmd(cfg *globalConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a resource",
	}
	cmd.AddCommand(newGetTaskCmd(cfg), newGetCardCmd(cfg))
	return cmd
}

func newGetCardCmd(cfg *globalConfig) *cobra.Command {
	var extended bool

	cmd := &cobra.Command{
		Use:   "card <url>",
		Short: "Fetch and display an agent card (alias for discover)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(cmd.Context(), cfg, args[0], extended)
		},
	}

	cmd.Flags().BoolVar(&extended, "extended", false, "Fetch the extended agent card")
	return cmd
}

func newGetTaskCmd(cfg *globalConfig) *cobra.Command {
	var history int

	cmd := &cobra.Command{
		Use:   "task <url> <task-id>",
		Short: "Get task details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			req := &a2a.GetTaskRequest{
				ID:     a2a.TaskID(args[1]),
				Tenant: cfg.tenant,
			}
			if cmd.Flags().Changed("history") {
				req.HistoryLength = &history
			}

			task, err := client.GetTask(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to get task %s: %w", args[1], err)
			}

			if err := cfg.printTask(task); err != nil {
				return fmt.Errorf("failed to print task: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&history, "history", 0, "Include up to n history messages")
	return cmd
}
