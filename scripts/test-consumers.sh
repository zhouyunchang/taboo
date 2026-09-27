#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../apps/server-go"
command -v node >/dev/null
export TABOO_TEST_CONSUMERS=1
go test -race ./internal/server -run '^TestConsumerProcesses$' -count=1 -timeout 300s
