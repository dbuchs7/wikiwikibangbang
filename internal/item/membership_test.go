package item

import (
	"errors"
	"slices"
	"testing"
)

func TestAccessIsListedAndTakenBack(t *testing.T) {
	s := permFixture(t)
	anna, pat, ben := s.As("anna"), s.As("pat"), s.As("ben")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))

	if _, err := anna.SpaceGrants("PLAT"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an editor lists the roles: %v", err)
	}
	grants, err := pat.SpaceGrants("PLAT")
	noErr(t, err)
	if !slices.Contains(grants, Grant{Principal: "anna", Role: Editor}) || !slices.Contains(grants, Grant{Principal: "ben", Role: Viewer}) {
		t.Fatalf("grants %v", grants)
	}
	if err := anna.Revoke("PLAT", "ben"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an editor revokes: %v", err)
	}
	noErr(t, pat.Revoke("PLAT", "ben"))
	if _, err := ben.Get(page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revoked viewer still reads: %v", err)
	}
	if err := pat.Revoke("PLAT", "ben"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking twice: %v", err)
	}

	// Through a group: dora edits OPS as a member of @ops, until she leaves.
	dora := s.As("dora")
	ops := mustItem(t)(dora.Create(NewItem{Space: "OPS", Type: "page", Title: "Pager"}))
	if g := s.Groups()["ops"]; !slices.Equal(g, []string{"dora"}) {
		t.Fatalf("groups %v", s.Groups())
	}
	noErr(t, s.RemoveFromGroup("root", "ops", "dora"))
	if _, err := dora.Get(ops.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a former member still reads: %v", err)
	}
	if _, ok := s.Groups()["ops"]; ok {
		t.Fatal("an empty group is listed")
	}

	// System roles; the last administrator keeps the role.
	noErr(t, s.GrantSystemRole("root", "root", RoleAdmin))
	if err := s.RevokeSystemRole("root", "root", RoleAdmin); !errors.Is(err, ErrValidation) {
		t.Fatalf("the last administrator lost the role: %v", err)
	}
	noErr(t, s.RevokeSystemRole("root", "erin", RoleErasure))
	if roles := s.SystemRoles(); len(roles["erin"]) != 0 || !slices.Equal(roles["root"], []string{RoleAdmin}) {
		t.Fatalf("system roles %v", roles)
	}
}
