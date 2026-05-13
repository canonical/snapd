#!/usr/bin/env bash
# Safe check for GITHUB_TOKEN environment variable.
# Exits 0 if set and non-empty, exits 1 with a safe message if missing.
# The token value is NEVER printed.

set -euo pipefail

if [ -z "${GITHUB_TOKEN:-}" ]; then
    echo "error: GITHUB_TOKEN environment variable is not set" >&2
    echo "hint: export GITHUB_TOKEN=<your-token>" >&2
    exit 1
fi

echo "ok: GITHUB_TOKEN is set"
exit 0
