#!/bin/sh
# Groovebox test entrypoint.
#   sh e2e/run.sh           # unit tests + isolated e2e (snapshot DB)
#   sh e2e/run.sh --live    # e2e against an already-running server (BASE_URL)
# Uses the global playwright install (no local npm deps — see e2e/README.md).
set -e
cd "$(dirname "$0")/.."

echo "== go unit tests =="
go test ./...

echo "== e2e (Playwright) =="
export NODE_PATH="$(npm root -g)"
node e2e/run.mjs "$@"