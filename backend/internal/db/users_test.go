package db

import (
	"testing"

	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
)

func TestGitHubRoleFromTeams(t *testing.T) {
	mappings := []RoleMapping{
		{GitHubOrg: "acme", TeamSlug: "infra-admins", Role: auth.RoleAdmin},
		{GitHubOrg: "acme", TeamSlug: "sre", Role: auth.RoleApprover},
	}
	if got := githubRole(`["acme/sre","acme/infra-admins"]`, mappings); got != auth.RoleAdmin {
		t.Errorf("got %q, want admin (highest mapped team)", got)
	}
	if got := githubRole(`["acme/sre"]`, mappings); got != auth.RoleApprover {
		t.Errorf("got %q, want approver", got)
	}
	if got := githubRole(`["other/infra-admins"]`, mappings); got != auth.RoleNone {
		t.Errorf("same team slug in another org must not match, got %q", got)
	}
	if got := githubRole(``, mappings); got != auth.RoleNone {
		t.Errorf("no teams stored must grant nothing, got %q", got)
	}
}
