#!/usr/bin/env python3
"""Runs ON dev122. Ensures a Let's Encrypt (DNS-01, Aliyun) certificate and an NPM
proxy host for taboo-dev122.swetcc.com -> taboo:7100 via the NPM REST API.

Inputs (never printed): NPM admin JSON (email/password) and a certbot-dns-aliyun
credential file with the same content existing dev122 certificates use.
Idempotent: reuses an existing certificate / proxy host for the domain."""
import json, os, sys, urllib.request, urllib.error

DOMAIN = os.environ.get('TABOO_DOMAIN', 'taboo-dev122.swetcc.com')
API = os.environ.get('NPM_API', 'http://10.0.30.122:81/api')
ADMIN = os.environ.get('NPM_ADMIN_JSON', '/var/snap/docker/common/swetcc-dev/infra/npm/admin.json')
DNS_CREDS = os.environ.get('NPM_DNS_CREDENTIALS')  # path to dns_aliyun ini
token = None

def api(method, path, data=None, timeout=60):
    headers = {'Content-Type': 'application/json'}
    if token: headers['Authorization'] = 'Bearer ' + token
    body = None if data is None else json.dumps(data).encode()
    req = urllib.request.Request(API + path, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raise SystemExit(f'NPM {method} {path} -> {e.code}: {e.read()[:500].decode(errors="replace")}')

admin = json.load(open(ADMIN))
token = api('POST', '/tokens', {'identity': admin['email'], 'secret': admin['password']})['token']

cert = next((c for c in api('GET', '/nginx/certificates') if DOMAIN in c.get('domain_names', [])), None)
if cert is None:
    if not DNS_CREDS: raise SystemExit('NPM_DNS_CREDENTIALS is required to request a new certificate')
    creds = open(DNS_CREDS).read().strip() + '\n'
    keys = {l.split('=', 1)[0].strip() for l in creds.splitlines() if '=' in l}
    if not {'dns_aliyun_access_key', 'dns_aliyun_access_key_secret'} <= keys:
        raise SystemExit('credential file lacks certbot-dns-aliyun fields')
    print('requesting Let\'s Encrypt certificate via Aliyun DNS-01 (takes ~5-6 min)...', flush=True)
    cert = api('POST', '/nginx/certificates', {'provider': 'letsencrypt', 'domain_names': [DOMAIN],
        'meta': {'dns_challenge': True, 'dns_provider': 'aliyun', 'dns_provider_credentials': creds,
                 'propagation_seconds': 300, 'key_type': 'ecdsa'}}, timeout=900)
print(f'certificate #{cert["id"]} expires {cert.get("expires_on")}')

host = {'domain_names': [DOMAIN], 'forward_scheme': 'http', 'forward_host': 'taboo', 'forward_port': 7100,
        'certificate_id': cert['id'], 'ssl_forced': True, 'hsts_enabled': True, 'hsts_subdomains': False,
        'http2_support': True, 'block_exploits': True, 'caching_enabled': False, 'allow_websocket_upgrade': True,
        'access_list_id': 0, 'advanced_config': '', 'locations': [], 'enabled': True, 'meta': {}}
existing = next((h for h in api('GET', '/nginx/proxy-hosts') if DOMAIN in h['domain_names']), None)
h = api('PUT' if existing else 'POST', '/nginx/proxy-hosts' + (f'/{existing["id"]}' if existing else ''), host)
print(f'proxy host #{h["id"]} {DOMAIN} -> taboo:7100 (cert #{h["certificate_id"]}, force SSL)')
