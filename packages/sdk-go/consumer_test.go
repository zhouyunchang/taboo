package taboo

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// Executed against the real Go server by scripts/test-consumers.sh. Both token
// profiles and the original local machine mode share this consumer contract.
func TestConsumerAPI(t *testing.T) {
	if os.Getenv("TABOO_TEST_CONSUMER") != "1" {
		t.Skip("run scripts/test-consumers.sh")
	}
	cfg := OAuthCredentials{Issuer: os.Getenv("TABOO_OAUTH_ISSUER"), MetadataMode: "oauth", ClientID: os.Getenv("TABOO_CLIENT_ID"), ClientSecret: os.Getenv("TABOO_CLIENT_SECRET"), Scopes: []string{"read", "write", "reveal", "export"}, Resource: "https://taboo.example/api", Organization: "a", TrustID: os.Getenv("TABOO_OAUTH_TRUST_ID")}
	c := New(os.Getenv("TABOO_SERVER"))
	if os.Getenv("TABOO_AUTH_MODE") == "oauth" {
		var err error
		c, err = c.WithOAuthClientCredentials(cfg)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		c.WithIdentity(cfg.ClientID, cfg.ClientSecret)
	}
	ctx := context.Background()
	expected := 0
	options := MutationOptions{IdempotencyKey: "sdk-consumer-create", ExpectedVersion: &expected}
	version, err := c.UpsertSecret(ctx, "project-a", "/", "CONSUMER_SECRET", "consumer-fixture-value", "", nil, options)
	if err != nil || version < 1 {
		t.Fatalf("write: %v", err)
	}
	replay, err := c.UpsertSecret(ctx, "project-a", "/", "CONSUMER_SECRET", "consumer-fixture-value", "", nil, options)
	if err != nil || replay != version {
		t.Fatalf("receipt replay: %v", err)
	}
	items, err := c.ListSecrets(ctx, "project-a", "/")
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v", err)
	}
	value, err := c.GetSecret(ctx, "project-a", "/", "CONSUMER_SECRET")
	if err != nil || value.Value != "consumer-fixture-value" {
		t.Fatalf("reveal: %v", err)
	}
	exported, err := c.ExportSecrets(ctx, "project-a")
	if err != nil || !strings.Contains(exported, "CONSUMER_SECRET=consumer-fixture-value") {
		t.Fatalf("export: %v", err)
	}
	refVersion := 0
	if _, err = c.UpsertSecret(ctx, "project-a", "/", "REF_TARGET", "resolved-target", "", nil, MutationOptions{IdempotencyKey: "sdk-ref-target", ExpectedVersion: &refVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.UpsertSecret(ctx, "project-a", "/", "REF_SRC", "${dev:REF_TARGET}", "", nil, MutationOptions{IdempotencyKey: "sdk-ref-src", ExpectedVersion: &refVersion}); err != nil {
		t.Fatal(err)
	}
	literal, err := c.GetSecret(ctx, "project-a", "/", "REF_SRC")
	if err != nil || literal.Value != "${dev:REF_TARGET}" {
		t.Fatalf("raw ref: %v", err)
	}
	resolved, err := c.GetSecret(ctx, "project-a", "/", "REF_SRC", GetOptions{Resolve: true})
	if err != nil || resolved.Value != "resolved-target" {
		t.Fatalf("resolved ref: %+v %v", resolved, err)
	}
	preview, err := c.ImportEnv(ctx, "project-a", "/", "FROM_ENV=imported-value\nREF_TARGET=clash\n", "", true)
	if err != nil || preview.Created != 1 || len(preview.Conflicts) != 1 {
		t.Fatalf("import preview: %+v %v", preview, err)
	}
	imported, err := c.ImportEnv(ctx, "project-a", "/", "FROM_ENV=imported-value\n", "skip", false, MutationOptions{IdempotencyKey: "sdk-import"})
	if err != nil || imported.Created != 1 {
		t.Fatalf("import: %+v %v", imported, err)
	}
	report, err := c.CreateScanReport(ctx, "a", "sdk", []ScanFinding{{RuleID: "generic-api-key", Level: "warning", File: "src/app.env", Line: 2, Match: "plaintext-secret"}})
	if err != nil || report.FindingCount != 1 {
		t.Fatalf("scan report: %+v %v", report, err)
	}
	detail, err := c.GetScanReport(ctx, "a", report.ID)
	if err != nil || len(detail.Findings) != 1 || detail.Findings[0].Alg != "hmac-sha256" || detail.Findings[0].Match != "" || len(detail.Findings[0].Fingerprint) != 64 {
		t.Fatalf("scan detail: %+v %v", detail, err)
	}
	if strings.Contains(detail.Findings[0].Fingerprint, "plaintext") {
		t.Fatalf("fingerprint leaked text: %+v", detail.Findings[0])
	}
	reports, err := c.ListScanReports(ctx, "a")
	if err != nil || len(reports) < 1 {
		t.Fatalf("scan list: %v", err)
	}
	_, err = c.ListSecrets(ctx, "project-b", "/")
	var denied *ApiError
	if !errors.As(err, &denied) || denied.Status != 403 {
		t.Fatalf("cross organization accepted: %v", err)
	}
	if c.oauth != nil {
		cfg.Scopes = []string{"write"}
		limited, err := New(c.BaseURL).WithOAuthClientCredentials(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = limited.GetSecret(ctx, "project-a", "/", "CONSUMER_SECRET")
		if !errors.As(err, &denied) || denied.Status != 403 {
			t.Fatalf("missing scope accepted: %v", err)
		}
		cfg.Organization = "b"
		wrong, err := New(c.BaseURL).WithOAuthClientCredentials(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = wrong.ListSecrets(ctx, "project-a", "/")
		if !errors.As(err, &denied) || denied.Status != 401 {
			t.Fatalf("wrong target accepted: %v", err)
		}
	}
}
