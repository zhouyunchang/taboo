#!/usr/bin/env bash
# Runs on the developer machine. Ships the current checkout to dev122, builds the
# linux/amd64 image natively there, syncs these deploy files, generates secrets on the
# first run (only on dev122, mode 0600), prepares the database and (re)starts taboo.
#   deploy/dev122/deploy.sh            # build + deploy
#   SKIP_BUILD=1 TABOO_IMAGE=taboo:dev-abc1234 deploy/dev122/deploy.sh
set -euo pipefail
root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"; cd "$root"
HOST="${DEV122_SSH:-dev122-root}"
DIR="${TABOO_DEPLOY_DIR:-/var/snap/docker/common/swetcc-dev/taboo}"
sha="$(git rev-parse --short=7 HEAD)"
git diff --quiet HEAD -- apps api package.json package-lock.json || sha="$sha-dirty"
TAG="${TABOO_IMAGE:-taboo:dev-$sha}"

ssh "$HOST" "mkdir -p '$DIR' && chmod 700 '$DIR'"
if [ "${SKIP_BUILD:-0}" != 1 ]; then
  echo "==> shipping sources and building $TAG on $HOST (linux/amd64)"
  git ls-files -co --exclude-standard -- apps api package.json package-lock.json \
    | grep -v -E '(^|/)node_modules/' | COPYFILE_DISABLE=1 tar --no-xattrs -cf - -T - \
    | ssh "$HOST" "rm -rf '$DIR/src' && mkdir -p '$DIR/src' && tar -xf - -C '$DIR/src'"
  ssh "$HOST" "docker build --pull=false -f '$DIR/src/apps/server-go/Dockerfile' -t '$TAG' \
    --label org.opencontainers.image.revision=$sha '$DIR/src'"
fi

echo "==> syncing deploy files"
COPYFILE_DISABLE=1 tar --no-xattrs -cf - -C deploy/dev122 compose.yaml env.example init-db.sh bootstrap-owner.sh npm-proxy.py README.md \
  | ssh "$HOST" "tar -xf - -C '$DIR'"

ssh "$HOST" TAG="$TAG" DIR="$DIR" bash -s <<'REMOTE'
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
sed -i "s|^TABOO_IMAGE=.*|TABOO_IMAGE=$TAG|" .env
./init-db.sh
docker compose up -d
for i in $(seq 1 60); do
  s="$(docker inspect -f '{{.State.Health.Status}}' taboo 2>/dev/null || true)"
  [ "$s" = healthy ] && break
  sleep 2
done
docker compose ps
curl -fsS "http://127.0.0.1:7100/api/v1/readyz"; echo
REMOTE
