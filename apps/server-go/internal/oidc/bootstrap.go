package oidc

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"strings"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

// BootstrapFromEnv 用环境变量登记实例级 OIDC Realm（Keycloak 等）。
// 需 TABOO_OIDC_ISSUER + TABOO_OIDC_CLIENT_ID + TABOO_OIDC_CLIENT_SECRET + TABOO_OIDC_ORG（已有组织 slug）。
// 已存在相同 issuer 的配置则跳过，避免覆盖手工修改。
func BootstrapFromEnv(db *sql.DB, masterKey []byte) {
	issuer := strings.TrimSuffix(os.Getenv("TABOO_OIDC_ISSUER"), "/")
	clientID := os.Getenv("TABOO_OIDC_CLIENT_ID")
	secret := os.Getenv("TABOO_OIDC_CLIENT_SECRET")
	orgSlug := os.Getenv("TABOO_OIDC_ORG")
	if issuer == "" && clientID == "" && secret == "" && orgSlug == "" {
		return
	}
	if issuer == "" || clientID == "" || secret == "" || orgSlug == "" {
		log.Println("[taboo] OIDC bootstrap skipped: set TABOO_OIDC_ISSUER, TABOO_OIDC_CLIENT_ID, TABOO_OIDC_CLIENT_SECRET, TABOO_OIDC_ORG together")
		return
	}
	var orgID string
	if err := db.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, orgSlug).Scan(&orgID); err != nil {
		log.Printf("[taboo] OIDC bootstrap: org %q not found, create it first then restart", orgSlug)
		return
	}
	var existing string
	err := db.QueryRow(`SELECT id FROM oidc_providers WHERE org_id = ? AND issuer = ?`, orgID, issuer).Scan(&existing)
	if err == nil {
		log.Printf("[taboo] OIDC realm already registered for org %s (%s)", orgSlug, issuer)
		return
	}
	if err != nil && err != sql.ErrNoRows {
		log.Printf("[taboo] OIDC bootstrap: %v", err)
		return
	}
	if _, _, err := discoCache.get("probe-"+tc.NewID(), issuer); err != nil {
		log.Printf("[taboo] OIDC bootstrap: discovery failed for %s: %v", issuer, err)
		return
	}
	enc, err := tc.Encrypt(masterKey, secret)
	if err != nil {
		log.Printf("[taboo] OIDC bootstrap: encrypt secret: %v", err)
		return
	}
	name := os.Getenv("TABOO_OIDC_NAME")
	if name == "" {
		name = "Keycloak"
	}
	roleClaim := os.Getenv("TABOO_OIDC_ROLE_CLAIM")
	if roleClaim == "" {
		roleClaim = "groups"
	}
	defRole := os.Getenv("TABOO_OIDC_DEFAULT_ROLE")
	if defRole == "" || !validRoles[defRole] {
		defRole = "viewer"
	}
	roleMap := map[string]string{}
	if raw := os.Getenv("TABOO_OIDC_ROLE_MAP"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &roleMap)
	}
	roleJSON, _ := json.Marshal(roleMap)
	id := tc.NewID()
	if _, err := db.Exec(`INSERT INTO oidc_providers
		(id, org_id, name, issuer, client_id, client_secret_enc, scopes, role_claim, role_map, default_role,
		 username_claim, public_login, autocreate, sync_role, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'email', 1, 1, 1, 1)`,
		id, orgID, name, issuer, clientID, enc, "openid,email,profile", roleClaim, string(roleJSON), defRole); err != nil {
		log.Printf("[taboo] OIDC bootstrap insert: %v", err)
		return
	}
	log.Printf("[taboo] OIDC realm %q registered for org %s (public login)", name, orgSlug)
}
