#!/bin/bash
# Runs a scripted A2A conversation against a real ACP agent through the
# executor/a2aacp executor: text turn, approved and denied tool calls,
# follow-up in the same context, cancel while input-required.
#
# Usage:
# > ./tools/acp_smoke.sh                      # opencode profile
# > ./tools/acp_smoke.sh claude               # any profile from a2aacp.Profiles()
# > ACP_CMD="cloudcode acp" ./tools/acp_smoke.sh
#
# Set ACP_VERBOSE=1 to print the agent's stderr. The workspace is a temporary
# directory, removed on exit unless ACP_KEEP=1.

set -euo pipefail

cd "$(dirname "$0")/.."

PROFILE="${1:-opencode}"
WORKSPACE="$(mktemp -d -t a2aacp-smoke-XXXXXX)"
if [[ "${ACP_KEEP:-0}" != "1" ]]; then
  trap 'rm -rf "$WORKSPACE"' EXIT
fi
echo "workspace: $WORKSPACE"

cat > "$WORKSPACE/AGENTS.md" <<'EOF'
You are being tested by an automated script. Follow instructions literally,
keep answers short, and use the bash tool when asked to run a command.
EOF

# opencode and its forks auto-approve shell commands by default. The test
# needs the agent to ask, so the permission requests reach the A2A caller.
export OPENCODE_PERMISSION='{"bash":"ask","edit":"ask"}'
export CLOUDCODE_PERMISSION="$OPENCODE_PERMISSION"

ARGS=(-dir "$WORKSPACE")
if [[ -n "${ACP_CMD:-}" ]]; then
  ARGS+=(-cmd "$ACP_CMD")
else
  ARGS+=(-profile "$PROFILE")
fi
if [[ "${ACP_VERBOSE:-0}" == "1" ]]; then
  ARGS+=(-v)
fi

go run ./tools/acpsmoke "${ARGS[@]}"
