package auth

import (
	"os"
	"strings"
)

// Role is what a user may do in TowerCrane.
//
//	member    view everything, run plans
//	approver  member + confirm ("yes") plans of apply: manual Applications
//	admin     approver + connect repositories, approve policy (manifest)
//	          changes, destroy, manage users and team mappings
type Role string

const (
	RoleNone     Role = ""
	RoleMember   Role = "member"
	RoleApprover Role = "approver"
	RoleAdmin    Role = "admin"
)

var roleRank = map[Role]int{RoleNone: 0, RoleMember: 1, RoleApprover: 2, RoleAdmin: 3}

// ParseRole validates a role name coming from user input.
func ParseRole(s string) (Role, bool) {
	r := Role(s)
	if r == RoleNone {
		return r, false
	}
	_, ok := roleRank[r]
	return r, ok
}

// AtLeast reports whether r grants at least the permissions of min.
func (r Role) AtLeast(min Role) bool { return roleRank[r] >= roleRank[min] }

// MaxRole returns the most privileged of the given roles.
func MaxRole(roles ...Role) Role {
	best := RoleNone
	for _, r := range roles {
		if roleRank[r] > roleRank[best] {
			best = r
		}
	}
	return best
}

// RoleSources is where a user's role comes from. A user's effective role is
// the highest of all sources, so a role can be granted either in TowerCrane
// or through a GitHub team, and taking it away means removing it from every
// source that grants it.
type RoleSources struct {
	// Local is assigned by an admin inside TowerCrane.
	Local Role `json:"local"`
	// GitHub comes from the GitHub teams the user belongs to, mapped to
	// roles by an admin (refreshed every time the user signs in).
	GitHub Role `json:"github"`
	// Bootstrap is true when the user is listed in TOWERCRANE_ADMINS.
	Bootstrap bool `json:"bootstrap"`
}

// Effective is the role TowerCrane enforces. Every signed-in user is at
// least a member.
func (s RoleSources) Effective() Role {
	bootstrap := RoleNone
	if s.Bootstrap {
		bootstrap = RoleAdmin
	}
	return MaxRole(RoleMember, s.Local, s.GitHub, bootstrap)
}

// IsBootstrapAdmin reports whether the user is listed in TOWERCRANE_ADMINS, a
// comma-separated list of GitHub logins and/or emails. This is how the very
// first admin is created, and a way back in if every admin gets locked out.
func IsBootstrapAdmin(githubLogin, email string) bool {
	for _, entry := range strings.Split(os.Getenv("TOWERCRANE_ADMINS"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if (githubLogin != "" && strings.EqualFold(entry, githubLogin)) ||
			(email != "" && strings.EqualFold(entry, email)) {
			return true
		}
	}
	return false
}
