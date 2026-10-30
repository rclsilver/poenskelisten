package mcpserver

import (
	"encoding/json"
	"testing"

	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"

	"github.com/google/uuid"
)

const claimScopes = "mcp:wishlists.read mcp:wishes.claim"

func claimOK(t *testing.T, userID uuid.UUID, tool, wishID string) claimOut {
	t.Helper()
	result := callWriteTool(t, userID, claimScopes, tool, map[string]any{"wish_id": wishID})
	if result.IsError {
		t.Fatalf("%s reported an error: %s", tool, firstText(t, result))
	}
	var out claimOut
	if err := json.Unmarshal([]byte(firstText(t, result)), &out); err != nil {
		t.Fatalf("bad %s result: %v", tool, err)
	}
	return out
}

func claimErr(t *testing.T, userID uuid.UUID, tool, wishID string) string {
	t.Helper()
	result := callWriteTool(t, userID, claimScopes, tool, map[string]any{"wish_id": wishID})
	if !result.IsError {
		t.Fatalf("%s: expected an error, got %s", tool, firstText(t, result))
	}
	return firstText(t, result)
}

func claimCount(t *testing.T, wishID uuid.UUID) int64 {
	t.Helper()
	var n int64
	database.Instance.Model(&models.WishClaim{}).Where("wish_id = ? AND enabled = ?", wishID, true).Count(&n)
	return n
}

func TestClaimAndUnclaimWish(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	carol := createMCPTestUser(t, "Carol")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	shareWithGroup(t, wishlist.ID, owner.ID, bob.ID, carol.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")
	id := wish.ID.String()

	if out := claimOK(t, bob.ID, "claim_wish", id); out.ID != id || !out.Claimed {
		t.Errorf("claim output = %+v", out)
	}
	if n := claimCount(t, wish.ID); n != 1 {
		t.Fatalf("claims = %d, want 1", n)
	}

	// Seen by the other member, with the claimer's name; never by the owner.
	var listed []wishOut
	callTool(t, carol.ID, "list_wishes", map[string]any{"wishlist_id": wishlist.ID.String()}, &listed)
	if len(listed) != 1 || listed[0].Claimed == nil || !*listed[0].Claimed || listed[0].ClaimedBy != "Bob" {
		t.Errorf("as member: %+v", listed)
	}
	var ownerView []wishOut
	callTool(t, owner.ID, "list_wishes", map[string]any{"wishlist_id": wishlist.ID.String()}, &ownerView)
	if len(ownerView) != 1 || ownerView[0].Claimed != nil || ownerView[0].ClaimedBy != "" {
		t.Errorf("claim leaked to the owner: %+v", ownerView)
	}

	// Only one claimer, and only the claimer can release it.
	claimErr(t, carol.ID, "claim_wish", id)
	claimErr(t, bob.ID, "claim_wish", id)
	if msg := claimErr(t, carol.ID, "unclaim_wish", id); msg == "" || claimCount(t, wish.ID) != 1 {
		t.Errorf("a non-claimer released the claim (%q)", msg)
	}

	if out := claimOK(t, bob.ID, "unclaim_wish", id); out.ID != id || out.Claimed {
		t.Errorf("unclaim output = %+v", out)
	}
	if n := claimCount(t, wish.ID); n != 0 {
		t.Errorf("claims = %d, want 0", n)
	}
	claimErr(t, bob.ID, "unclaim_wish", id)

	// Once released, someone else may take it.
	claimOK(t, carol.ID, "claim_wish", id)
}

func TestClaimWishRules(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Cleo")
	member := createMCPTestUser(t, "Bob")
	stranger := createMCPTestUser(t, "Erin")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	shareWithGroup(t, wishlist.ID, owner.ID, member.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")
	own := createWritableWish(t, wishlist, member.ID, "Bob's own idea")
	id := wish.ID.String()

	for label, uid := range map[string]uuid.UUID{"owner": owner.ID, "collaborator": collab.ID} {
		if msg := claimErr(t, uid, "claim_wish", id); msg == errNoWish.Error() {
			t.Errorf("%s: got the not-found error, want an explicit refusal", label)
		}
	}
	claimErr(t, member.ID, "claim_wish", own.ID.String())

	// A stranger can't tell an existing wish from a missing one.
	a := claimErr(t, stranger.ID, "claim_wish", id)
	b := claimErr(t, stranger.ID, "claim_wish", uuid.NewString())
	if a != errNoWish.Error() || a != b {
		t.Errorf("existing = %q, missing = %q; want identical %q", a, b, errNoWish)
	}
	if a := claimErr(t, stranger.ID, "unclaim_wish", id); a != errNoWish.Error() {
		t.Errorf("unclaim as stranger = %q", a)
	}
	claimErr(t, member.ID, "claim_wish", "nope")

	// Not claimable.
	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Update("claimable", false)
	claimErr(t, member.ID, "claim_wish", id)
	if n := claimCount(t, wish.ID); n != 0 {
		t.Errorf("claims = %d, want none", n)
	}

	// Disabled wish.
	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Update("claimable", true)
	database.Instance.Model(&models.Wish{}).Where("id = ?", wish.ID).Update("enabled", false)
	if msg := claimErr(t, member.ID, "claim_wish", id); msg != errNoWish.Error() {
		t.Errorf("disabled wish = %q", msg)
	}
}

func TestClaimWishNeedsScope(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	member := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	shareWithGroup(t, wishlist.ID, owner.ID, member.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")

	for _, tool := range []string{"claim_wish", "unclaim_wish"} {
		result := callWriteTool(t, member.ID, writeScopes, tool, map[string]any{"wish_id": wish.ID.String()})
		if !result.IsError {
			t.Errorf("%s worked without mcp:wishes.claim", tool)
		}
	}
	if n := claimCount(t, wish.ID); n != 0 {
		t.Errorf("claims = %d, want none", n)
	}
}

func TestClaimedByHiddenWhenClaimersHidden(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	carol := createMCPTestUser(t, "Carol")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Update("hide_claimers", true)
	shareWithGroup(t, wishlist.ID, owner.ID, bob.ID, carol.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")

	claimOK(t, bob.ID, "claim_wish", wish.ID.String())
	var listed []wishOut
	callTool(t, carol.ID, "list_wishes", map[string]any{"wishlist_id": wishlist.ID.String()}, &listed)
	if len(listed) != 1 || listed[0].Claimed == nil || !*listed[0].Claimed || listed[0].ClaimedBy == "Bob" {
		t.Errorf("hide_claimers not respected: %+v", listed)
	}
}
