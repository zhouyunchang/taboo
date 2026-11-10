"""taboo Python SDK. Method names follow packages/sdk-go."""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from base64 import b64encode


class ApiError(Exception):
    def __init__(self, status, code, message, details=None):
        super().__init__(f"taboo {status} {code}: {message}")
        self.status = status
        self.code = code
        self.message = message
        self.details = details


def _loopback(raw):
    parsed = urllib.parse.urlparse(raw)
    host = parsed.hostname or ""
    if parsed.username or parsed.query or parsed.fragment:
        raise ValueError("HTTPS or loopback HTTP URL required")
    if parsed.scheme == "https" or (parsed.scheme == "http" and host in {"127.0.0.1", "localhost", "::1"}):
        return parsed
    raise ValueError("HTTPS or loopback HTTP URL required")


def _query(pairs):
    values = []
    for i in range(0, len(pairs), 2):
        if pairs[i + 1] not in ("", None):
            values.append((pairs[i], str(pairs[i + 1])))
    text = urllib.parse.urlencode(values)
    return f"?{text}" if text else ""


class Client:
    def __init__(self, base_url):
        self.base_url = base_url.rstrip("/")
        self.token = ""
        self.refresh_token = ""
        self.identity = None
        self.oauth = None
        self.on_refresh = None

    def with_token(self, token):
        self.token = token
        self.refresh_token = ""
        self.identity = None
        self.oauth = None
        return self

    def with_user_tokens(self, access, refresh, on_refresh=None):
        self.token = access
        self.refresh_token = refresh
        self.identity = None
        self.oauth = None
        self.on_refresh = on_refresh
        return self

    def with_identity(self, client_id, client_secret):
        self.identity = {"client_id": client_id, "client_secret": client_secret, "expires_at": 0}
        self.oauth = None
        self.token = ""
        self.refresh_token = ""
        return self

    def with_oauth_client_credentials(self, config):
        _loopback(config["issuer"])
        _loopback(self.base_url)
        mode = config.get("metadata_mode") or "oauth"
        if mode not in {"oauth", "oidc"}:
            raise ValueError("metadata mode must be oauth or oidc")
        scopes = list(config.get("scopes") or [])
        if not config.get("client_id") or not config.get("client_secret") or not config.get("organization") or not config.get("trust_id") or not scopes:
            raise ValueError("OAuth client credential, scopes, organization and trust ID are required")
        for scope in scopes:
            if not scope or any(ch.isspace() for ch in scope):
                raise ValueError("each OAuth scope must be one exact value")
        if config.get("resource"):
            resource = urllib.parse.urlparse(config["resource"])
            if not resource.scheme or resource.fragment:
                raise ValueError("resource must be an absolute URI without a fragment")
        self.oauth = {**config, "metadata_mode": mode, "scopes": scopes, "endpoint": "", "metadata_expires": 0, "expires": 0}
        self.identity = None
        self.token = ""
        self.refresh_token = ""
        self.on_refresh = None
        return self

    def _ensure_token(self):
        if self.oauth:
            return self._oauth_token()
        ident = self.identity
        if ident and ident["expires_at"] - time.time() < 30:
            status, data = self._raw("POST", "/api/v1/identities/token", {"client_id": ident["client_id"], "client_secret": ident["client_secret"]}, auth=False)
            if status != 200 or not data.get("access_token"):
                raise RuntimeError(f"identity token exchange failed: {status}")
            self.token = data["access_token"]
            ident["expires_at"] = time.time() + data["expires_in"]
        return self.token

    def _oauth_token(self):
        cfg = self.oauth
        if self.token and cfg["expires"] - time.time() > 30:
            return self.token
        issuer = _loopback(cfg["issuer"])
        if not cfg["endpoint"] or time.time() > cfg["metadata_expires"]:
            path = issuer.path.rstrip("/")
            metadata = f"{issuer.scheme}://{issuer.netloc}/.well-known/oauth-authorization-server{path}"
            if cfg["metadata_mode"] == "oidc":
                metadata = cfg["issuer"].rstrip("/") + "/.well-known/openid-configuration"
            req = urllib.request.Request(metadata)
            with urllib.request.urlopen(req, timeout=30) as res:
                data = json.loads(res.read(1 << 20))
                status = res.status
            if status != 200 or data.get("issuer") != cfg["issuer"]:
                raise RuntimeError("OAuth metadata rejected")
            endpoint = _loopback(data["token_endpoint"])
            if endpoint.scheme != issuer.scheme or endpoint.netloc != issuer.netloc:
                raise RuntimeError("untrusted OAuth token endpoint")
            methods = data.get("token_endpoint_auth_methods_supported") or []
            if methods and "client_secret_basic" not in methods:
                raise RuntimeError("OAuth client_secret_basic is not supported")
            cfg["endpoint"] = data["token_endpoint"]
            cfg["metadata_expires"] = time.time() + 300
        form = {"grant_type": "client_credentials", "scope": " ".join(cfg["scopes"])}
        if cfg.get("resource"):
            form["resource"] = cfg["resource"]
        basic = b64encode(f"{urllib.parse.quote(cfg['client_id'], safe='')}:{urllib.parse.quote(cfg['client_secret'], safe='')}".encode()).decode()
        req = urllib.request.Request(cfg["endpoint"], data=urllib.parse.urlencode(form).encode(), method="POST")
        req.add_header("Content-Type", "application/x-www-form-urlencoded")
        req.add_header("Authorization", f"Basic {basic}")
        try:
            with urllib.request.urlopen(req, timeout=30) as res:
                token = json.loads(res.read(1 << 20))
                status = res.status
        except urllib.error.HTTPError as exc:
            raise RuntimeError("OAuth token exchange rejected") from exc
        access = token.get("access_token") or ""
        if status != 200 or not access or len(access) > 16384 or str(token.get("token_type", "")).lower() != "bearer" or not 0 < int(token.get("expires_in") or 0) <= 86400:
            raise RuntimeError("OAuth token exchange rejected")
        if token.get("scope") is not None:
            granted = str(token["scope"]).split()
            for scope in cfg["scopes"]:
                if scope not in granted:
                    raise RuntimeError("OAuth server did not grant every requested scope")
        self.token = access
        cfg["expires"] = time.time() + int(token["expires_in"])
        return self.token

    def _raw(self, method, path, body=None, auth=True, idempotency_key=None, text=False):
        token = self._ensure_token() if auth else ""
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base_url + path, data=data, method=method)
        if body is not None:
            req.add_header("Content-Type", "application/json")
        if idempotency_key:
            req.add_header("Idempotency-Key", idempotency_key)
        if token:
            req.add_header("Authorization", f"Bearer {token}")
        if self.oauth and auth:
            req.add_header("X-Taboo-Trust", self.oauth["trust_id"])
            req.add_header("X-Taboo-Organization", self.oauth["organization"])
        try:
            with urllib.request.urlopen(req, timeout=30) as res:
                payload = res.read()
                if text:
                    return res.status, payload.decode()
                if res.status == 204 or not payload:
                    return res.status, None
                return res.status, json.loads(payload)
        except urllib.error.HTTPError as exc:
            raw = exc.read()
            try:
                parsed = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                parsed = {}
            raise ApiError(exc.code, parsed.get("code") or f"HTTP_{exc.code}", parsed.get("message") or "request rejected", parsed.get("details")) from None

    def request(self, method, path, body=None, idempotency_key=None, text=False):
        _, data = self._raw(method, path, body, True, idempotency_key, text)
        return data

    def login(self, email, password):
        return self.request("POST", "/api/v1/auth/login", {"email": email, "password": password})

    def totp_login(self, challenge, code):
        return self.request("POST", "/api/v1/auth/totp/login", {"challenge": challenge, "code": code})

    def register(self, email, password, name):
        return self.request("POST", "/api/v1/auth/register", {"email": email, "password": password, "name": name})

    def refresh(self, refresh):
        out = self.request("POST", "/api/v1/auth/refresh", {"refresh": refresh})
        self.token = out["access"]
        self.refresh_token = out["refresh"]
        if self.on_refresh:
            self.on_refresh(out["access"], out["refresh"])
        return out

    def me(self):
        return self.request("GET", "/api/v1/me")

    def projects(self, org_slug):
        return self.request("GET", f"/api/v1/orgs/{urllib.parse.quote(org_slug)}/projects")

    def list_folders(self, project_id, env):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/folders{_query(['env', env])}")

    def create_folder(self, project_id, env, path):
        return self.request("POST", f"/api/v1/projects/{urllib.parse.quote(project_id)}/folders{_query(['env', env])}", {"path": path})

    def list_secrets(self, project_id, env, folder_path=""):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets{_query(['env', env, 'path', folder_path])}")

    def upsert_secret(self, project_id, env, folder_path, key, value, comment="", tags=None, options=None):
        options = options or {}
        body = {"key": key, "value": value, "comment": comment, "tags": tags or []}
        if options.get("expected_version") is not None:
            body["expectedVersion"] = options["expected_version"]
        return self.request("POST", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets{_query(['env', env, 'path', folder_path])}", body, options.get("idempotency_key"))

    def get_by_locator(self, locator, key):
        return self.request("GET", "/api/v1/access?" + urllib.parse.urlencode({"locator": locator, "key": key}))

    def put_by_locator(self, locator, key, value, options=None):
        options = options or {}
        body = {"key": key, "value": value}
        if options.get("expected_version") is not None:
            body["expectedVersion"] = options["expected_version"]
        return self.request("PUT", "/api/v1/access?" + urllib.parse.urlencode({"locator": locator}), body, options.get("idempotency_key"))

    def list_by_locator(self, locator):
        return self.request("GET", "/api/v1/access?" + urllib.parse.urlencode({"locator": locator}))

    def get_secret(self, project_id, env, folder_path, key, resolve=False):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets/{urllib.parse.quote(key)}{_query(['env', env, 'path', folder_path, 'resolve', 'true' if resolve else ''])}")

    def reveal_secret_version(self, project_id, env, folder_path, key, version, resolve=False):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets/{urllib.parse.quote(key)}/versions/{version}{_query(['env', env, 'path', folder_path, 'resolve', 'true' if resolve else ''])}")

    def secret_versions(self, project_id, env, folder_path, key):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets/{urllib.parse.quote(key)}/versions{_query(['env', env, 'path', folder_path])}")

    def rollback_secret(self, project_id, env, folder_path, key, version, options=None):
        options = options or {}
        body = {"version": version}
        if options.get("expected_version") is not None:
            body["expectedVersion"] = options["expected_version"]
        return self.request("POST", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets/{urllib.parse.quote(key)}/rollback{_query(['env', env, 'path', folder_path])}", body, options.get("idempotency_key"))

    def export_secrets(self, project_id, env, resolve=False):
        return self.request("GET", f"/api/v1/projects/{urllib.parse.quote(project_id)}/export{_query(['env', env, 'resolve', 'true' if resolve else ''])}", text=True)

    def import_env(self, project_id, env, folder_path, content, strategy="", preview=False, options=None):
        options = options or {}
        body = {"content": content, "preview": preview}
        if strategy:
            body["strategy"] = strategy
        return self.request("POST", f"/api/v1/projects/{urllib.parse.quote(project_id)}/import{_query(['env', env, 'path', folder_path])}", body, options.get("idempotency_key"))

    def delete_secret(self, project_id, env, folder_path, key, options=None):
        options = options or {}
        pairs = ["env", env, "path", folder_path]
        if options.get("expected_version") is not None:
            pairs += ["expectedVersion", str(options["expected_version"])]
        return self.request("DELETE", f"/api/v1/projects/{urllib.parse.quote(project_id)}/secrets/{urllib.parse.quote(key)}{_query(pairs)}", idempotency_key=options.get("idempotency_key"))

    def default_project(self, org_slug):
        data = self.projects(org_slug)
        for item in data.get("projects") or []:
            if item.get("slug") == "default":
                return item
        if not data.get("projects"):
            raise RuntimeError(f"no project in org {org_slug}")
        return data["projects"][0]

    def create_scan_report(self, org_slug, source, findings):
        return self.request("POST", f"/api/v1/orgs/{urllib.parse.quote(org_slug)}/scan-reports", {"source": source, "findings": findings or []})

    def list_scan_reports(self, org_slug):
        return self.request("GET", f"/api/v1/orgs/{urllib.parse.quote(org_slug)}/scan-reports")

    def get_scan_report(self, org_slug, report_id):
        return self.request("GET", f"/api/v1/orgs/{urllib.parse.quote(org_slug)}/scan-reports/{urllib.parse.quote(report_id)}")
