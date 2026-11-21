package auth

import "testing"

func TestEffectiveRole(t *testing.T) {
	tests := []struct {
		name string
		src  RoleSources
		want Role
	}{
		{"nothing assigned is member", RoleSources{}, RoleMember},
		{"local approver", RoleSources{Local: RoleApprover}, RoleApprover},
		{"github admin beats local member", RoleSources{Local: RoleMember, GitHub: RoleAdmin}, RoleAdmin},
		{"local admin beats github approver", RoleSources{Local: RoleAdmin, GitHub: RoleApprover}, RoleAdmin},
		{"bootstrap is always admin", RoleSources{Bootstrap: true}, RoleAdmin},
		{"unknown role is ignored", RoleSources{Local: Role("superuser")}, RoleMember},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.src.Effective(); got != tt.want {
				t.Errorf("Effective() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAtLeast(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleApprover) || !RoleApprover.AtLeast(RoleApprover) {
		t.Error("admin and approver must be allowed to approve")
	}
	if RoleMember.AtLeast(RoleApprover) || RoleApprover.AtLeast(RoleAdmin) {
		t.Error("lower roles must not pass higher requirements")
	}
	if Role("bogus").AtLeast(RoleMember) {
		t.Error("unknown roles must not pass any requirement")
	}
}

func TestParseRole(t *testing.T) {
	for _, ok := range []string{"member", "approver", "admin"} {
		if _, valid := ParseRole(ok); !valid {
			t.Errorf("ParseRole(%q) should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Admin", "root"} {
		if _, valid := ParseRole(bad); valid {
			t.Errorf("ParseRole(%q) should be invalid", bad)
		}
	}
}

func TestIsBootstrapAdmin(t *testing.T) {
	t.Setenv("TOWERCRANE_ADMINS", " alice , ops@example.com,")
	if !IsBootstrapAdmin("Alice", "") || !IsBootstrapAdmin("", "OPS@example.com") {
		t.Error("listed login/email must be bootstrap admins (case-insensitive)")
	}
	if IsBootstrapAdmin("", "") || IsBootstrapAdmin("bob", "bob@example.com") {
		t.Error("unlisted or empty identities must not be bootstrap admins")
	}
}
