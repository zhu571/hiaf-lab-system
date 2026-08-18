package middleware

import "testing"

func TestMaintainerCanManageMembers(t *testing.T) {
	if !roleHasPermission("maintainer", PermManageMembers) {
		t.Fatal("maintainer must have manage_members")
	}
	if !roleHasPermission("owner", PermManageMembers) {
		t.Fatal("owner must retain manage_members")
	}
	if roleHasPermission("member", PermManageMembers) || roleHasPermission("viewer", PermManageMembers) {
		t.Fatal("member/viewer must not manage members")
	}
}
