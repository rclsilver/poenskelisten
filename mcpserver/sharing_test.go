package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func sharingArgs(wishlistID, groupID uuid.UUID) map[string]any {
	return map[string]any{"wishlist_id": wishlistID.String(), "group_id": groupID.String()}
}

func liveShares(t *testing.T, wishlistID uuid.UUID) int64 {
	t.Helper()
	var n int64
	database.Instance.Model(&models.WishlistMembership{}).Where("wishlist_id = ? AND enabled = ?", wishlistID, true).Count(&n)
	return n
}

func TestAttachAndDetachWishlist(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	group := shareWithGroup(t, uuid.Nil, ada.ID, bob.ID)

	var out sharingOut
	writeOK(t, ada.ID, "attach_wishlist_to_group", sharingArgs(wishlist.ID, group.ID), &out)
	if len(out.Groups) != 1 || out.Groups[0].ID != group.ID.String() || out.Groups[0].Name != group.Name || !out.Groups[0].Member {
		t.Fatalf("groups after attach = %+v, want the group", out.Groups)
	}
	if r, err := resolveRole(bob.ID, wishlist.ID); err != nil || r != roleGroupMember {
		t.Fatalf("bob's role after attach = (%q, %v), want group_member", r, err)
	}

	if msg := writeErr(t, ada.ID, "attach_wishlist_to_group", sharingArgs(wishlist.ID, group.ID)); !strings.Contains(msg, "already shared") {
		t.Errorf("second attach = %q, want an 'already shared' error", msg)
	}
	if n := liveShares(t, wishlist.ID); n != 1 {
		t.Errorf("live shares = %d, want 1", n)
	}

	writeOK(t, ada.ID, "detach_wishlist_from_group", sharingArgs(wishlist.ID, group.ID), &out)
	if len(out.Groups) != 0 {
		t.Fatalf("groups after detach = %+v, want none", out.Groups)
	}
	if _, err := resolveRole(bob.ID, wishlist.ID); err != errNoAccess {
		t.Fatalf("bob still reaches the wishlist after detach: %v", err)
	}
	if msg := writeErr(t, ada.ID, "detach_wishlist_from_group", sharingArgs(wishlist.ID, group.ID)); !strings.Contains(msg, "not shared") {
		t.Errorf("second detach = %q, want a 'not shared' error", msg)
	}
}

func TestAttachWishlistRequiresGroupMembership(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	eve := createMCPTestUser(t, "Eve")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	foreign := shareWithGroup(t, uuid.Nil, eve.ID)
	deleted := shareWithGroup(t, uuid.Nil, ada.ID)
	database.Instance.Model(&models.Group{}).Where("id = ?", deleted.ID).Update("enabled", false)

	for _, groupID := range []uuid.UUID{foreign.ID, deleted.ID, uuid.New()} {
		if msg := writeErr(t, ada.ID, "attach_wishlist_to_group", sharingArgs(wishlist.ID, groupID)); msg != errNoGroup.Error() {
			t.Errorf("attach to %s = %q, want %q", groupID, msg, errNoGroup)
		}
	}
	if n := liveShares(t, wishlist.ID); n != 0 {
		t.Errorf("live shares = %d, want 0", n)
	}
}

func TestSharingToolsAreOwnerOnly(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	carol := createMCPTestUser(t, "Carol")
	bob := createMCPTestUser(t, "Bob")
	eve := createMCPTestUser(t, "Eve")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	addCollaborator(t, wishlist.ID, carol.ID)
	group := shareWithGroup(t, wishlist.ID, ada.ID, bob.ID, carol.ID)
	other := shareWithGroup(t, uuid.Nil, ada.ID, bob.ID, carol.ID)

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
			if msg := writeErr(t, c.userID, "get_wishlist_sharing", map[string]any{"wishlist_id": wishlist.ID.String()}); msg != c.want {
				t.Errorf("get_wishlist_sharing = %q, want %q", msg, c.want)
			}
			if msg := writeErr(t, c.userID, "attach_wishlist_to_group", sharingArgs(wishlist.ID, other.ID)); msg != c.want {
				t.Errorf("attach = %q, want %q", msg, c.want)
			}
			if msg := writeErr(t, c.userID, "detach_wishlist_from_group", sharingArgs(wishlist.ID, group.ID)); msg != c.want {
				t.Errorf("detach = %q, want %q", msg, c.want)
			}
		})
	}
	// A stranger can't tell a real wishlist from a made-up one.
	if msg := writeErr(t, eve.ID, "get_wishlist_sharing", map[string]any{"wishlist_id": uuid.NewString()}); msg != errNoAccess.Error() {
		t.Errorf("unknown wishlist = %q, want %q", msg, errNoAccess)
	}
	if n := liveShares(t, wishlist.ID); n != 1 {
		t.Errorf("live shares = %d, want the original one", n)
	}
}

func TestGetWishlistSharing(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	carol := createMCPTestUser(t, "Carol")
	gone := createMCPTestUser(t, "Gone")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	addCollaborator(t, wishlist.ID, carol.ID)
	addCollaborator(t, wishlist.ID, gone.ID)
	database.Instance.Model(&models.User{}).Where("id = ?", gone.ID).Update("enabled", false)
	joined := shareWithGroup(t, wishlist.ID, ada.ID)
	// A share with a group Ada isn't in, as left by older versions when she
	// left it: she must see it to undo it, without learning the group's name.
	left := shareWithGroup(t, wishlist.ID, carol.ID)
	database.Instance.Model(&models.Group{}).Where("id = ?", left.ID).Update("name", "Secret circle")

	var out sharingOut
	writeOK(t, ada.ID, "get_wishlist_sharing", map[string]any{"wishlist_id": wishlist.ID.String()}, &out)
	if out.Public || out.PublicHash != "" {
		t.Errorf("private wishlist reports public=%v hash=%q", out.Public, out.PublicHash)
	}
	if len(out.Collaborators) != 1 || out.Collaborators[0].ID != carol.ID.String() || out.Collaborators[0].Name != "Carol" {
		t.Errorf("collaborators = %+v, want Carol only", out.Collaborators)
	}
	byID := map[string]sharedGroupOut{}
	for _, g := range out.Groups {
		byID[g.ID] = g
	}
	if g := byID[joined.ID.String()]; len(out.Groups) != 2 || !g.Member || g.Name == "" {
		t.Errorf("groups = %+v, want the joined group named and the left one", out.Groups)
	}
	if g, ok := byID[left.ID.String()]; !ok || g.Member || g.Name != "" {
		t.Errorf("left group = %+v, want member=false without a name", g)
	}

	writeOK(t, ada.ID, "detach_wishlist_from_group", sharingArgs(wishlist.ID, left.ID), &out)
	if len(out.Groups) != 1 || out.Groups[0].ID != joined.ID.String() {
		t.Errorf("groups after detaching the left group = %+v, want the joined one", out.Groups)
	}

	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Updates(map[string]any{"public": true, "claimable": false})
	writeOK(t, ada.ID, "get_wishlist_sharing", map[string]any{"wishlist_id": wishlist.ID.String()}, &out)
	if !out.Public || out.PublicHash != wishlist.PublicHash.String() {
		t.Errorf("public wishlist = public=%v hash=%q, want its hash", out.Public, out.PublicHash)
	}
}

func TestSharingToolsNeedScopes(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, ada.ID, "Birthday")
	group := shareWithGroup(t, uuid.Nil, ada.ID)

	for _, name := range []string{"attach_wishlist_to_group", "detach_wishlist_from_group"} {
		if result := callWriteTool(t, ada.ID, "mcp:wishlists.read mcp:groups.read", name, sharingArgs(wishlist.ID, group.ID)); !result.IsError {
			t.Errorf("%s succeeded without mcp:wishlists.write", name)
		}
	}
	if result := callWriteTool(t, ada.ID, "mcp:wishlists.write", "get_wishlist_sharing", map[string]any{"wishlist_id": wishlist.ID.String()}); !result.IsError {
		t.Errorf("get_wishlist_sharing succeeded without mcp:wishlists.read")
	}
	if n := liveShares(t, wishlist.ID); n != 0 {
		t.Errorf("live shares = %d, want 0", n)
	}
}
