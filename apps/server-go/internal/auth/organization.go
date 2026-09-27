package auth

import (
	"database/sql"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

func OrganizationLoginPolicy(db *sql.DB, org string) (string, error) {
	var policy string
	err := db.QueryRow(`SELECT login_policy FROM org_security WHERE org_id=?`, org).Scan(&policy)
	if err == sql.ErrNoRows {
		return "LOCAL_OR_EXTERNAL", nil
	}
	return policy, err
}

// Login policy concerns human authentication. Machine credential policies are
// independently bound and retain their declared migration/revocation semantics.
func OrganizationLoginAllowed(db *sql.DB, a *Actor, org string) bool {
	if a == nil {
		return false
	}
	if a.Kind != KindUser {
		return true
	}
	policy, err := OrganizationLoginPolicy(db, org)
	return err == nil && (policy == "LOCAL_OR_EXTERNAL" || (a.BrowserOrigin != "" && a.AuthenticationSource != "" && a.OrganizationID == org))
}

// DelegationAction prevents a mapped external admin from manufacturing another
// credential or delivery channel that bypasses explicit secret action grants.
// Organization owners are assigned locally and own authorization governance.
func DelegationAction(a *Actor) string {
	if a != nil && a.AuthenticationSource != "" {
		return "members"
	}
	return "admin"
}

// ProjectVisible covers non-secret project/environment metadata. Secret and
// folder handlers still check their explicit resource actions independently.
func ProjectVisible(db *sql.DB, a *Actor, org, project string) bool {
	if a == nil || a.OrganizationID != "" && a.OrganizationID != org {
		return false
	}
	if a.Kind == KindIdentity {
		return Can(db, a, org, project, "read", "")
	}
	role, restricted, ok := MemberInfo(db, org, a.ID)
	if !ok {
		return false
	}
	grant, hasGrant := projectGrant(db, project, a.ID)
	effective, ok := rbac.EffectiveRole(role, restricted, grant, hasGrant)
	return ok && rbac.UserAllows(effective, rbac.Read, false)
}

// ExternalOwnerAvailable is evaluated in the same transaction as changes to
// connections or identity bindings. An admission alone is not current proof:
// its issuer must still match the user's explicitly bound external identity.
func ExternalOwnerAvailable(db interface{ QueryRow(string, ...any) *sql.Row }, org string) bool {
	var policy string
	err := db.QueryRow(`SELECT login_policy FROM org_security WHERE org_id=?`, org).Scan(&policy)
	if err == sql.ErrNoRows {
		return true
	}
	if err != nil {
		return false
	}
	if policy != "EXTERNAL_REQUIRED" {
		return true
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM org_members m JOIN users u ON u.id=m.user_id JOIN federation_admissions a ON a.org_id=m.org_id AND a.user_id=m.user_id JOIN oidc_providers p ON p.id=a.provider_id AND p.org_id=a.org_id JOIN external_identities e ON e.user_id=m.user_id AND e.issuer=p.issuer WHERE m.org_id=? AND m.role='owner' AND u.status='active' AND p.enabled=1`, org).Scan(&n)
	return err == nil && n > 0
}
