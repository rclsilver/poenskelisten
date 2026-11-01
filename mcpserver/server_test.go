package mcpserver

import (
	pauth "aunefyren/poenskelisten/auth"
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupMCPConfig(t *testing.T) {
	t.Helper()
	original := config.ConfigFile
	t.Cleanup(func() { config.ConfigFile = original })
	keyPEM, kid, err := config.GenerateOAuthSigningKey(config.OAuthAlgES256)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	config.ConfigFile.OAuthSigningKey = keyPEM
	config.ConfigFile.OAuthSigningKeyID = kid
	config.ConfigFile.PoenskelistenExternalURL = "https://wishlist.example.com"
}

func TestContainsScope(t *testing.T) {
	scopes := []string{"mcp:wishlists.read", "mcp:groups.read"}
	if !containsScope(scopes, "mcp:groups.read") {
		t.Error("expected scope to be found")
	}
	if containsScope(scopes, "mcp:wishlists.write") {
		t.Error("did not expect scope to be found")
	}
}

func TestVerifyTokenValid(t *testing.T) {
	setupMCPConfig(t)
	userID := uuid.New()

	token, err := pauth.GenerateOAuthAccessToken(userID, "mcp-client", config.MCPResource(), "mcp:wishlists.read mcp:groups.read", false, true)
	if err != nil {
		t.Fatalf("GenerateOAuthAccessToken error: %v", err)
	}

	info, err := verifyToken(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("verifyToken error: %v", err)
	}
	if info.UserID != userID.String() {
		t.Errorf("UserID = %q, want %q", info.UserID, userID.String())
	}
	if !containsScope(info.Scopes, "mcp:wishlists.read") {
		t.Errorf("scopes = %v, want to include mcp:wishlists.read", info.Scopes)
	}
}

func TestVerifyTokenWrongAudience(t *testing.T) {
	setupMCPConfig(t)

	// A token for the API resource must not be accepted by the MCP resource server.
	token, err := pauth.GenerateOAuthAccessToken(uuid.New(), "mcp-client", config.APIResource(), "openid", false, true)
	if err != nil {
		t.Fatalf("GenerateOAuthAccessToken error: %v", err)
	}
	if _, err := verifyToken(context.Background(), token, nil); err == nil {
		t.Error("verifyToken accepted a token for the wrong audience")
	}
}

func TestVerifyTokenGarbage(t *testing.T) {
	setupMCPConfig(t)
	if _, err := verifyToken(context.Background(), "not-a-token", nil); err == nil {
		t.Error("verifyToken accepted garbage")
	}
}

// contextWithTokenInfo returns a context carrying info exactly the way the
// SDK's bearer middleware attaches it (the context key is unexported), so the
// tool functions can be called directly with any token shape.
func contextWithTokenInfo(t *testing.T, info *mcpauth.TokenInfo) context.Context {
	t.Helper()
	var captured context.Context
	verifier := func(context.Context, string, *http.Request) (*mcpauth.TokenInfo, error) { return info, nil }
	handler := mcpauth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = r.Context()
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer irrelevant")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if captured == nil {
		t.Fatal("bearer middleware did not reach the inner handler")
	}
	return captured
}

func TestRequireScope(t *testing.T) {
	userID := uuid.New()
	expiry := time.Now().Add(time.Hour)

	cases := []struct {
		name    string
		ctx     context.Context
		wantErr string
	}{
		{"no token info", context.Background(), "not authenticated"},
		{"missing scope", contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: userID.String(), Scopes: []string{scopeGroupsRead}, Expiration: expiry}), "missing required scope"},
		{"subject not a UUID", contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: "not-a-uuid", Scopes: []string{scopeWishlistsRead}, Expiration: expiry}), "invalid token subject"},
		{"nil subject", contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: uuid.Nil.String(), Scopes: []string{scopeWishlistsRead}, Expiration: expiry}), "invalid token subject"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := requireScope(c.ctx, scopeWishlistsRead)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("requireScope error = %v, want it to contain %q", err, c.wantErr)
			}
			if got != uuid.Nil {
				t.Errorf("requireScope userID = %v, want uuid.Nil on failure", got)
			}
		})
	}

	ctx := contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: userID.String(), Scopes: []string{scopeWishlistsRead}, Expiration: expiry})
	if got, err := requireScope(ctx, scopeWishlistsRead); err != nil || got != userID {
		t.Errorf("requireScope = (%v, %v), want (%v, nil)", got, err, userID)
	}
}

// TestToolsRejectMissingScope calls each tool directly with a token lacking
// the scope it needs; none of them may touch the database.
func TestToolsRejectMissingScope(t *testing.T) {
	ctx := contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: uuid.NewString(), Scopes: []string{"unrelated"}, Expiration: time.Now().Add(time.Hour)})

	if res, _, err := listWishlists(ctx, nil, noArgs{}); err == nil || res != nil {
		t.Errorf("listWishlists = (%v, %v), want nil result and a scope error", res, err)
	}
	if res, _, err := listWishes(ctx, nil, listWishesArgs{WishlistID: uuid.NewString()}); err == nil || res != nil {
		t.Errorf("listWishes = (%v, %v), want nil result and a scope error", res, err)
	}
	if res, _, err := listGroups(ctx, nil, noArgs{}); err == nil || res != nil {
		t.Errorf("listGroups = (%v, %v), want nil result and a scope error", res, err)
	}
}

// TestToolsSurfaceDatabaseFailures drops the table each tool reads so the
// query fails, and checks the tool returns its generic error rather than a
// partial or empty result.
func TestToolsSurfaceDatabaseFailures(t *testing.T) {
	allScopes := []string{scopeWishlistsRead, scopeGroupsRead}

	t.Run("list_wishlists", func(t *testing.T) {
		setupMCPDB(t)
		owner := createMCPTestUser(t, "Ada")
		if err := database.Instance.Migrator().DropTable(&models.Wishlist{}); err != nil {
			t.Fatalf("failed to drop wishlists: %v", err)
		}
		ctx := contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: owner.ID.String(), Scopes: allScopes, Expiration: time.Now().Add(time.Hour)})

		res, _, err := listWishlists(ctx, nil, noArgs{})
		if err == nil || err.Error() != "failed to load wishlists" || res != nil {
			t.Errorf("listWishlists = (%v, %v), want 'failed to load wishlists'", res, err)
		}
	})

	t.Run("list_wishes", func(t *testing.T) {
		setupMCPDB(t)
		owner := createMCPTestUser(t, "Ada")
		now := time.Now()
		wishlist := models.Wishlist{Name: "Birthday", Enabled: true, OwnerID: owner.ID, Date: &now}
		wishlist.ID = uuid.New()
		if r := database.Instance.Create(&wishlist); r.Error != nil {
			t.Fatalf("failed to create wishlist: %v", r.Error)
		}
		// Ownership still verifies against wishlists; only the wish query fails.
		if err := database.Instance.Migrator().DropTable(&models.Wish{}); err != nil {
			t.Fatalf("failed to drop wishes: %v", err)
		}
		ctx := contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: owner.ID.String(), Scopes: allScopes, Expiration: time.Now().Add(time.Hour)})

		res, _, err := listWishes(ctx, nil, listWishesArgs{WishlistID: wishlist.ID.String()})
		if err == nil || err.Error() != "failed to load wishes" || res != nil {
			t.Errorf("listWishes = (%v, %v), want 'failed to load wishes'", res, err)
		}
	})

	t.Run("list_groups", func(t *testing.T) {
		setupMCPDB(t)
		owner := createMCPTestUser(t, "Ada")
		if err := database.Instance.Migrator().DropTable(&models.Group{}); err != nil {
			t.Fatalf("failed to drop groups: %v", err)
		}
		ctx := contextWithTokenInfo(t, &mcpauth.TokenInfo{UserID: owner.ID.String(), Scopes: allScopes, Expiration: time.Now().Add(time.Hour)})

		res, _, err := listGroups(ctx, nil, noArgs{})
		if err == nil || err.Error() != "failed to load groups" || res != nil {
			t.Errorf("listGroups = (%v, %v), want 'failed to load groups'", res, err)
		}
	})
}

// snapshotMCPTables dumps every row of the tables the tools touch, so a test
// can check that nothing at all was written.
func snapshotMCPTables(t *testing.T) string {
	t.Helper()
	var dump strings.Builder
	for _, table := range []string{"users", "wishlists", "wishes", "groups", "group_memberships", "wishlist_memberships", "wishlist_collaborators", "wish_claims", "wish_categories"} {
		rows := []map[string]interface{}{}
		if err := database.Instance.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatalf("failed to read %s: %v", table, err)
		}
		fmt.Fprintf(&dump, "%s: %v\n", table, rows)
	}
	return dump.String()
}

// The nil UUID once made every lookup match whatever record came first, so a
// caller owning one wishlist could read and change everyone's. Every tool
// must refuse it, whichever argument carries it, and change nothing.
func TestToolsRefuseNilID(t *testing.T) {
	setupMCPDB(t)
	nilID := uuid.Nil.String()

	caller := createMCPTestUser(t, "Erin")
	callerWishlist := createMCPWishlist(t, caller, "Caller list", false)
	createMCPWish(t, caller, callerWishlist, "Caller wish")
	callerGroup := shareWithGroup(t, callerWishlist.ID, caller.ID)

	victim := createMCPTestUser(t, "Victim")
	claimer := createMCPTestUser(t, "Claimer")
	victimWishlist := createMCPWishlist(t, victim, "Victim list", false)
	victimWish := createMCPWish(t, victim, victimWishlist, "Victim wish")
	createMCPClaim(t, claimer, victimWish)
	createMCPCollaborator(t, claimer, victimWishlist)
	victimGroup := shareWithGroup(t, victimWishlist.ID, victim.ID, claimer.ID)
	publicWishlist := createMCPWishlist(t, victim, "Victim public list", false)
	if r := database.Instance.Model(&publicWishlist).Updates(map[string]interface{}{"public": true, "claimable": false, "public_hash": uuid.NewString()}); r.Error != nil {
		t.Fatalf("failed to publish wishlist: %v", r.Error)
	}

	before := snapshotMCPTables(t)
	scopes := strings.Join([]string{scopeWishlistsRead, scopeGroupsRead, scopeWishlistsWrite, scopeWishesClaim}, " ")
	session := startMCPTestServer(t, caller.ID, scopes)

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"list_wishes", map[string]any{"wishlist_id": nilID}},
		{"get_public_wishlist", map[string]any{"public_hash": nilID}},
		{"list_group_members", map[string]any{"group_id": nilID}},
		{"get_wishlist_sharing", map[string]any{"wishlist_id": nilID}},
		{"attach_wishlist_to_group", map[string]any{"wishlist_id": nilID, "group_id": callerGroup.ID.String()}},
		{"attach_wishlist_to_group", map[string]any{"wishlist_id": callerWishlist.ID.String(), "group_id": nilID}},
		{"detach_wishlist_from_group", map[string]any{"wishlist_id": nilID, "group_id": victimGroup.ID.String()}},
		{"detach_wishlist_from_group", map[string]any{"wishlist_id": callerWishlist.ID.String(), "group_id": nilID}},
		{"add_collaborator", map[string]any{"wishlist_id": nilID, "user_id": caller.ID.String()}},
		{"add_collaborator", map[string]any{"wishlist_id": callerWishlist.ID.String(), "user_id": nilID}},
		{"remove_collaborator", map[string]any{"wishlist_id": nilID, "user_id": claimer.ID.String()}},
		{"remove_collaborator", map[string]any{"wishlist_id": callerWishlist.ID.String(), "user_id": nilID}},
		{"create_wishlist", map[string]any{"name": "Nil group list", "group_ids": []string{nilID}}},
		{"update_wishlist", map[string]any{"wishlist_id": nilID, "name": "Renamed list"}},
		{"delete_wishlist", map[string]any{"wishlist_id": nilID}},
		{"create_wish", map[string]any{"wishlist_id": nilID, "name": "Nil wish name"}},
		{"update_wish", map[string]any{"wish_id": nilID, "name": "Renamed wish"}},
		{"delete_wish", map[string]any{"wish_id": nilID}},
		{"claim_wish", map[string]any{"wish_id": nilID}},
		{"unclaim_wish", map[string]any{"wish_id": nilID}},
	}
	for _, c := range cases {
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil {
			t.Fatalf("%s %v: transport error %v", c.tool, c.args, err)
		}
		text := firstText(t, result)
		if !result.IsError {
			t.Errorf("%s %v succeeded with %s, want an error", c.tool, c.args, text)
		}
		for _, secret := range []string{"Victim", "Claimer", victimWishlist.ID.String(), publicWishlist.ID.String()} {
			if strings.Contains(text, secret) {
				t.Errorf("%s %v leaked %q: %s", c.tool, c.args, secret, text)
			}
		}
	}

	nilSession := startMCPTestServer(t, uuid.Nil, scopes)
	for _, tool := range []string{"list_wishlists", "list_groups"} {
		result, err := nilSession.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
		if err == nil && !result.IsError {
			t.Errorf("%s with a token for the nil UUID succeeded with %s, want an error", tool, firstText(t, result))
		}
	}

	if after := snapshotMCPTables(t); after != before {
		t.Errorf("tools called with the nil UUID changed the database:\nbefore %s\nafter  %s", before, after)
	}
}
