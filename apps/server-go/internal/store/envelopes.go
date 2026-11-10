package store

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
)

// UpgradeEnvelopes runs offline with the original master key. Every ciphertext
// is authenticated before commit. A bad key or damaged row rolls back all work.
// Passing apply=false verifies recoverability without modifying the database.
func UpgradeEnvelopes(db *sql.DB, master []byte, apply bool) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count := 0
	deks := map[string][]byte{}
	defer func() {
		for _, key := range deks {
			clear(key)
		}
	}()
	rows, err := tx.Query(`SELECT id,dek_encrypted FROM orgs ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type wrapped struct{ id, value string }
	var orgs []wrapped
	for rows.Next() {
		var o wrapped
		if err = rows.Scan(&o.id, &o.value); err != nil {
			rows.Close()
			return 0, err
		}
		orgs = append(orgs, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	upgrade := func(key []byte, value string, c tc.Context) (string, error) {
		plain, err := tc.Open(key, value, c)
		if err != nil {
			return "", fmt.Errorf("envelope verification failed (%s, object %s)", c.Purpose, c.Object)
		}
		if strings.HasPrefix(value, "taboo:v1:") {
			return value, nil
		}
		count++
		if !apply {
			return value, nil
		}
		return tc.Seal(key, plain, c)
	}
	for _, o := range orgs {
		plain, err := tc.Open(master, o.value, tc.DEKContext(o.id))
		if err != nil {
			return 0, fmt.Errorf("cannot recover DEK for organization %s", o.id)
		}
		key, err := hex.DecodeString(plain)
		if err != nil || len(key) != 32 {
			return 0, fmt.Errorf("invalid DEK for organization %s", o.id)
		}
		deks[o.id] = key
		value, err := upgrade(master, o.value, tc.DEKContext(o.id))
		if err != nil {
			return 0, err
		}
		if apply {
			if _, err = tx.Exec(`UPDATE orgs SET dek_encrypted=? WHERE id=?`, value, o.id); err != nil {
				return 0, err
			}
		}
	}
	// Queries return ID, organization, project, environment, version, ciphertext.
	type source struct {
		table, column, query, purpose string
		orgKey                        bool
	}
	sources := []source{
		{"oauth_trusts", "client_secret_enc", `SELECT id,org_id,'','',1,client_secret_enc,id FROM oauth_trusts WHERE client_secret_enc<>''`, "oauth-introspection", false},
		{"secret_versions", "ciphertext", `SELECT v.id,p.org_id,p.id,'',v.version,v.ciphertext,s.id FROM secret_versions v JOIN secrets s ON s.id=v.secret_id JOIN projects p ON p.id=s.project_id`, "secret", true},
		{"oidc_providers", "client_secret_enc", `SELECT id,org_id,'','',1,client_secret_enc,id FROM oidc_providers`, "oidc-client", false},
		{"sync_targets", "config_enc", `SELECT id,org_id,'','',1,config_enc,id FROM sync_targets`, "sync-target", false},
		{"webhooks", "secret_enc", `SELECT id,org_id,'','',1,secret_enc,id FROM webhooks`, "webhook", false},
		{"dynamic_engines", "conn_encrypted", `SELECT id,org_id,project_id,'',1,conn_encrypted,id FROM dynamic_engines`, "dynamic-engine", false},
		{"dynamic_leases", "password_encrypted", `SELECT l.id,e.org_id,e.project_id,'',1,l.password_encrypted,l.id FROM dynamic_leases l JOIN dynamic_engines e ON e.id=l.engine_id`, "dynamic-lease", false},
		{"users", "totp_secret_enc", `SELECT id,'','','',1,totp_secret_enc,id FROM users WHERE totp_secret_enc IS NOT NULL AND totp_secret_enc<>''`, "user-totp", false},
		{"users", "totp_pending_enc", `SELECT id,'','','',1,totp_pending_enc,id FROM users WHERE totp_pending_enc IS NOT NULL AND totp_pending_enc<>''`, "user-totp", false},
	}
	for _, src := range sources {
		type item struct {
			id, value string
			c         tc.Context
		}
		var items []item
		rows, err := tx.Query(src.query)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var x item
			var ignored string
			x.c.Purpose = src.purpose
			if err = rows.Scan(&x.id, &x.c.Org, &x.c.Project, &ignored, &x.c.Version, &x.value, &x.c.Object); err != nil {
				rows.Close()
				return 0, err
			}
			items = append(items, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		for _, x := range items {
			key := master
			if src.orgKey {
				key = deks[x.c.Org]
			}
			value, err := upgrade(key, x.value, x.c)
			if err != nil {
				return 0, err
			}
			if apply {
				if _, err = tx.Exec(`UPDATE `+src.table+` SET `+src.column+`=? WHERE id=?`, value, x.id); err != nil {
					return 0, err
				}
			}
		}
	}
	// Pending durable sync snapshots must remain decryptable after cutover too.
	for _, table := range []string{"outbox_events", "sync_runs"} {
		column := "payload"
		if table == "sync_runs" {
			column = "event_payload"
		}
		rows, err := tx.Query(`SELECT id,` + column + ` FROM ` + table)
		if err != nil {
			return 0, err
		}
		var items []wrapped
		for rows.Next() {
			var x wrapped
			if err = rows.Scan(&x.id, &x.value); err != nil {
				rows.Close()
				return 0, err
			}
			items = append(items, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		for _, x := range items {
			var e events.SecretEvent
			if err = json.Unmarshal([]byte(x.value), &e); err != nil {
				return 0, fmt.Errorf("invalid durable payload %s", x.id)
			}
			if e.Ciphertext == "" {
				continue
			}
			e.Ciphertext, err = upgrade(deks[e.OrgID], e.Ciphertext, tc.SecretContext(e.OrgID, e.ProjectID, e.SecretID, e.Version))
			if err != nil {
				return 0, err
			}
			if apply {
				payload, _ := json.Marshal(e)
				if _, err = tx.Exec(`UPDATE `+table+` SET `+column+`=? WHERE id=?`, string(payload), x.id); err != nil {
					return 0, err
				}
			}
		}
	}
	if apply {
		// Provider ciphertext revisions change. Expire pending browser flows and
		// sessions explicitly rather than leaving them to fail unpredictably.
		if _, err = tx.Exec(`DELETE FROM oidc_login_requests`); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(`DELETE FROM browser_sessions`); err != nil {
			return 0, err
		}
		if err = tx.Commit(); err != nil {
			return 0, err
		}
	}
	return count, nil
}
