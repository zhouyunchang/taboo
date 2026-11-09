package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func newCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Run self-check and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck()
		},
	}
}

func runCheck() error {
	initJSONLogger()
	rt, err := loadRuntime(true)
	if err != nil {
		return err
	}
	defer rt.close()
	return runSelfCheck(rt.db, rt.masterKey)
}

func runSelfCheck(db *sql.DB, masterKey []byte) error {
	ok := true
	check := func(name string, err error) {
		if err != nil {
			log.Printf("[check] FAIL %s: %v", name, err)
			ok = false
		} else {
			log.Printf("[check] ok   %s", name)
		}
	}

	ct, err := tc.Encrypt(masterKey, "taboo-self-check")
	check("encrypt", err)
	pt, err := tc.Decrypt(masterKey, ct)
	if err == nil && pt != "taboo-self-check" {
		err = errors.New("roundtrip mismatch")
	}
	check("decrypt roundtrip", err)

	hash, err := tc.HashPassword("check-password")
	check("argon2id hash", err)
	valid, _, err := tc.CheckPassword("check-password", hash)
	if err == nil && !valid {
		err = errors.New("password verify mismatch")
	}
	check("argon2id verify", err)

	v, err := store.Version(db)
	check("read schema version", err)
	if v != store.SchemaVersion {
		check("schema version", fmt.Errorf("user_version=%d, want %d", v, store.SchemaVersion))
	} else {
		log.Printf("[check] ok   schema version %d", v)
	}
	for _, t := range []string{
		"users", "orgs", "secrets", "secret_versions", "audit_logs", "webhooks", "sync_targets",
		"oidc_providers", "org_invites", "project_grants", "outbox_events", "audit_checkpoints", "scan_reports", "rotation_policies",
		"approval_requests", "access_requests", "scim_tokens", "cluster_leases",
	} {
		exists, err := store.HasTable(db, t)
		if err == nil && !exists {
			err = fmt.Errorf("missing table: %s", t)
		}
		check("table "+t, err)
	}

	if !ok {
		log.Printf("[check] FAILED")
		return &exitError{code: 1}
	}
	log.Printf("[check] all checks passed")
	return nil
}
