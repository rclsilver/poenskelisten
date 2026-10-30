package mcpserver

import (
	"strings"
	"testing"
	"time"

	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"

	"github.com/google/uuid"
)

func TestCreateWishlistTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")

	var out wishlistWriteOut
	writeOK(t, owner.ID, "create_wishlist", map[string]any{"name": "  Birthday 2027  ", "description": " Things I like "}, &out)

	if out.Name != "Birthday 2027" || out.Description != "Things I like" || out.Role != "owner" {
		t.Errorf("unexpected output: %+v", out)
	}
	if !out.Claimable || out.Public || out.HideClaimers || out.PublicHash != "" || out.Date != "" {
		t.Errorf("unexpected defaults: %+v", out)
	}

	var stored models.Wishlist
	if r := database.Instance.First(&stored, "id = ?", out.ID); r.Error != nil {
		t.Fatalf("wishlist not stored: %v", r.Error)
	}
	if stored.OwnerID != owner.ID || !stored.Enabled || stored.Expires == nil || *stored.Expires {
		t.Errorf("unexpected stored wishlist: %+v", stored)
	}

	var listed []wishlistOut
	callTool(t, owner.ID, "list_wishlists", nil, &listed)
	if len(listed) != 1 || listed[0].ID != out.ID {
		t.Errorf("list_wishlists = %+v, want the new wishlist", listed)
	}
}

func TestCreateWishlistToolDateAndValidation(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")

	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	var out wishlistWriteOut
	writeOK(t, owner.ID, "create_wishlist", map[string]any{"name": "Dated list", "date": future, "public": true}, &out)
	if out.Date != future || !out.Public || out.Claimable || out.PublicHash == "" {
		t.Errorf("unexpected output for a dated public list: %+v", out)
	}

	for label, args := range map[string]map[string]any{
		"past date":            {"name": "Past list", "date": "2001-01-01"},
		"bad date":             {"name": "Bad date", "date": "tomorrow"},
		"too short":            {"name": "abc"},
		"forbidden character":  {"name": "Bad <name>"},
		"forbidden in desc":    {"name": "Fine name", "description": "a \"quote\""},
		"too long":             {"name": strings.Repeat("a", maxNameLength+1)},
		"public and claimable": {"name": "Both flags", "public": true, "claimable": true},
		"description too long": {"name": "Long desc", "description": strings.Repeat("a", maxDescriptionLength+1)},
		"bad group id":         {"name": "Group list", "group_ids": []string{"nope"}},
		"duplicate name":       {"name": "Dated list"},
	} {
		if msg := writeErr(t, owner.ID, "create_wishlist", args); msg == "" {
			t.Errorf("%s: empty error", label)
		}
	}

	var count int64
	database.Instance.Model(&models.Wishlist{}).Count(&count)
	if count != 1 {
		t.Errorf("wishlists stored = %d, want only the valid one", count)
	}
}

func TestCreateWishlistToolGroups(t *testing.T) {
	setupMCPDB(t)
	me := createMCPTestUser(t, "Ada")
	other := createMCPTestUser(t, "Bob")
	mine := shareWithGroup(t, uuid.Nil, me.ID, other.ID)
	notMine := shareWithGroup(t, uuid.Nil, other.ID)

	var out wishlistWriteOut
	writeOK(t, me.ID, "create_wishlist", map[string]any{"name": "Shared list", "group_ids": []string{mine.ID.String(), mine.ID.String()}}, &out)

	var memberships []models.WishlistMembership
	database.Instance.Where("wishlist_id = ?", out.ID).Find(&memberships)
	if len(memberships) != 1 || memberships[0].GroupID != mine.ID {
		t.Errorf("memberships = %+v, want exactly one, on my group", memberships)
	}

	var listed []wishlistOut
	callTool(t, other.ID, "list_wishlists", nil, &listed)
	if len(listed) != 1 || listed[0].Role != "group_member" {
		t.Errorf("a member of the group sees %+v, want the shared list", listed)
	}

	msg := writeErr(t, me.ID, "create_wishlist", map[string]any{"name": "Sneaky list", "group_ids": []string{notMine.ID.String()}})
	if !strings.Contains(msg, "not a member") {
		t.Errorf("error = %q, want a membership error", msg)
	}
	var count int64
	database.Instance.Model(&models.Wishlist{}).Where("name = ?", "Sneaky list").Count(&count)
	if count != 0 {
		t.Error("a wishlist was created despite the group being refused")
	}
}

func TestUpdateWishlistTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	createWritableWishlist(t, owner.ID, "Christmas")
	addCollaborator(t, wishlist.ID, collab.ID)
	id := wishlist.ID.String()

	var out wishlistWriteOut
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "name": "Birthday 2027", "description": "New"}, &out)
	if out.Name != "Birthday 2027" || out.Description != "New" || out.Role != "owner" || !out.Claimable {
		t.Errorf("unexpected output: %+v", out)
	}

	// A collaborator may edit, and the name stays unique among the OWNER's lists.
	writeOK(t, collab.ID, "update_wishlist", map[string]any{"wishlist_id": id, "hide_claimers": true}, &out)
	if !out.HideClaimers || out.Role != "collaborator" || out.Name != "Birthday 2027" {
		t.Errorf("collaborator update: %+v", out)
	}
	writeErr(t, collab.ID, "update_wishlist", map[string]any{"wishlist_id": id, "name": "Christmas"})

	date := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "description": "", "date": date}, &out)
	if out.Description != "" || out.Date != date {
		t.Errorf("clear description / set date: %+v", out)
	}
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "expires": false}, &out)
	if out.Date != "" {
		t.Errorf("expires=false must drop the date: %+v", out)
	}

	// Sending the current name again is not a conflict with itself.
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "name": "Birthday 2027"}, nil)

	for label, args := range map[string]map[string]any{
		"short name":  {"wishlist_id": id, "name": "abc"},
		"bad chars":   {"wishlist_id": id, "name": "Bad <name>"},
		"bad date":    {"wishlist_id": id, "date": "soon"},
		"bad id":      {"wishlist_id": "nope"},
		"description": {"wishlist_id": id, "description": "a `tick`"},
	} {
		if msg := writeErr(t, owner.ID, "update_wishlist", args); msg == "" {
			t.Errorf("%s: empty error", label)
		}
	}
}

func TestUpdateWishlistToolPublicLink(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	id := wishlist.ID.String()

	// The fixture is claimable, so going public alone is refused.
	writeErr(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "public": true})

	var first, second, third wishlistWriteOut
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "public": true, "claimable": false}, &first)
	if !first.Public || first.PublicHash == "" || first.PublicHash == wishlist.PublicHash.String() {
		t.Errorf("going public must issue a fresh link: %+v", first)
	}
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "description": "edit"}, &second)
	if second.PublicHash != first.PublicHash {
		t.Error("an ordinary edit must keep the public link")
	}
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "public": false}, nil)
	writeOK(t, owner.ID, "update_wishlist", map[string]any{"wishlist_id": id, "public": true}, &third)
	if third.PublicHash == first.PublicHash {
		t.Error("re-publishing must revoke the old link")
	}
}

func TestUpdateWishlistToolAccess(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	member := createMCPTestUser(t, "Bob")
	stranger := createMCPTestUser(t, "Erin")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	shareWithGroup(t, wishlist.ID, owner.ID, member.ID)
	args := map[string]any{"wishlist_id": wishlist.ID.String(), "name": "Hijacked name"}

	if msg := writeErr(t, member.ID, "update_wishlist", args); msg != errNotEditor.Error() {
		t.Errorf("group member error = %q", msg)
	}
	strangerMsg := writeErr(t, stranger.ID, "update_wishlist", args)
	missingMsg := writeErr(t, stranger.ID, "update_wishlist", map[string]any{"wishlist_id": uuid.NewString(), "name": "Hijacked name"})
	if strangerMsg != errNoAccess.Error() || strangerMsg != missingMsg {
		t.Errorf("stranger = %q, missing = %q; must be the same no-access error", strangerMsg, missingMsg)
	}

	var stored models.Wishlist
	database.Instance.First(&stored, "id = ?", wishlist.ID)
	if stored.Name != "Birthday" {
		t.Errorf("name changed to %q by an unauthorised caller", stored.Name)
	}
}

func TestDeleteWishlistTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	member := createMCPTestUser(t, "Cleo")
	stranger := createMCPTestUser(t, "Erin")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	shareWithGroup(t, wishlist.ID, owner.ID, member.ID)
	args := map[string]any{"wishlist_id": wishlist.ID.String()}

	for _, caller := range []models.User{collab, member} {
		if msg := writeErr(t, caller.ID, "delete_wishlist", args); msg != errNotOwner.Error() {
			t.Errorf("non-owner error = %q", msg)
		}
	}
	strangerMsg := writeErr(t, stranger.ID, "delete_wishlist", args)
	missingMsg := writeErr(t, stranger.ID, "delete_wishlist", map[string]any{"wishlist_id": uuid.NewString()})
	if strangerMsg != errNoAccess.Error() || strangerMsg != missingMsg {
		t.Errorf("stranger = %q, missing = %q", strangerMsg, missingMsg)
	}
	var stored models.Wishlist
	database.Instance.First(&stored, "id = ?", wishlist.ID)
	if !stored.Enabled {
		t.Fatal("wishlist disabled by a caller who is not the owner")
	}

	var out deletedOut
	writeOK(t, owner.ID, "delete_wishlist", args, &out)
	if !out.Deleted || out.ID != wishlist.ID.String() {
		t.Errorf("unexpected output: %+v", out)
	}

	// Soft delete only: the row is still there, disabled.
	database.Instance.First(&stored, "id = ?", wishlist.ID)
	if stored.ID != wishlist.ID || stored.Enabled {
		t.Errorf("stored = %+v, want the row kept with enabled=false", stored)
	}
	var listed []wishlistOut
	callTool(t, owner.ID, "list_wishlists", nil, &listed)
	if len(listed) != 0 {
		t.Errorf("deleted wishlist still listed: %+v", listed)
	}
	if msg := writeErr(t, owner.ID, "delete_wishlist", args); msg != errNoAccess.Error() {
		t.Errorf("second delete = %q, want not found", msg)
	}
}
