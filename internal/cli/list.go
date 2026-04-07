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
	"time"

	"github.com/spf13/cobra"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func newListCmd(cfg *globalConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List resources",
	}
	cmd.AddCommand(newListTasksCmd(cfg))
	return cmd
}

func newListTasksCmd(cfg *globalConfig) *cobra.Command {
	var (
		contextID     string
		status        string
		limit         int
		pageToken     string
		history       int
		since         string
		withArtifacts bool
	)

	cmd := &cobra.Command{
		Use:   "tasks <url>",
		Short: "List tasks",
		Args:  cobra.ExactArgs(1),
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

			req := &a2a.ListTasksRequest{
				Tenant:           cfg.tenant,
				ContextID:        contextID,
				PageSize:         limit,
				PageToken:        pageToken,
				IncludeArtifacts: withArtifacts,
			}
			if status != "" {
				s, err := parseTaskState(status)
				if err != nil {
					return err
				}
				req.Status = s
			}
			if cmd.Flags().Changed("history") {
				req.HistoryLength = &history
			}
			if since != "" {
				t, err := time.Parse(time.RFC3339, since)
				if err != nil {
					return fmt.Errorf("parsing --since: %w", err)
				}
				req.StatusTimestampAfter = &t
			}

			resp, err := client.ListTasks(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to list tasks: %w", err)
			}

			if err := cfg.printTaskList(resp); err != nil {
				return fmt.Errorf("failed to print task list: %w", err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&contextID, "context", "", "Filter by context ID")
	f.StringVar(&status, "status", "", "Filter by task state")
	f.IntVar(&limit, "limit", 0, "Page size")
	f.StringVar(&pageToken, "page-token", "", "Pagination token")
	f.IntVar(&history, "history", 0, "Include up to n history messages per task")
	f.StringVar(&since, "since", "", "Only tasks with status updates after this timestamp (RFC 3339)")
	f.BoolVar(&withArtifacts, "with-artifacts", false, "Include artifacts in the response")

	return cmd
}
