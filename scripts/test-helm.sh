#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
helm() { docker run --rm -v "$PWD:/workspace:ro" -w /workspace alpine/helm:3.17.3 "$@"; }
helm lint deploy/helm/taboo --set existingSecret=fixture-root
helm template fixture deploy/helm/taboo --set existingSecret=fixture-root >/dev/null
helm template fixture deploy/helm/taboo --set existingSecret=fixture-root --set database.mode=postgres --set database.existingSecret=fixture-db --set replicaCount=2 --set persistence.accessMode=ReadWriteMany --set federation.publicOrigin=https://kms.example.test --set federation.issuerAllowlist[0]=https://issuer.example.test --set federation.bootstrapSecret=fixture-oidc >/dev/null
if helm template fixture deploy/helm/taboo --set existingSecret=fixture-root --set replicaCount=2 >/dev/null 2>&1; then echo 'SQLite multi-replica unexpectedly accepted'; exit 1; fi
if helm template fixture deploy/helm/taboo --set existingSecret=fixture-root --set database.mode=postgres --set database.existingSecret=fixture-db --set replicaCount=2 >/dev/null 2>&1; then echo 'Unshared export directory unexpectedly accepted'; exit 1; fi
if helm template fixture deploy/helm/taboo --set existingSecret=fixture-root --set database.mode=postgres >/dev/null 2>&1; then echo 'Missing database credential unexpectedly accepted'; exit 1; fi
echo 'Helm SQLite/PostgreSQL/federation render and deployment guard checks passed.'
