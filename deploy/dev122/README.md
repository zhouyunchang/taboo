# taboo on dev122 (`https://taboo.swetcc.com`)

Dev/staging deployment that reuses the shared dev122 infrastructure
(`/var/snap/docker/common/swetcc-dev`): the `swetcc` docker network, the shared
PostgreSQL 18 (`swetcc-postgres`, TLS only) and Nginx Proxy Manager (Let's Encrypt via
Aliyun DNS-01). **No secret is stored in this repository.**

| Item | Value |
| --- | --- |
| Deploy dir on dev122 | `/var/snap/docker/common/swetcc-dev/taboo` (0700) |
| Secrets | `<deploy dir>/.env` (0600, generated on first deploy) |
| Bootstrap owner | `<deploy dir>/bootstrap-owner.json` (0600) |
| Container / alias | `taboo` on network `swetcc`, host bind `127.0.0.1:7100` |
| Data volume | `taboo_data` (`/data`, local state only; system of record is PostgreSQL) |
| Database | `taboo` on `swetcc-postgres`, owner `taboo_control` (LOGIN, NOSUPERUSER, NOBYPASSRLS); runtime role `taboo_runtime` (NOLOGIN) granted to it; FORCE RLS |
| DB TLS | host `swetcc-postgres` (not the ambiguous `postgres` alias, which `swetcc-powerdata-postgres` also holds), `sslmode=verify-full`, CA `infra/tls/ca.crt` mounted at `/etc/taboo/pg-ca.crt` (server SAN: `postgres`, `swetcc-postgres`) |
| Public URL | NPM proxy host `taboo.swetcc.com` → `http://taboo:7100`, force SSL, HTTP/2, HSTS |

## Files

- `compose.yaml` – the service (image tag comes from `TABOO_IMAGE` in `.env`).
- `env.example` – shape of `.env`; real values are random and only on dev122.
- `deploy.sh` – run from a developer checkout: ships sources (`git ls-files`) to dev122,
  builds the linux/amd64 image natively there (`taboo:dev-<sha>`), syncs these files,
  creates `.env` on first run, runs `init-db.sh`, `docker compose up -d`, waits for health.
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

Health: `curl -fsS https://taboo.swetcc.com/api/v1/readyz`; self-check:
`docker exec taboo taboo-server --check`.

DNS: public record `taboo.swetcc.com A 10.0.30.122` (Aliyun). Until it exists, use a hosts
override (`10.0.30.122 taboo.swetcc.com`); dev122 itself has one in `/etc/hosts`.

## Backup / restore

The encryption root is `TABOO_MASTER_KEY` in `.env`; a database dump is useless without it.

```bash
docker exec swetcc-postgres pg_dump -U postgres -Fc taboo > taboo-$(date +%F).dump   # on dev122
```

Keep dump and `.env` together (both 0600). Never run `docker compose down -v` in
`infra/` – the PostgreSQL volume is shared by other services. `docker compose down` here
only touches the `taboo` container; `taboo_data` holds no system-of-record data in PG mode.

## Rollback

Set `TABOO_IMAGE` in `.env` to a previous `taboo:dev-<sha>` tag and `docker compose up -d`.
Schema migrations are forward-only; take a `pg_dump` before deploying a new schema version.
