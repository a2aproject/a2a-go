# Release Flow

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code / workflow files. This document may be out of date.

This document describes how versioned releases and downloadable CLI binaries are produced.

## Overview

Two tools cooperate, both driven from `.github/workflows/release-please.yaml`:

```
push to main
  -> release-please job (googleapis/release-please-action)
       - maintains a "release PR" that bumps the version + CHANGELOG
       - when that PR merges: creates the git tag (vX.Y.Z) and the GitHub Release
         (release notes come from CHANGELOG.md)
       - emits outputs: release_created, tag_name
  -> goreleaser job (runs only if release_created == 'true')
       - checks out the tag_name ref (fetch-depth: 0 so the tag is visible)
       - runs `goreleaser release --clean` (config: .goreleaser.yaml)
       - cross-compiles cmd/a2a, builds archives + checksums.txt
       - uploads the artifacts to the EXISTING GitHub Release
```

Key point: release-please owns the tag, the release, and the release notes.
GoReleaser only attaches binaries. This is why `.goreleaser.yaml` sets
`release.mode: keep-existing` (do not touch the release body) and
`changelog.disable: true` (do not generate its own changelog).

## Version injection

`internal/cli/version.go` declares build-metadata vars (`version`, `commit`,
`date`), defaulting to dev values. GoReleaser overwrites them at link time via
`-ldflags -X github.com/a2aproject/a2a-go/v2/internal/cli.<var>=...`. Because the
`main` package (`cmd/a2a`) just delegates to `internal/cli`, the ldflags target
the `internal/cli` package path, NOT `main`.

For `go install <module>@<version>` builds (no ldflags), `buildVersionInfo()`
falls back to `runtime/debug.ReadBuildInfo()` for the module version.

Surfaced via `a2a version` (text/`-o json`) and the cobra-provided `--version`.

## Build matrix

goos: linux, darwin, windows × goarch: amd64, arm64 (6 archives).
Unix archives are `tar.gz`; Windows is `zip` (`archives.format_overrides`).

## Local validation

- `goreleaser check` — validate `.goreleaser.yaml`.
- `goreleaser release --snapshot --clean --skip=publish` — full dry-run build into `dist/`.
- `actionlint .github/workflows/release-please.yaml` — lint the workflow.

`dist/` is gitignored.

## Gotchas

- The `goreleaser` job uses the default `GITHUB_TOKEN` (needs `contents: write`)
  to upload assets. The Release itself is created by release-please using
  `secrets.A2A_BOT_PAT`.
- Tags are `vX.Y.Z`; GoReleaser strips the `v` for `.Version` (e.g. `2.5.0`).
- Pin action SHAs (repo convention); the version comment must match the SHA.
- The GoReleaser *CLI* cannot be pinned to a commit SHA (the action installs
  released binaries); it is pinned to an exact release tag via the action's
  `version:` input (e.g. `v2.17.1`). Bump it in lockstep with the config schema.
- `release-type: go` only updates `CHANGELOG.md` + `.release-please-manifest.json`
  — Go has no version file to bump, and we don't need one (the version flows from
  the git tag into the binary via ldflags, and from build info for `go install`).
