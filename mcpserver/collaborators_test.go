package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func collaboratorArgsOf(wishlistID, userID uuid.UUID) map[string]any {
	return map[string]any{"wishlist_id": wishlistID.String(), "user_id": userID.String()}
}

func liveCollaborators(t *testing.T, wishlistID uuid.UUID) int64 {
	t.Helper()
	var n int64
	database.Instance.Model(&models.WishlistCollaborator{}).Where("wishlist_id = ? AND enabled = ?", wishlistID, true).Count(&n)
	return n
}

func TestAddAndRemoveCollaborator(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	shareWithGroup(t, uuid.Nil, ada.ID, bob.ID)

	var out sharingOut
	writeOK(t, ada.ID, "add_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID), &out)
	if len(out.Collaborators) != 1 || out.Collaborators[0].ID != bob.ID.String() || out.Collaborators[0].Name != "Bob" {
		t.Fatalf("collaborators after add = %+v, want Bob", out.Collaborators)
	}
	if r, err := resolveRole(bob.ID, wishlist.ID); err != nil || r != roleCollaborator {
		t.Fatalf("bob's role = (%q, %v), want collaborator", r, err)
	}
	if msg := writeErr(t, ada.ID, "add_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID)); !strings.Contains(msg, "already a collaborator") {
		t.Errorf("second add = %q, want an 'already a collaborator' error", msg)
	}

	writeOK(t, ada.ID, "remove_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID), &out)
	if len(out.Collaborators) != 0 {
		t.Fatalf("collaborators after remove = %+v, want none", out.Collaborators)
	}
	if _, err := resolveRole(bob.ID, wishlist.ID); err != errNoAccess {
		t.Fatalf("bob still reaches the wishlist: %v", err)
	}
	if msg := writeErr(t, ada.ID, "remove_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID)); !strings.Contains(msg, "not a collaborator") {
		t.Errorf("second remove = %q, want a 'not a collaborator' error", msg)
	}
}

func TestAddCollaboratorOnlyReachesGroupMates(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	eve := createMCPTestUser(t, "Eve")
	gone := createMCPTestUser(t, "Gone")
	formerMate := createMCPTestUser(t, "Frank")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	shareWithGroup(t, uuid.Nil, ada.ID, gone.ID)
	database.Instance.Model(&models.User{}).Where("id = ?", gone.ID).Update("enabled", false)
	deleted := shareWithGroup(t, uuid.Nil, ada.ID, formerMate.ID)
	database.Instance.Model(&models.Group{}).Where("id = ?", deleted.ID).Update("enabled", false)

	for name, id := range map[string]uuid.UUID{"stranger": eve.ID, "disabled": gone.ID, "deleted group": formerMate.ID, "unknown": uuid.New()} {
		if msg := writeErr(t, ada.ID, "add_collaborator", collaboratorArgsOf(wishlist.ID, id)); msg != errNoUser.Error() {
			t.Errorf("%s: got %q, want %q", name, msg, errNoUser)
		}
	}
	if msg := writeErr(t, ada.ID, "add_collaborator", collaboratorArgsOf(wishlist.ID, ada.ID)); !strings.Contains(msg, "owner can't be a collaborator") {
		t.Errorf("adding oneself = %q, want the owner error", msg)
	}
	if n := liveCollaborators(t, wishlist.ID); n != 0 {
		t.Errorf("collaborators = %d, want 0", n)
	}
}

func TestCollaboratorToolsAreOwnerOnly(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	carol := createMCPTestUser(t, "Carol")
	eve := createMCPTestUser(t, "Eve")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	addCollaborator(t, wishlist.ID, carol.ID)
	shareWithGroup(t, wishlist.ID, ada.ID, bob.ID, carol.ID)
	// Eve is in a group with all of them, but it can't see this wishlist.
	shareWithGroup(t, uuid.Nil, ada.ID, bob.ID, carol.ID, eve.ID)

	cases := []struct {
		name   string
		userID uuid.UUID
		want   string
	}{
		{"collaborator", carol.ID, errNotOwner.Error()},
		{"group member", bob.ID, errNotOwner.Error()},
		{"stranger", eve.ID, errNoAccess.Error()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if msg := writeErr(t, c.userID, "add_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID)); msg != c.want {
				t.Errorf("add = %q, want %q", msg, c.want)
			}
			if msg := writeErr(t, c.userID, "remove_collaborator", collaboratorArgsOf(wishlist.ID, carol.ID)); msg != c.want {
				t.Errorf("remove = %q, want %q", msg, c.want)
			}
		})
	}
	if n := liveCollaborators(t, wishlist.ID); n != 1 {
		t.Errorf("collaborators = %d, want Carol only", n)
	}
}

func TestRemoveCollaboratorNoLongerReachable(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	// Bob shares no group with Ada any more; she can still take access back.
	addCollaborator(t, wishlist.ID, bob.ID)

	var out sharingOut
	writeOK(t, ada.ID, "remove_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID), &out)
	if n := liveCollaborators(t, wishlist.ID); n != 0 || len(out.Collaborators) != 0 {
		t.Errorf("collaborators = %d (%+v), want none", n, out.Collaborators)
	}
}

func TestCollaboratorToolsNeedWriteScope(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	shareWithGroup(t, uuid.Nil, ada.ID, bob.ID)

	if result := callWriteTool(t, ada.ID, "mcp:wishlists.read mcp:groups.read", "add_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID)); !result.IsError {
		t.Errorf("add_collaborator succeeded without mcp:wishlists.write")
	}
	addCollaborator(t, wishlist.ID, bob.ID)
	if result := callWriteTool(t, ada.ID, "mcp:wishlists.read mcp:groups.read", "remove_collaborator", collaboratorArgsOf(wishlist.ID, bob.ID)); !result.IsError {
		t.Errorf("remove_collaborator succeeded without mcp:wishlists.write")
	}
	if n := liveCollaborators(t, wishlist.ID); n != 1 {
		t.Errorf("collaborators = %d, want Bob untouched", n)
	}
}
