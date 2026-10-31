#!/usr/bin/env bash
# Runs ON dev122 once, while registration is still enabled. Creates the first owner
# with a random password, stores it in ./bootstrap-owner.json (0600), then disables
# self-registration and recreates the container. Refuses to overwrite an existing file.
set -euo pipefail
cd "$(dirname "$0")"
out=bootstrap-owner.json
[ -e "$out" ] && { echo "$out already exists; owner was bootstrapped before"; exit 0; }
base="http://127.0.0.1:${TABOO_HTTP_PORT:-7100}/api/v1"
email="${TABOO_OWNER_EMAIL:-owner@taboo.swetcc.com}"
pw="$(openssl rand -base64 30 | tr -d '/+=' | cut -c1-32)"
umask 077
printf '{"email":"%s","password":"%s","name":"taboo owner","url":"https://taboo.swetcc.com"}\n' "$email" "$pw" > "$out"
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  --data-binary @- "$base/auth/register" <<JSON
{"email":"$email","password":"$pw","name":"taboo owner"}
JSON
)
if [ "$code" != 201 ] && [ "$code" != 200 ]; then rm -f "$out"; echo "register failed: HTTP $code"; exit 1; fi
echo "owner created; credentials stored in $(pwd)/$out"
sed -i 's/^TABOO_DISABLE_REGISTER=.*/TABOO_DISABLE_REGISTER=true/' .env
docker compose up -d --force-recreate
