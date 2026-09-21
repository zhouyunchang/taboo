// 权限求值：纯函数，不做 I/O。DB 查找在 auth.Can。
package rbac

const (
	Read        = "read"
	Reveal      = "reveal"
	Write       = "write"
	Admin       = "admin"
	Members     = "members"
	AuditRead   = "audit.read"
	AuditExport = "audit.export"
	Manage      = "manage" // 兼容旧调用：等同 admin
)

var roleRank = map[string]int{"viewer": 1, "developer": 2, "admin": 3, "owner": 4}

func Normalize(action string) string {
	if action == Manage {
		return Admin
	}
	return action
}

func Rank(role string) int { return roleRank[role] }

func Privileged(role string) bool { return roleRank[role] >= roleRank["admin"] }

// EffectiveRole 组织角色 + 可选项目收窄/覆盖。
func EffectiveRole(orgRole string, restricted bool, grantRole string, hasGrant bool) (role string, ok bool) {
	if Privileged(orgRole) {
		return orgRole, true
	}
	if restricted {
		if !hasGrant {
			return "", false
		}
		return grantRole, true
	}
	if hasGrant {
		return grantRole, true
	}
	return orgRole, true
}

// UserAllows 用户角色 × 动作 × 保护环境。
func UserAllows(role, action string, envProtected bool) bool {
	action = Normalize(action)
	rank := roleRank[role]
	switch action {
	case Read:
		return rank >= roleRank["viewer"]
	case Reveal, Write:
		if rank >= roleRank["admin"] {
			return true
		}
		return role == "developer" && !envProtected
	case Admin:
		return rank >= roleRank["admin"]
	case Members:
		return rank >= roleRank["owner"]
	case AuditRead, AuditExport:
		return rank >= roleRank["admin"]
	}
	return false
}

func hasPerm(perms []string, want ...string) bool {
	for _, p := range perms {
		for _, w := range want {
			if p == w {
				return true
			}
		}
	}
	return false
}

// IdentityAllows 机器身份：write ⊃ reveal ⊃ read。envEmpty 时项目内任一 scope 即可读。
func IdentityAllows(action string, perms []string) bool {
	switch Normalize(action) {
	case Read:
		return hasPerm(perms, "read", "reveal", "write")
	case Reveal:
		return hasPerm(perms, "reveal", "write")
	case Write:
		return hasPerm(perms, "write")
	}
	return false
}

func Sensitive(action string) bool {
	switch Normalize(action) {
	case Reveal, Write, Admin, Members, AuditExport:
		return true
	}
	return false
}
