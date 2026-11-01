package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aunefyren/poenskelisten/controllers"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func createWritableWish(t *testing.T, wishlist models.Wishlist, author uuid.UUID, name string) models.Wish {
	t.Helper()
	wish := models.Wish{Name: name, Enabled: true, OwnerID: author, WishlistID: wishlist.ID}
	wish.ID = uuid.New()
	if r := database.Instance.Create(&wish); r.Error != nil {
		t.Fatalf("failed to create wish: %v", r.Error)
	}
	return wish
}

func TestCreateWishTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	id := wishlist.ID.String()

	var out wishWriteOut
	writeOK(t, owner.ID, "create_wish", map[string]any{
		"wishlist_id": id, "name": "  Headphones  ", "note": "Over-ear", "url": "https://example.com/hp",
		"price": 199.5, "category_name": "Music",
	}, &out)
	if out.Name != "Headphones" || out.Note != "Over-ear" || out.URL != "https://example.com/hp" ||
		out.Price == nil || *out.Price != 199.5 || out.Category != "Music" || out.WishlistID != id {
		t.Errorf("unexpected output: %+v", out)
	}
	if out.Claimed != nil || out.ClaimedBy != "" {
		t.Errorf("claim info leaked to the wishlist's owner: %+v", out)
	}

	var stored models.Wish
	database.Instance.First(&stored, "id = ?", out.ID)
	if stored.OwnerID != owner.ID || !stored.Enabled || stored.CategoryID == nil {
		t.Errorf("unexpected stored wish: %+v", stored)
	}

	// A collaborator adds one too, reusing the existing category by name.
	var second wishWriteOut
	writeOK(t, collab.ID, "create_wish", map[string]any{"wishlist_id": id, "name": "Keyboard", "category_name": "Music"}, &second)
	var categories int64
	database.Instance.Model(&models.WishCategory{}).Count(&categories)
	if categories != 1 {
		t.Errorf("categories = %d, want the existing one reused", categories)
	}
	stored = models.Wish{}
	database.Instance.First(&stored, "id = ?", second.ID)
	if stored.OwnerID != collab.ID {
		t.Errorf("wish author = %s, want the collaborator", stored.OwnerID)
	}

	var listed []wishOut
	callTool(t, owner.ID, "list_wishes", map[string]any{"wishlist_id": id}, &listed)
	if len(listed) != 2 {
		t.Errorf("list_wishes = %+v, want both wishes", listed)
	}
}

func TestCreateWishToolValidation(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	createWritableWish(t, wishlist, owner.ID, "Headphones")
	id := wishlist.ID.String()

	for label, args := range map[string]map[string]any{
		"short":       {"wishlist_id": id, "name": "abc"},
		"chars":       {"wishlist_id": id, "name": "Bad <name>"},
		"too long":    {"wishlist_id": id, "name": strings.Repeat("a", maxNameLength+1)},
		"duplicate":   {"wishlist_id": id, "name": "Headphones"},
		"bad note":    {"wishlist_id": id, "name": "Valid name", "note": "a `b`"},
		"long note":   {"wishlist_id": id, "name": "Valid name", "note": strings.Repeat("a", maxNoteLength+1)},
		"no scheme":   {"wishlist_id": id, "name": "Valid name", "url": "example.com"},
		"js url":      {"wishlist_id": id, "name": "Valid name", "url": "javascript:alert(1)"},
		"ftp url":     {"wishlist_id": id, "name": "Valid name", "url": "ftp://example.com/x"},
		"negative":    {"wishlist_id": id, "name": "Valid name", "price": -1},
		"short cat":   {"wishlist_id": id, "name": "Valid name", "category_name": "a"},
		"bad list id": {"wishlist_id": "nope", "name": "Valid name"},
	} {
		if msg := writeErr(t, owner.ID, "create_wish", args); msg == "" {
			t.Errorf("%s: empty error", label)
		}
	}

	var count int64
	database.Instance.Model(&models.Wish{}).Count(&count)
	if count != 1 {
		t.Errorf("wishes stored = %d, want only the fixture", count)
	}
}

func TestWishToolsAccess(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	member := createMCPTestUser(t, "Bob")
	stranger := createMCPTestUser(t, "Erin")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	shareWithGroup(t, wishlist.ID, owner.ID, member.ID)
	// The member authored this wish, yet the web app still reserves edits to
	// the wishlist's owner and collaborators.
	wish := createWritableWish(t, wishlist, member.ID, "Headphones")
	wishID := wish.ID.String()
	listID := wishlist.ID.String()

	attempts := []struct {
		tool string
		args map[string]any
	}{
		{"create_wish", map[string]any{"wishlist_id": listID, "name": "Something new"}},
		{"update_wish", map[string]any{"wish_id": wishID, "name": "Renamed wish"}},
		{"delete_wish", map[string]any{"wish_id": wishID}},
	}
	for _, a := range attempts {
		if msg := writeErr(t, member.ID, a.tool, a.args); msg != errNotEditor.Error() {
			t.Errorf("%s as group member: %q", a.tool, msg)
		}
		if msg := writeErr(t, stranger.ID, a.tool, a.args); msg != errNoAccess.Error() && msg != errNoWish.Error() {
			t.Errorf("%s as stranger: %q", a.tool, msg)
		}
	}

	// A stranger can't tell an existing wish from a missing one.
	a := writeErr(t, stranger.ID, "update_wish", map[string]any{"wish_id": wishID, "name": "Renamed wish"})
	b := writeErr(t, stranger.ID, "update_wish", map[string]any{"wish_id": uuid.NewString(), "name": "Renamed wish"})
	if a != b {
		t.Errorf("existing = %q, missing = %q; must be identical", a, b)
	}

	var stored models.Wish
	database.Instance.First(&stored, "id = ?", wish.ID)
	if stored.Name != "Headphones" || !stored.Enabled {
		t.Errorf("wish changed by an unauthorised caller: %+v", stored)
	}
	var count int64
	database.Instance.Model(&models.Wish{}).Count(&count)
	if count != 1 {
		t.Errorf("wishes = %d, want no creation by unauthorised callers", count)
	}
}

func TestUpdateWishTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")
	createWritableWish(t, wishlist, owner.ID, "Keyboard")
	id := wish.ID.String()

	var out wishWriteOut
	writeOK(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "name": "Better headphones", "price": 250.0, "url": "https://example.com/x", "category_name": "Music"}, &out)
	if out.Name != "Better headphones" || out.Price == nil || *out.Price != 250 || out.Category != "Music" || out.URL != "https://example.com/x" {
		t.Errorf("unexpected output: %+v", out)
	}

	// Untouched fields stay as they were, and a collaborator may edit.
	writeOK(t, collab.ID, "update_wish", map[string]any{"wish_id": id, "note": "Black"}, &out)
	if out.Note != "Black" || out.Name != "Better headphones" || out.Category != "Music" {
		t.Errorf("partial update lost data: %+v", out)
	}

	writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "name": "Keyboard"})
	writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "url": "nope"})
	writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "price": -3})
	writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": "nope"})
	writeOK(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "name": "Better headphones"}, nil)

	// Leaving the category empties it, so it is cleaned up like on the web.
	writeOK(t, owner.ID, "update_wish", map[string]any{"wish_id": id, "category_name": ""}, &out)
	if out.Category != "" {
		t.Errorf("category = %q, want none", out.Category)
	}
	var enabledCategories int64
	database.Instance.Model(&models.WishCategory{}).Where("enabled = ?", true).Count(&enabledCategories)
	if enabledCategories != 0 {
		t.Errorf("enabled categories = %d, want the emptied one disabled", enabledCategories)
	}
}

func TestDeleteWishTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")
	other := createWritableWish(t, wishlist, owner.ID, "Keyboard")

	var out deletedOut
	writeOK(t, collab.ID, "delete_wish", map[string]any{"wish_id": wish.ID.String()}, &out)
	if !out.Deleted || out.ID != wish.ID.String() {
		t.Errorf("unexpected output: %+v", out)
	}

	// Soft delete only: the row survives, disabled.
	var stored models.Wish
	database.Instance.First(&stored, "id = ?", wish.ID)
	if stored.ID != wish.ID || stored.Enabled {
		t.Errorf("stored = %+v, want the row kept with enabled=false", stored)
	}

	var listed []wishOut
	callTool(t, owner.ID, "list_wishes", map[string]any{"wishlist_id": wishlist.ID.String()}, &listed)
	if len(listed) != 1 || listed[0].ID != other.ID.String() {
		t.Errorf("list_wishes = %+v, want only the other wish", listed)
	}
	if msg := writeErr(t, owner.ID, "delete_wish", map[string]any{"wish_id": wish.ID.String()}); msg != errNoWish.Error() {
		t.Errorf("second delete = %q", msg)
	}
}

func TestWishToolsRefuseDisabledWishlist(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	addCollaborator(t, wishlist.ID, collab.ID)
	wish := createWritableWish(t, wishlist, owner.ID, "Headphones")
	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Update("enabled", false)

	// The collaboration row outlives the wishlist; it must not grant access.
	writeErr(t, collab.ID, "create_wish", map[string]any{"wishlist_id": wishlist.ID.String(), "name": "Something new"})
	writeErr(t, collab.ID, "update_wish", map[string]any{"wish_id": wish.ID.String(), "name": "Renamed wish"})
	writeErr(t, collab.ID, "delete_wish", map[string]any{"wish_id": wish.ID.String()})
}

// wishImageDataURI is a small valid PNG as the data URI the image fields take.
func wishImageDataURI(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func wishHasImage(t *testing.T, id string) bool {
	t.Helper()
	exists, err := controllers.CheckIfWishImageExists(uuid.MustParse(id))
	if err != nil {
		t.Fatalf("failed to check the wish image: %v", err)
	}
	return exists
}

func TestWishImageTools(t *testing.T) {
	setupMCPDB(t)
	t.Cleanup(controllers.UseWishImageDir(filepath.Join(t.TempDir(), "wishes")))
	owner := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	id := wishlist.ID.String()
	picture := wishImageDataURI(t)

	var created wishWriteOut
	writeOK(t, owner.ID, "create_wish", map[string]any{"wishlist_id": id, "name": "Headphones", "image": picture}, &created)
	if !wishHasImage(t, created.ID) {
		t.Fatal("create_wish did not store the image")
	}

	// Replacing keeps an image; deleting removes it.
	writeOK(t, owner.ID, "update_wish", map[string]any{"wish_id": created.ID, "image": picture}, nil)
	if !wishHasImage(t, created.ID) {
		t.Error("update_wish lost the image it replaced")
	}
	writeOK(t, owner.ID, "update_wish", map[string]any{"wish_id": created.ID, "image_delete": true}, nil)
	if wishHasImage(t, created.ID) {
		t.Error("image_delete left the image")
	}

	// Both at once is refused, and so is anything that isn't an image.
	if msg := writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": created.ID, "image": picture, "image_delete": true}); !strings.Contains(msg, "image_delete") {
		t.Errorf("image with image_delete = %q", msg)
	}
	writeErr(t, owner.ID, "update_wish", map[string]any{"wish_id": created.ID, "image": "not an image"})
	if wishHasImage(t, created.ID) {
		t.Error("a refused image was stored")
	}

	// A refused image on creation leaves no wish behind.
	writeErr(t, owner.ID, "create_wish", map[string]any{"wishlist_id": id, "name": "Keyboard", "image": "data:image/png;base64,AAAA"})
	var count int64
	database.Instance.Model(&models.Wish{}).Where("name = ?", "Keyboard").Count(&count)
	if count != 0 {
		t.Errorf("a wish was created with a refused image")
	}
}

// An image over the account's upload budget is refused before it is decoded
// or anything is written; a wish without one still goes through.
func TestWishImageToolsCountAgainstTheUploadBudget(t *testing.T) {
	setupMCPDB(t)
	t.Cleanup(controllers.UseWishImageDir(filepath.Join(t.TempDir(), "wishes")))
	owner := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")
	id := wishlist.ID.String()
	session := startMCPTestServerWithUploads(t, owner.ID, writeScopes, middlewares.NewUploadBudget(1, time.Minute))
	create := func(name, picture string) *sdkmcp.CallToolResult {
		args := map[string]any{"wishlist_id": id, "name": name}
		if picture != "" {
			args["image"] = picture
		}
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "create_wish", Arguments: args})
		if err != nil {
			t.Fatalf("create_wish failed: %v", err)
		}
		return result
	}

	if result := create("Headphones", wishImageDataURI(t)); result.IsError {
		t.Fatalf("first image refused: %s", firstText(t, result))
	}
	result := create("Keyboard", wishImageDataURI(t))
	if !result.IsError || firstText(t, result) != middlewares.TooManyUploads {
		t.Errorf("second image = %+v, want %q", result.Content, middlewares.TooManyUploads)
	}
	var count int64
	database.Instance.Model(&models.Wish{}).Where("name = ?", "Keyboard").Count(&count)
	if count != 0 {
		t.Error("a wish was created with an image over the budget")
	}
	if result := create("Mouse pad", ""); result.IsError {
		t.Errorf("a wish without an image was refused: %s", firstText(t, result))
	}
}
