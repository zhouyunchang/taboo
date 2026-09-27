package auth

import "database/sql"

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
