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
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Build metadata. These defaults are overwritten at release time using
// ldflags specified in .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type versionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

func buildVersionInfo() versionInfo {
	v := version
	if v == "dev" { // go install produces binaries without ldflags
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			v = bi.Main.Version
		}
	}
	return versionInfo{
		Version:   v,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func newVersionCmd(cfg *globalConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := buildVersionInfo()
			if cfg.output == "json" {
				return printJSON(cfg.out, info)
			}
			_, err := fmt.Fprintf(cfg.out,
				"a2a %s\ncommit:  %s\nbuilt:   %s\ngo:      %s\nos/arch: %s\n",
				info.Version, info.Commit, info.Date, info.GoVersion, info.Platform)
			return err
		},
	}
}
