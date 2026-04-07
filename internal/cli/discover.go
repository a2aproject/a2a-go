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
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
)

func newDiscoverCmd(cfg *globalConfig) *cobra.Command {
	var extended bool

	cmd := &cobra.Command{
		Use:   "discover <url>",
		Short: "Fetch and display an agent card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(cmd.Context(), cfg, args[0], extended)
		},
	}

	cmd.Flags().BoolVar(&extended, "extended", false, "Fetch the extended agent card")
	return cmd
}

func runDiscover(ctx context.Context, cfg *globalConfig, agentURL string, extended bool) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	var card *a2a.AgentCard

	if extended {
		ctx = withServiceParams(ctx, cfg)
		client, err := newClient(ctx, cfg, agentURL)
		if err != nil {
			return fmt.Errorf("failed to create a client: %w", err)
		}
		defer func() {
			if err := client.Destroy(); err != nil {
				cfg.logf("failed to destroy client: %v", err)
			}
		}()

		card, err = client.GetExtendedAgentCard(ctx, &a2a.GetExtendedAgentCardRequest{
			Tenant: cfg.tenant,
		})
		if err != nil {
			return fmt.Errorf("failed to get extended agent card: %w", err)
		}
	} else {
		var resolveOpts []agentcard.ResolveOption
		if cfg.auth != "" {
			resolveOpts = append(resolveOpts, agentcard.WithRequestHeader("Authorization", cfg.auth))
		}
		cfg.logf("fetching agent card from %s", agentURL)

		var err error
		card, err = agentcard.DefaultResolver.Resolve(ctx, agentURL, resolveOpts...)
		if err != nil {
			return fmt.Errorf("failed to resolve agent card: %w", err)
		}
	}

	if err := cfg.printCard(card); err != nil {
		return fmt.Errorf("failed to print agent card: %w", err)
	}
	return nil
}
