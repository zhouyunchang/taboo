#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Public pre-federation baseline, kept fixed so upgrades remain reproducible.
baseline=d5724d32f4d5828ee89734be6e6223a992ec74b8
legacy=$(mktemp -d)
trap 'rm -rf "$legacy"' EXIT
if ! git cat-file -e "$baseline^{commit}" 2>/dev/null; then git fetch origin "$baseline"; fi
git archive "$baseline" | tar -x -C "$legacy"
docker build -f "$legacy/apps/server-go/Dockerfile" -t taboo:public-legacy-test "$legacy"
TABOO_TEST_LEGACY_IMAGE=taboo:public-legacy-test node scripts/test-container-recovery.mjs
