#!/usr/bin/env bash
# Runs ON dev122 (root). Day-2 operations for the taboo deployment in this directory.
#
#   taboo-ctl.sh status                     当前镜像 / 版本 / 健康 / 最近发布记录
#   taboo-ctl.sh upgrade TAG                升级前自动备份 → 切换镜像 → 健康+版本校验（失败自动回滚）
#   taboo-ctl.sh rollback [TAG]             回到上一个（或指定）已发布镜像；仅切换镜像，不动数据库
#   taboo-ctl.sh history                    发布记录（releases.log）
#   taboo-ctl.sh backup [LABEL]             pg_dump + /data + .env 快照 → backups/<时间>-<LABEL>/，按标签保留
#   taboo-ctl.sh backups                    列出备份
#   taboo-ctl.sh restore-drill [BACKUP]     恢复演练：还原到临时库 taboo_restore_test，校验后删除临时库
#   taboo-ctl.sh restore BACKUP --yes       真实恢复：先做 pre-restore 备份，再用 BACKUP 覆盖 taboo 库
#   taboo-ctl.sh smoke                      用专用 smoke 账号跑 HTTPS 冒烟（运行时启用，结束即禁用）
#   taboo-ctl.sh user status|enable|disable EMAIL   本地账号启停（taboo-server user）
#   taboo-ctl.sh install-timers             安装 systemd 定时任务（每日备份、每周恢复演练）
#   taboo-ctl.sh reload                     修改 .env 后（如 TABOO_GITHUB_*）重建容器使之生效，并显示 GitHub 登录状态
#
# Only ever touches taboo's own container, image tags, database (taboo / taboo_restore_test)
# and this directory. Never prints secrets.
set -euo pipefail
cd "$(dirname "$(readlink -f "$0")")"
DIR="$PWD"
PG="${PG_CONTAINER:-swetcc-postgres}"
PUBLIC_URL="${TABOO_PUBLIC_URL:-https://taboo.swetcc.com}"
PUBLIC_IP="${TABOO_PUBLIC_IP:-10.0.30.122}"
DRILL_DB=taboo_restore_test
PG_CA="${TABOO_PG_CA:-/var/snap/docker/common/swetcc-dev/infra/tls/ca.crt}"
RELEASES="$DIR/releases.log"
umask 077
mkdir -p backups

log() { printf '%s %s\n' "$(date '+%F %T')" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }
envval() { sed -n "s/^$1=//p" "${2:-.env}" | tail -1; }
psql_admin() { docker exec -i "$PG" psql -v ON_ERROR_STOP=1 -U postgres -qtA "$@"; }
current_tag() { envval TABOO_IMAGE; }
healthz() { curl -fsS --max-time 5 "http://127.0.0.1:${TABOO_HTTP_PORT:-7100}/api/v1/healthz"; }
jget() { python3 -c 'import json,sys
try: print(json.load(sys.stdin).get(sys.argv[1], ""))
except Exception: print("")' "$1"; }

wait_healthy() {
  local s=""
  for _ in $(seq 1 60); do
    s="$(docker inspect -f '{{.State.Health.Status}}' taboo 2>/dev/null || true)"
    [ "$s" = healthy ] && return 0
    sleep 2
  done
  log "container health: ${s:-missing}"; return 1
}

switch_image() { # TAG
  docker image inspect "$1" >/dev/null 2>&1 || die "image $1 not found on this host"
  sed -i "s|^TABOO_IMAGE=.*|TABOO_IMAGE=$1|" .env
  ./init-db.sh >/dev/null
  docker compose up -d >/dev/null
  wait_healthy
}

record() { # ACTION TAG
  local h; h="$(healthz)"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$(date '+%F %T')" "$1" "$2" \
    "$(docker image inspect -f '{{.Id}}' "$2" | cut -c8-19)" "$(jget version <<<"$h")" "$(jget commit <<<"$h")" >> "$RELEASES"
}

counts() { # DB → "table<TAB>rows" for every table in public, sorted
  psql_admin -d "$1" -F $'\t' <<'SQL'
SELECT format('SELECT %L, count(*) FROM public.%I', tablename, tablename)
  FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename \gexec
SQL
}

prune() { # LABEL KEEP
  local old
  old="$(find backups -mindepth 1 -maxdepth 1 -type d -name "*-$1" | sort | head -n "-$2")"
  [ -n "$old" ] || return 0
  while read -r d; do log "pruning old backup $d"; rm -rf -- "$d"; done <<<"$old"
}

cmd_backup() { # [LABEL]
  local label="${1:-manual}" b keep
  [[ "$label" =~ ^[a-z0-9-]+$ ]] || die "label must match [a-z0-9-]+"
  b="backups/$(date +%Y%m%d-%H%M%S)-$label"
  mkdir -p "$b"
  log "backup → $DIR/$b"
  docker exec "$PG" pg_dump -U postgres -Fc taboo > "$b/taboo.dump"
  docker exec -i "$PG" pg_restore -l < "$b/taboo.dump" > "$b/toc.txt" || die "dump is not readable"
  if docker exec taboo true 2>/dev/null; then docker exec taboo tar czf - -C /data . > "$b/data.tar.gz"; else log "taboo container not running; /data skipped"; fi
  cp .env "$b/env"                      # master key + DB credentials needed to read this backup (0600)
  counts taboo > "$b/counts.tsv"
  { echo "image=$(current_tag)"; echo "image_id=$(docker image inspect -f '{{.Id}}' "$(current_tag)" 2>/dev/null)";
    echo "healthz=$(healthz 2>/dev/null || echo unavailable)"; echo "created=$(date -Is)"; } > "$b/meta.txt"
  ( cd "$b" && sha256sum taboo.dump data.tar.gz env 2>/dev/null > SHA256SUMS || true )
  case "$label" in daily) keep="${TABOO_BACKUP_KEEP_DAILY:-7}" ;; *) keep="${TABOO_BACKUP_KEEP:-5}" ;; esac
  prune "$label" "$keep"
  log "backup ok: $(du -sh "$b" | cut -f1), $(wc -l < "$b/toc.txt") TOC entries, $(wc -l < "$b/counts.tsv") tables"
  echo "$DIR/$b"
}

latest_backup() { find backups -mindepth 1 -maxdepth 1 -type d -name '2*' | sort | tail -1; }
resolve_backup() { local b="${1:-$(latest_backup)}"; [ -n "$b" ] || die "no backups yet"; b="${b%/}"; [ -d "$b" ] || b="backups/$b"; [ -f "$b/taboo.dump" ] || die "not a backup: $1"; echo "$b"; }

cmd_restore_drill() { # [BACKUP]
  local b tag url out rc=0
  b="$(resolve_backup "${1:-}")"; out="$b/restore-drill-$(date +%Y%m%d-%H%M%S).log"
  tag="$(current_tag)"
  log "restore drill of $b into temporary database $DRILL_DB (live taboo DB untouched)"
  ( cd "$b" && sha256sum -c --quiet SHA256SUMS ) || die "checksum mismatch in $b"
  drill_cleanup() { docker rm -f taboo-restore-drill >/dev/null 2>&1 || true; psql_admin -d postgres -c "DROP DATABASE IF EXISTS $DRILL_DB WITH (FORCE)" >/dev/null; }
  trap drill_cleanup EXIT
  drill_cleanup
  psql_admin -d postgres -c "CREATE DATABASE $DRILL_DB OWNER taboo_control" >/dev/null
  psql_admin -d "$DRILL_DB" -c "REVOKE ALL ON DATABASE $DRILL_DB FROM PUBLIC; GRANT CONNECT ON DATABASE $DRILL_DB TO taboo_control" >/dev/null
  docker exec -i "$PG" pg_restore -U postgres --exit-on-error -d "$DRILL_DB" < "$b/taboo.dump"
  {
    echo "# restore drill $(date -Is) backup=$b image=$tag"
    if diff <(counts "$DRILL_DB") "$b/counts.tsv" >/dev/null; then echo "row counts: MATCH ($(wc -l < "$b/counts.tsv") tables, $(awk -F'\t' '{s+=$2} END {print s}' "$b/counts.tsv") rows)"
    else echo "row counts: MISMATCH"; diff <(counts "$DRILL_DB") "$b/counts.tsv" || true; rc=1; fi
    url="$(envval TABOO_DATABASE_URL "$b/env" | sed "s#/taboo?#/$DRILL_DB?#")"
    # Decrypt every envelope with the master key from the backup set itself.
    if docker run --rm --network swetcc --env-file "$b/env" -e TABOO_DATABASE_URL="$url" -e TABOO_DATABASE_RUNTIME_ROLE=taboo_runtime \
         -v "$PG_CA:/etc/taboo/pg-ca.crt:ro" \
         --entrypoint taboo-migrate "$tag" verify; then echo "envelope verify: OK"; else echo "envelope verify: FAILED"; rc=1; fi
    docker run -d --name taboo-restore-drill --network swetcc --env-file "$b/env" -e TABOO_DATABASE_URL="$url" \
      -e TABOO_DATABASE_RUNTIME_ROLE=taboo_runtime -e TABOO_PORT=7100 -e TABOO_DATA_DIR=/tmp \
      -v "$PG_CA:/etc/taboo/pg-ca.crt:ro" -p 127.0.0.1:7198:7100 "$tag" >/dev/null
    ok=""; for _ in $(seq 1 30); do curl -fsS --max-time 3 http://127.0.0.1:7198/api/v1/readyz >/dev/null 2>&1 && { ok=1; break; }; sleep 1; done
    if [ -n "$ok" ]; then echo "restored server readyz: OK ($(curl -fsS http://127.0.0.1:7198/api/v1/healthz))"; else echo "restored server readyz: FAILED"; docker logs --tail 20 taboo-restore-drill 2>&1 | sed 's/^/  /'; rc=1; fi
    [ "$rc" = 0 ] && echo "RESULT: PASS" || echo "RESULT: FAIL"
  } 2>&1 | tee "$out"
  grep -q '^RESULT: PASS' "$out" || die "restore drill failed (see $DIR/$out)"
  log "drill log: $DIR/$out (temporary database dropped)"
}

cmd_restore() { # BACKUP --yes
  local b; b="$(resolve_backup "${1:-}")"
  [ "${2:-}" = --yes ] || die "this overwrites the live taboo database with $b; re-run with --yes"
  ( cd "$b" && sha256sum -c --quiet SHA256SUMS ) || die "checksum mismatch in $b"
  cmp -s <(envval TABOO_MASTER_KEY "$b/env") <(envval TABOO_MASTER_KEY) || die "backup was taken with a different master key; restore its env first"
  cmd_backup pre-restore >/dev/null
  log "restoring $b into taboo (taboo container stopped meanwhile)"
  docker compose stop taboo >/dev/null
  docker exec -i "$PG" pg_restore -U postgres --clean --if-exists --single-transaction --exit-on-error -d taboo < "$b/taboo.dump"
  docker compose up -d >/dev/null; wait_healthy || die "taboo unhealthy after restore"
  diff <(counts taboo) "$b/counts.tsv" >/dev/null && log "row counts match backup" || log "WARNING: row counts differ from backup (app may have written since start)"
  record restore "$(current_tag)"; log "restore done"
}

cmd_upgrade() { # TAG
  local tag="${1:?usage: upgrade TAG}" prev; prev="$(current_tag)"
  docker image inspect "$tag" >/dev/null 2>&1 || die "image $tag not found"
  [ -s "$RELEASES" ] || record baseline "$prev"
  cmd_backup pre-upgrade >/dev/null
  log "upgrade $prev → $tag"
  if switch_image "$tag"; then
    record upgrade "$tag"; log "now running: $(healthz)"
  else
    log "new image unhealthy; rolling back to $prev"; docker logs --tail 30 taboo >&2 || true
    switch_image "$prev" && record auto-rollback "$prev"; die "upgrade to $tag failed; rolled back to $prev"
  fi
}

cmd_rollback() { # [TAG]
  local cur target; cur="$(current_tag)"
  target="${1:-$(awk -F'\t' -v c="$cur" '$3 != c {t=$3} END {print t}' "$RELEASES" 2>/dev/null)}"
  [ -n "$target" ] || die "no previous release recorded; pass a TAG (see: docker images taboo)"
  log "rollback $cur → $target (image only; database unchanged — use restore for data)"
  switch_image "$target" || die "rollback target $target unhealthy"
  record rollback "$target"; log "now running: $(healthz)"
}

user_status() { # enable|disable EMAIL — via the server CLI; SQL fallback for images older than the `user` command
  if docker exec taboo taboo-server user "$1" "$2" >/dev/null 2>&1; then return 0; fi
  local st=active; [ "$1" = disable ] && st=disabled
  log "taboo-server user $1 unavailable in this image; using SQL fallback"
  psql_admin -d taboo -v email="$2" -v st="$st" <<'SQL' >/dev/null
UPDATE users SET status = :'st' WHERE lower(email) = lower(:'email') AND status <> :'st';
DELETE FROM refresh_tokens WHERE :'st' = 'disabled' AND user_id IN (SELECT id FROM users WHERE lower(email) = lower(:'email'));
DELETE FROM browser_sessions WHERE :'st' = 'disabled' AND user_id IN (SELECT id FROM users WHERE lower(email) = lower(:'email'));
SQL
}

cmd_user() { # status|enable|disable EMAIL
  docker exec taboo taboo-server user "$@"
}

cmd_smoke() {
  local creds="$DIR/smoke-account.json" email pw
  email="${TABOO_SMOKE_ACCOUNT:-smoke@taboo.swetcc.com}"
  if [ ! -f "$creds" ]; then
    pw="$(openssl rand -base64 30 | tr -d '/+=' | cut -c1-32)"
    printf '%s\n' "$pw" | docker exec -i taboo taboo-server user create "$email" --name "smoke (automation)" --disabled >/dev/null
    printf '{"email":"%s","password":"%s"}\n' "$email" "$pw" > "$creds"; unset pw
    log "created dedicated smoke account $email (credentials: $creds, 0600)"
  fi
  email="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["email"])' "$creds")"
  SMOKE_EMAIL="$email"   # global: the EXIT trap runs after this function's locals are gone
  smoke_off() { user_status disable "$SMOKE_EMAIL" && log "smoke account $SMOKE_EMAIL disabled again"; }
  trap smoke_off EXIT
  user_status enable "$email"
  docker run --rm --network host --add-host "${PUBLIC_URL#https://}:$PUBLIC_IP" \
    -v "$DIR/src/apps/server/scripts:/smoke:ro" -v "$creds:/run/smoke.json:ro" \
    -e TABOO_BASE="$PUBLIC_URL" -e TABOO_SMOKE_CREDENTIALS=/run/smoke.json \
    "${NODE_IMAGE:-node:24-bookworm}" node /smoke/smoke.js
}

cmd_install_timers() {
  local self="$DIR/taboo-ctl.sh"
  cat > /etc/systemd/system/taboo-backup.service <<UNIT
[Unit]
Description=taboo daily backup (pg_dump + /data + env snapshot)
After=docker.service snap.docker.dockerd.service
[Service]
Type=oneshot
ExecStart=$self backup daily
UNIT
  cat > /etc/systemd/system/taboo-backup.timer <<UNIT
[Unit]
Description=taboo daily backup at 03:30
[Timer]
OnCalendar=*-*-* 03:30:00
Persistent=true
RandomizedDelaySec=5min
[Install]
WantedBy=timers.target
UNIT
  cat > /etc/systemd/system/taboo-restore-drill.service <<UNIT
[Unit]
Description=taboo weekly restore drill of the latest backup (temporary database only)
After=docker.service snap.docker.dockerd.service
[Service]
Type=oneshot
ExecStart=$self restore-drill
UNIT
  cat > /etc/systemd/system/taboo-restore-drill.timer <<UNIT
[Unit]
Description=taboo weekly restore drill (Sunday 04:15)
[Timer]
OnCalendar=Sun *-*-* 04:15:00
Persistent=true
[Install]
WantedBy=timers.target
UNIT
  systemctl daemon-reload
  systemctl enable --now taboo-backup.timer taboo-restore-drill.timer >/dev/null
  systemctl list-timers --no-pager 'taboo-*'
}

github_state() {
  curl -fsS --max-time 5 "http://127.0.0.1:${TABOO_HTTP_PORT:-7100}/api/v1/auth/sources" 2>/dev/null \
    | grep -q '"github_enabled":true' && echo enabled || echo disabled
}

cmd_reload() {
  chmod 600 .env
  docker compose up -d >/dev/null
  if ! wait_healthy; then
    log "taboo failed to start with the current .env; last log lines (config errors never contain secret values):"
    docker logs --tail 15 taboo 2>&1 | sed 's/^/  /'
    die "fix .env and run reload again"
  fi
  echo "healthz: $(healthz)"; echo "github login: $(github_state)"
}

cmd_status() {
  echo "image:   $(current_tag)"; echo "healthz: $(healthz 2>/dev/null || echo unavailable)"
  echo "github login: $(github_state)"
  docker compose ps --format '{{.Name}}  {{.Status}}'
  echo "latest backup: $(latest_backup || true)"
  [ -s "$RELEASES" ] && { echo "recent releases:"; tail -5 "$RELEASES"; } || true
}

case "${1:-}" in
  status) cmd_status ;;
  upgrade) shift; cmd_upgrade "$@" ;;
  rollback) shift; cmd_rollback "$@" ;;
  history) column -t -s $'\t' "$RELEASES" 2>/dev/null || echo "no releases recorded" ;;
  backup) shift; cmd_backup "$@" ;;
  backups) ls -1 backups ;;
  restore-drill) shift; cmd_restore_drill "$@" ;;
  restore) shift; cmd_restore "$@" ;;
  smoke) cmd_smoke ;;
  user) shift; cmd_user "$@" ;;
  install-timers) cmd_install_timers ;;
  reload) cmd_reload ;;
  *) sed -n '2,17p' "$0"; exit 2 ;;
esac
