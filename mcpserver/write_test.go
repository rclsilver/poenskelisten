package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const writeScopes = "mcp:wishlists.read mcp:wishlists.write"

// callWriteTool runs a tool with a token holding the given scopes and returns
// the raw result.
func callWriteTool(t *testing.T, userID uuid.UUID, scopes, name string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	session := startMCPTestServer(t, userID, scopes)
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	return result
}

// writeOK calls a write tool and decodes its JSON result into dst.
func writeOK(t *testing.T, userID uuid.UUID, name string, args map[string]any, dst any) {
	t.Helper()
	result := callWriteTool(t, userID, writeScopes, name, args)
	if result.IsError {
		t.Fatalf("%s reported an error: %s", name, firstText(t, result))
	}
	if dst != nil {
		// Decoding into a reused value would keep fields the result omits.
		reflect.ValueOf(dst).Elem().SetZero()
		if err := json.Unmarshal([]byte(firstText(t, result)), dst); err != nil {
			t.Fatalf("failed to parse %s result: %v", name, err)
		}
	}
}

// writeErr calls a write tool, requires an error result and returns its text.
func writeErr(t *testing.T, userID uuid.UUID, name string, args map[string]any) string {
	t.Helper()
	result := callWriteTool(t, userID, writeScopes, name, args)
	if !result.IsError {
		t.Fatalf("%s: expected an error result, got %s", name, firstText(t, result))
	}
	return firstText(t, result)
}

func createWritableWishlist(t *testing.T, owner uuid.UUID, name string) models.Wishlist {
	t.Helper()
	now := time.Now()
	wishlist := models.Wishlist{
		Name: name, Enabled: true, OwnerID: owner, Date: &now,
		Expires: ptrTo(false), Claimable: ptrTo(true), HideClaimers: ptrTo(false),
		Public: ptrTo(false), PublicHash: uuid.New(),
	}
	wishlist.ID = uuid.New()
	if r := database.Instance.Create(&wishlist); r.Error != nil {
		t.Fatalf("failed to create wishlist: %v", r.Error)
	}
	return wishlist
}

func addCollaborator(t *testing.T, wishlistID, userID uuid.UUID) {
	t.Helper()
	c := models.WishlistCollaborator{WishlistID: wishlistID, UserID: userID, Enabled: true}
	c.ID = uuid.New()
	if r := database.Instance.Create(&c); r.Error != nil {
		t.Fatalf("failed to create collaborator: %v", r.Error)
	}
}

// shareWithGroup creates a group holding the given members and, unless
// wishlistID is nil, attaches the wishlist to it.
func shareWithGroup(t *testing.T, wishlistID uuid.UUID, members ...uuid.UUID) models.Group {
	t.Helper()
	group := models.Group{Name: "Family", Enabled: true}
	group.ID = uuid.New()
	if r := database.Instance.Create(&group); r.Error != nil {
		t.Fatalf("failed to create group: %v", r.Error)
	}
	for _, m := range members {
		gm := models.GroupMembership{GroupID: group.ID, MemberID: m, Enabled: true}
		gm.ID = uuid.New()
		if r := database.Instance.Create(&gm); r.Error != nil {
			t.Fatalf("failed to create group membership: %v", r.Error)
		}
	}
	if wishlistID != uuid.Nil {
		wm := models.WishlistMembership{GroupID: group.ID, WishlistID: wishlistID, Enabled: true}
		wm.ID = uuid.New()
		if r := database.Instance.Create(&wm); r.Error != nil {
			t.Fatalf("failed to create wishlist membership: %v", r.Error)
		}
	}
	return group
}

func TestWriteToolsRequireWriteScope(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	wishlist := createWritableWishlist(t, owner.ID, "Birthday")

	calls := map[string]map[string]any{
		"create_wishlist": {"name": "Brand new"},
		"update_wishlist": {"wishlist_id": wishlist.ID.String(), "name": "Renamed list"},
		"delete_wishlist": {"wishlist_id": wishlist.ID.String()},
		"create_wish":     {"wishlist_id": wishlist.ID.String(), "name": "Headphones"},
		"update_wish":     {"wish_id": uuid.NewString(), "name": "Headphones"},
		"delete_wish":     {"wish_id": uuid.NewString()},
	}
	for name, args := range calls {
		result := callWriteTool(t, owner.ID, "mcp:wishlists.read", name, args)
		if !result.IsError {
			t.Errorf("%s succeeded without mcp:wishlists.write", name)
		}
	}

	var count int64
	database.Instance.Model(&models.Wishlist{}).Where("enabled = ?", true).Count(&count)
	if count != 1 {
		t.Errorf("enabled wishlists = %d, want the original one untouched", count)
	}
}

func TestWriteToolsAreAnnotated(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	session := startMCPTestServer(t, owner.ID, writeScopes)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	destructive := map[string]bool{}
	for _, tool := range tools.Tools {
		if tool.Annotations != nil && tool.Annotations.DestructiveHint != nil {
			destructive[tool.Name] = *tool.Annotations.DestructiveHint
		}
	}
	for _, name := range []string{"delete_wishlist", "delete_wish", "detach_wishlist_from_group"} {
		if !destructive[name] {
			t.Errorf("%s must carry destructiveHint=true", name)
		}
	}
	for _, name := range []string{"create_wishlist", "create_wish", "attach_wishlist_to_group"} {
		if v, ok := destructive[name]; !ok || v {
			t.Errorf("%s must carry destructiveHint=false", name)
		}
	}
}
