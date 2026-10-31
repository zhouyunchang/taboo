#!/usr/bin/env bash
# Runs on the developer machine. Ships the current checkout to dev122, builds the
# linux/amd64 image natively there, syncs these deploy files, generates secrets on the
# first run (only on dev122, mode 0600), prepares the database and (re)starts taboo.
# Later runs go through taboo-ctl.sh upgrade: pre-upgrade backup → switch image →
# health + version check → automatic rollback if the new image never becomes healthy.
#   deploy/dev122/deploy.sh            # build + upgrade (one command)
#   deploy/dev122/deploy.sh --image taboo:dev-abc1234   # switch to an image already on dev122
#   deploy/dev122/deploy.sh --rollback [TAG]            # one-command rollback (previous release by default)
set -euo pipefail
case "${1:-}" in
  --image) [ -n "${2:-}" ] || { echo "usage: deploy.sh --image TAG"; exit 2; }; export SKIP_BUILD=1 TABOO_IMAGE="$2" ;;
  --rollback) exec ssh "${DEV122_SSH:-dev122-root}" "${TABOO_DEPLOY_DIR:-/var/snap/docker/common/swetcc-dev/taboo}/taboo-ctl.sh" rollback ${2:+"$2"} ;;
  "") ;;
  *) echo "usage: deploy.sh [--image TAG | --rollback [TAG]]"; exit 2 ;;
esac
root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"; cd "$root"
HOST="${DEV122_SSH:-dev122-root}"
DIR="${TABOO_DEPLOY_DIR:-/var/snap/docker/common/swetcc-dev/taboo}"
sha="$(git rev-parse --short=7 HEAD)"
git diff --quiet HEAD -- apps api package.json package-lock.json || sha="$sha-dirty"
TAG="${TABOO_IMAGE:-taboo:dev-$sha}"
version="$(node -p "require('./package.json').version" 2>/dev/null || sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' package.json | head -1)"
built="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ssh "$HOST" "mkdir -p '$DIR' && chmod 700 '$DIR'"
if [ "${SKIP_BUILD:-0}" != 1 ]; then
  echo "==> shipping sources and building $TAG on $HOST (linux/amd64)"
  git ls-files -co --exclude-standard -- apps api package.json package-lock.json \
    | grep -v -E '(^|/)node_modules/' | COPYFILE_DISABLE=1 tar --no-xattrs -cf - -T - \
    | ssh "$HOST" "rm -rf '$DIR/src' && mkdir -p '$DIR/src' && tar -xf - -C '$DIR/src'"
  ssh "$HOST" "docker build --pull=false -f '$DIR/src/apps/server-go/Dockerfile' -t '$TAG' \
    --build-arg TABOO_VERSION='$version' --build-arg TABOO_COMMIT='$sha' --build-arg BUILD_TIME='$built' '$DIR/src'"
  ssh "$HOST" "docker run --rm '$TAG' --version"
fi

echo "==> syncing deploy files"
COPYFILE_DISABLE=1 tar --no-xattrs -cf - -C deploy/dev122 compose.yaml env.example init-db.sh bootstrap-owner.sh npm-proxy.py taboo-ctl.sh README.md \
  | ssh "$HOST" "tar -xf - -C '$DIR'"

ssh "$HOST" TAG="$TAG" DIR="$DIR" SHA="$([ "${SKIP_BUILD:-0}" = 1 ] || echo "$sha")" bash -s <<'REMOTE'
set -euo pipefail
cd "$DIR"
if [ ! -f .env ]; then
  echo "==> first run: generating fresh secrets into $DIR/.env (0600)"
  umask 077
  pw="$(openssl rand -hex 32)"
  cat > .env <<ENV
TABOO_IMAGE=$TAG
TABOO_HTTP_PORT=7100
TABOO_PUBLIC_ORIGIN=https://taboo.swetcc.com
TABOO_CORS_ORIGIN=https://taboo.swetcc.com
TABOO_DISABLE_REGISTER=false
TABOO_MASTER_KEY=$(openssl rand -hex 32)
TABOO_JWT_SECRET=$(openssl rand -hex 32)
TABOO_CONTROL_PASSWORD=$pw
TABOO_DATABASE_URL=postgres://taboo_control:$pw@swetcc-postgres:5432/taboo?sslmode=verify-full&sslrootcert=/etc/taboo/pg-ca.crt
ENV
fi
chmod 600 .env
if docker inspect taboo >/dev/null 2>&1; then
  ./taboo-ctl.sh upgrade "$TAG"
else
  sed -i "s|^TABOO_IMAGE=.*|TABOO_IMAGE=$TAG|" .env
  ./init-db.sh
  docker compose up -d
  for i in $(seq 1 60); do
    [ "$(docker inspect -f '{{.State.Health.Status}}' taboo 2>/dev/null || true)" = healthy ] && break
    sleep 2
  done
fi
docker compose ps
h="$(curl -fsS "http://127.0.0.1:7100/api/v1/healthz")"; echo "$h"
if [ -n "$SHA" ]; then case "$h" in *"\"commit\":\"$SHA\""*) echo "==> version check ok ($SHA)";; *) echo "==> WARNING: running commit differs from $SHA"; exit 1;; esac; fi
REMOTE
