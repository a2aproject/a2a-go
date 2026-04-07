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

func newSubscribeCmd(cfg *globalConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "subscribe <url> <task-id>",
		Short: "Subscribe to task events",
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

			cfg.logf("subscribing to task %s", args[1])

			for event, err := range client.SubscribeToTask(ctx, &a2a.SubscribeToTaskRequest{
				ID:     a2a.TaskID(args[1]),
				Tenant: cfg.tenant,
			}) {
				if err != nil {
					return fmt.Errorf("subscription error: %w", err)
				}
				if err := cfg.printEvent(event); err != nil {
					return fmt.Errorf("failed to print event: %w", err)
				}
			}
			return nil
		},
	}
}
