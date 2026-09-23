# Release Flow

> **Staleness warning**: if anything described here contradicts observed behavior, re-read
> the source code / workflow files. This document may be out of date.

This document describes how versioned releases of the `a2a-go` library are produced.

> **Note**: the `a2a` CLI was extracted into its own repository,
> [a2aproject/a2a-cli](https://github.com/a2aproject/a2a-cli). Downloadable CLI
> binaries (GoReleaser cross-compilation + archives) are produced there, not
> here. This doc now covers only the library release.

## Overview

Releases are driven from `.github/workflows/release-please.yaml`:

```
push to main
  -> release-please job (googleapis/release-please-action)
       - maintains a "release PR" that bumps the version + CHANGELOG
       - when that PR merges: creates the git tag (vX.Y.Z) and the GitHub Release
         (release notes come from CHANGELOG.md)
       - emits outputs: release_created, tag_name
```

release-please owns the tag, the release, and the release notes. Consumers pull
the tagged module via `go get github.com/a2aproject/a2a-go/v2@vX.Y.Z`.

## Local validation

- `actionlint .github/workflows/release-please.yaml` — lint the workflow.

## Gotchas

- The Release is created by release-please using `secrets.A2A_BOT_PAT`.
- Tags are `vX.Y.Z`.
- Pin action SHAs (repo convention); the version comment must match the SHA.
- `release-type: go` only updates `CHANGELOG.md` + `.release-please-manifest.json`
  — Go has no version file to bump, and we don't need one (the version flows from
  the git tag; module consumers resolve it via the Go module proxy / build info).
