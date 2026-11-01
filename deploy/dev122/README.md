# taboo on dev122 (`https://taboo-dev122.swetcc.com`)

Dev/staging deployment that reuses the shared dev122 infrastructure
(`/var/snap/docker/common/swetcc-dev`): the `swetcc` docker network, the shared
PostgreSQL 18 (`swetcc-postgres`, TLS only) and Nginx Proxy Manager (Let's Encrypt via
Aliyun DNS-01). **No secret is stored in this repository.**

| Item | Value |
| --- | --- |
| Deploy dir on dev122 | `/var/snap/docker/common/swetcc-dev/taboo` (0700) |
| Secrets | `<deploy dir>/.env` (0600, generated on first deploy) |
| Bootstrap owner | `<deploy dir>/bootstrap-owner.json` (0600) |
| Smoke account | `smoke@taboo.swetcc.com`, credentials `<deploy dir>/smoke-account.json` (0600); **disabled** except while `taboo-ctl.sh smoke` runs |
| Backups | `<deploy dir>/backups/<YYYYmmdd-HHMMSS>-<label>/` (0700): `taboo.dump`, `data.tar.gz`, `env` (0600), `counts.tsv`, `meta.txt`, `SHA256SUMS` |
| Release log | `<deploy dir>/releases.log` (time, action, tag, image id, version, commit) |
| Timers | `taboo-backup.timer` daily 03:30 (keep 7), `taboo-restore-drill.timer` Sun 04:15 (dev122 local time, CST) |
| Container / alias | `taboo` on network `swetcc`, host bind `127.0.0.1:7100` |
| Data volume | `taboo_data` (`/data`, local state only; system of record is PostgreSQL) |
| Database | `taboo` on `swetcc-postgres`, owner `taboo_control` (LOGIN, NOSUPERUSER, NOBYPASSRLS); runtime role `taboo_runtime` (NOLOGIN) granted to it; FORCE RLS |
| DB TLS | host `swetcc-postgres` (not the ambiguous `postgres` alias, which `swetcc-powerdata-postgres` also holds), `sslmode=verify-full`, CA `infra/tls/ca.crt` mounted at `/etc/taboo/pg-ca.crt` (server SAN: `postgres`, `swetcc-postgres`) |
| Public URL | NPM proxy host `taboo-dev122.swetcc.com` → `http://taboo:7100`, force SSL, HTTP/2, HSTS |

## Files

- `compose.yaml` – the service (image tag comes from `TABOO_IMAGE` in `.env`).
- `env.example` – shape of `.env`; real values are random and only on dev122.
- `deploy.sh` – run from a developer checkout: ships sources (`git ls-files`) to dev122,
  builds the linux/amd64 image natively there (`taboo:dev-<sha>`, version/commit/build time
  baked in via `--build-arg` → ldflags + OCI labels), syncs these files, creates `.env` on the
  first run, then `taboo-ctl.sh upgrade` and checks that `/api/v1/healthz` reports the commit.
  `--image TAG` switches to an existing tag; `--rollback [TAG]` rolls back.
- `taboo-ctl.sh` – day-2 operations on dev122: `status`, `upgrade`, `rollback`, `history`,
  `backup`, `backups`, `restore-drill`, `restore`, `smoke`, `user`, `install-timers`.
- `init-db.sh` – idempotent DB/role setup on `swetcc-postgres` (admin via container socket).
- `bootstrap-owner.sh` – one-shot: registers the first owner with a random password into
  `bootstrap-owner.json` (0600), then sets `TABOO_DISABLE_REGISTER=true` and recreates.
- `npm-proxy.py` – idempotent NPM API client: LE certificate (DNS-01 aliyun, 300 s
  propagation, ECDSA) + proxy host. Needs `NPM_DNS_CREDENTIALS=<certbot-dns-aliyun ini>`
  only when the certificate does not exist yet.

## Usage

```bash
deploy/dev122/deploy.sh                                   # build + (re)deploy current checkout
ssh dev122-root /var/snap/docker/common/swetcc-dev/taboo/bootstrap-owner.sh   # first time only
ssh dev122-root 'cd /var/snap/docker/common/swetcc-dev/taboo && NPM_DNS_CREDENTIALS=/path/aliyun.ini ./npm-proxy.py'
```

Health: `curl -fsS https://taboo-dev122.swetcc.com/api/v1/readyz`; self-check:
`docker exec taboo taboo-server --check`.

Name resolution: like every other `*-dev122.swetcc.com` host there is no DNS record; add
`10.0.30.122 taboo-dev122.swetcc.com` to `/etc/hosts` (dev122 already has it).

## Upgrade / rollback

```bash
deploy/dev122/deploy.sh                    # one command: build + pre-upgrade backup + switch + health/version check
deploy/dev122/deploy.sh --rollback         # back to the previous release (image only)
deploy/dev122/deploy.sh --rollback taboo:dev-abc1234
ssh dev122-root /var/snap/docker/common/swetcc-dev/taboo/taboo-ctl.sh status
ssh dev122-root /var/snap/docker/common/swetcc-dev/taboo/taboo-ctl.sh smoke   # HTTPS smoke, 21 checks after Phase 5
```

`upgrade` takes a `pre-upgrade` backup first (keep 5) and automatically switches back if the
new image never becomes healthy. Rollback only swaps the image: schema migrations are
forward-only, so if the newer release migrated the schema, roll the data back with
`restore <pre-upgrade backup> --yes` as well.

## Backup / restore

The encryption root is `TABOO_MASTER_KEY` in `.env`; a database dump is useless without it,
so every backup set carries an `env` snapshot (0600). Backups stay on dev122 — copy them off
the host if dev122 itself must be survivable.

```bash
T=/var/snap/docker/common/swetcc-dev/taboo/taboo-ctl.sh
ssh dev122-root $T backup                    # manual backup (label "manual", keep 5)
ssh dev122-root $T backups                   # list
ssh dev122-root $T restore-drill             # latest backup → temp DB taboo_restore_test → verify → drop
ssh dev122-root $T restore 20261031-153000-pre-upgrade --yes   # overwrite live taboo DB (takes a pre-restore backup first)
ssh dev122-root $T install-timers            # (re)install systemd timers
```

The drill checks SHA256SUMS, restores into `taboo_restore_test`, compares per-table row
counts with `counts.tsv`, decrypts every envelope with `taboo-migrate verify` using the
backup's own master key, starts a throwaway server on `127.0.0.1:7198` against it and
waits for `readyz`, then drops the temporary database. A log is written next to the backup.

Never run `docker compose down -v` in `infra/` – the PostgreSQL volume is shared by other
services. `docker compose down` here only touches the `taboo` container; `taboo_data`
holds no system-of-record data in PG mode.

## Accounts

Registration is disabled. Manage local accounts with
`docker exec -i taboo taboo-server user create EMAIL [--name N] [--disabled]` (password on
stdin), `taboo-server user status|enable|disable EMAIL`. Disabling revokes refresh tokens and
browser sessions; every change is audited as `admin.user.*`.

## Sign in with GitHub (optional)

Disabled until the operator adds credentials. Create a GitHub **OAuth App** (not a GitHub App):

| Field | Value |
| --- | --- |
| Homepage URL | `https://taboo-dev122.swetcc.com` |
| Authorization callback URL | `https://taboo-dev122.swetcc.com/api/v1/auth/github/callback` |
| Enable Device Flow | off |

Then on dev122 (never paste the secret anywhere else):

```sh
ssh dev122-root
cd /var/snap/docker/common/swetcc-dev/taboo
vi .env      # append TABOO_GITHUB_CLIENT_ID=..., TABOO_GITHUB_CLIENT_SECRET=..., and one policy, e.g. TABOO_GITHUB_ALLOWED_USERS=alice
./taboo-ctl.sh reload   # recreates the container, prints "github login: enabled"
```

All variables (policy, auto-create, join org) are documented in `env.example`. The server
refuses to start if credentials are set without an access policy (fail closed); `reload` then
prints the error line. To disable again, comment the two credential lines and `reload`.
