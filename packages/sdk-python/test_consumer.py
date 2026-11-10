"""Consumer contract shared with packages/sdk-go. Skips unless TABOO_TEST_CONSUMER=1."""

import os
import sys

from taboo import ApiError, Client


def main():
    if os.environ.get("TABOO_TEST_CONSUMER") != "1":
        print("skip: run scripts/test-consumers.sh")
        return
    client = Client(os.environ["TABOO_SERVER"])
    scopes = ["read", "write", "reveal", "export"]
    if os.environ.get("TABOO_AUTH_MODE") == "oauth":
        client.with_oauth_client_credentials({
            "issuer": os.environ["TABOO_OAUTH_ISSUER"], "metadata_mode": "oauth",
            "client_id": os.environ["TABOO_CLIENT_ID"], "client_secret": os.environ["TABOO_CLIENT_SECRET"],
            "scopes": scopes, "resource": "https://taboo.example/api", "organization": "a",
            "trust_id": os.environ["TABOO_OAUTH_TRUST_ID"],
        })
    else:
        client.with_identity(os.environ["TABOO_CLIENT_ID"], os.environ["TABOO_CLIENT_SECRET"])
    created = client.upsert_secret("project-a", "/", "PY_SECRET", "python-fixture-value", "", None, {"idempotency_key": "py-consumer-create", "expected_version": 0})
    assert created["version"] >= 1
    replay = client.upsert_secret("project-a", "/", "PY_SECRET", "python-fixture-value", "", None, {"idempotency_key": "py-consumer-create", "expected_version": 0})
    assert replay["version"] == created["version"]
    listed = client.list_secrets("project-a", "/")
    assert any(item["key"] == "PY_SECRET" for item in listed["secrets"])
    assert client.get_secret("project-a", "/", "PY_SECRET")["value"] == "python-fixture-value"
    assert "PY_SECRET=python-fixture-value" in client.export_secrets("project-a")
    try:
        client.list_secrets("project-b", "/")
        raise SystemExit("cross organization accepted")
    except ApiError as err:
        assert err.status == 403
    client.upsert_secret("project-a", "/", "PY_REF_TARGET", "python-resolved", "", None, {"idempotency_key": "py-ref-target", "expected_version": 0})
    client.upsert_secret("project-a", "/", "PY_REF_SRC", "${dev:PY_REF_TARGET}", "", None, {"idempotency_key": "py-ref-src", "expected_version": 0})
    assert client.get_secret("project-a", "/", "PY_REF_SRC")["value"] == "${dev:PY_REF_TARGET}"
    assert client.get_secret("project-a", "/", "PY_REF_SRC", resolve=True)["value"] == "python-resolved"
    preview = client.import_env("project-a", "/", "PY_FROM_ENV=imported\nPY_REF_TARGET=clash\n", "", True)
    assert preview["created"] == 1 and len(preview["conflicts"]) == 1
    imported = client.import_env("project-a", "/", "PY_FROM_ENV=imported\n", "skip", False, {"idempotency_key": "py-import"})
    assert imported["created"] == 1
    report = client.create_scan_report("a", "sdk-python", [{"rule_id": "generic-api-key", "level": "warning", "file": "src/app.env", "line": 2, "match": "plaintext-secret"}])
    assert report["finding_count"] == 1 and "plaintext-secret" not in str(report)
    detail = client.get_scan_report("a", report["id"])
    assert detail["findings"][0]["alg"] == "hmac-sha256" and "plaintext-secret" not in str(detail)
    assert len(client.list_scan_reports("a")["reports"]) >= 1
    if os.environ.get("TABOO_AUTH_MODE") == "oauth":
        limited = Client(client.base_url).with_oauth_client_credentials({
            "issuer": os.environ["TABOO_OAUTH_ISSUER"], "metadata_mode": "oauth",
            "client_id": os.environ["TABOO_CLIENT_ID"], "client_secret": os.environ["TABOO_CLIENT_SECRET"],
            "scopes": ["write"], "resource": "https://taboo.example/api", "organization": "a",
            "trust_id": os.environ["TABOO_OAUTH_TRUST_ID"],
        })
        try:
            limited.get_secret("project-a", "/", "PY_SECRET")
            raise SystemExit("missing scope accepted")
        except ApiError as err:
            assert err.status == 403
        wrong = Client(client.base_url).with_oauth_client_credentials({
            "issuer": os.environ["TABOO_OAUTH_ISSUER"], "metadata_mode": "oauth",
            "client_id": os.environ["TABOO_CLIENT_ID"], "client_secret": os.environ["TABOO_CLIENT_SECRET"],
            "scopes": ["write"], "resource": "https://taboo.example/api", "organization": "b",
            "trust_id": os.environ["TABOO_OAUTH_TRUST_ID"],
        })
        try:
            wrong.list_secrets("project-a", "/")
            raise SystemExit("wrong target accepted")
        except ApiError as err:
            assert err.status == 401
    print("python sdk consumer contract passed")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(exc, file=sys.stderr)
        raise
