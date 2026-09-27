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
	version, err := c.UpsertSecret(ctx, "project-a", "dev", "/", "CONSUMER_SECRET", "consumer-fixture-value", "", nil)
	if err != nil || version < 1 {
		t.Fatalf("write: %v", err)
	}
	items, err := c.ListSecrets(ctx, "project-a", "dev", "/")
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v", err)
	}
	value, err := c.GetSecret(ctx, "project-a", "dev", "/", "CONSUMER_SECRET")
	if err != nil || value.Value != "consumer-fixture-value" {
		t.Fatalf("reveal: %v", err)
	}
	exported, err := c.ExportSecrets(ctx, "project-a", "dev")
	if err != nil || !strings.Contains(exported, "CONSUMER_SECRET=consumer-fixture-value") {
		t.Fatalf("export: %v", err)
	}
	_, err = c.ListSecrets(ctx, "project-b", "dev", "/")
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
		_, err = limited.GetSecret(ctx, "project-a", "dev", "/", "CONSUMER_SECRET")
		if !errors.As(err, &denied) || denied.Status != 403 {
			t.Fatalf("missing scope accepted: %v", err)
		}
		cfg.Organization = "b"
		wrong, err := New(c.BaseURL).WithOAuthClientCredentials(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = wrong.ListSecrets(ctx, "project-a", "dev", "/")
		if !errors.As(err, &denied) || denied.Status != 401 {
			t.Fatalf("wrong target accepted: %v", err)
		}
	}
}
