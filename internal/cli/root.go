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

// Package cli implements the a2a command-line interface.
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type globalConfig struct {
	out          io.Writer
	output       string
	transport    string
	svcParams    []string
	auth         string
	tenant       string
	timeout      time.Duration
	verbose      bool
	insecureGRPC bool
}

func (g *globalConfig) logf(format string, args ...any) {
	if g.verbose {
		_, _ = fmt.Fprintf(os.Stderr, "# "+format+"\n", args...)
	}
}

// Execute runs the CLI and returns the exit code.
func Execute() int {
	cfg := &globalConfig{}
	root := newRootCmd(cfg, os.Stdout)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd(cfg *globalConfig, out io.Writer) *cobra.Command {
	cfg.out = out

	cmd := &cobra.Command{
		Use:           "a2a",
		Short:         "CLI for the Agent-to-Agent protocol",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&cfg.output, "output", "o", "text", "Output format: text, json")
	pf.StringVar(&cfg.transport, "transport", "", "Force transport: rest, jsonrpc, grpc")
	pf.StringArrayVar(&cfg.svcParams, "svc-param", nil, "Service parameter k=v (repeatable)")
	pf.StringVar(&cfg.auth, "auth", "", "Shorthand for --svc-param Authorization=<creds>")
	pf.StringVar(&cfg.tenant, "tenant", "", "Tenant identifier")
	pf.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Request timeout")
	pf.BoolVarP(&cfg.verbose, "verbose", "v", false, "Verbose output to stderr")
	pf.BoolVar(&cfg.insecureGRPC, "insecure", false, "Use insecure (plaintext) gRPC transport credentials")

	cmd.AddCommand(
		newDiscoverCmd(cfg),
		newSendCmd(cfg),
		newGetCmd(cfg),
		newListCmd(cfg),
		newCancelCmd(cfg),
		newSubscribeCmd(cfg),
		newServeCmd(cfg),
	)

	return cmd
}
