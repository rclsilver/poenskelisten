// Package mcpserver exposes Pønskelisten as an authenticated MCP (Model Context
// Protocol) resource server. It is an OAuth 2.1 resource server: the MCP endpoint
// is guarded by the SDK's bearer-token middleware, which validates our ES256
// access tokens (audience = the MCP resource) and returns the RFC 9728
// WWW-Authenticate challenge so clients can discover the authorization server.
package mcpserver

import (
	pauth "aunefyren/poenskelisten/auth"
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/controllers"
	"aunefyren/poenskelisten/database"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	scopeWishlistsRead = "mcp:wishlists.read"
	scopeGroupsRead    = "mcp:groups.read"
)

// noArgs is the input schema for tools that take no arguments.
type noArgs struct{}

type listWishesArgs struct {
	WishlistID string `json:"wishlist_id" jsonschema:"the ID of the wishlist whose wishes to list"`
}

type publicWishlistArgs struct {
	PublicHash string `json:"public_hash" jsonschema:"the public hash from a shared public wishlist link"`
}

// Output shapes — deliberately trimmed so internal fields never reach the client.
type wishlistOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Date        string `json:"date,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Role        string `json:"role,omitempty"`
}

type wishOut struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Note  string   `json:"note,omitempty"`
	URL   string   `json:"url,omitempty"`
	Price *float64 `json:"price,omitempty"`
	// Claimed and ClaimedBy are omitted when the caller isn't allowed to know
	// (owner, collaborators); Claimed=false then really means "unclaimed".
	Claimed   *bool  `json:"claimed,omitempty"`
	ClaimedBy string `json:"claimed_by,omitempty"`
}

type publicWishlistOut struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Date        string    `json:"date,omitempty"`
	Owner       string    `json:"owner,omitempty"`
	Wishes      []wishOut `json:"wishes"`
}

type groupOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Handler builds the MCP server and returns a gin handler that guards it with
// OAuth bearer validation. Serves 404 when MCP is disabled.
func Handler() gin.HandlerFunc {
	server := buildServer()

	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	protected := mcpauth.RequireBearerToken(verifyToken, &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: config.OAuthIssuer() + "/.well-known/oauth-protected-resource",
	})(streamable)

	return func(ctx *gin.Context) {
		if !config.ConfigFile.MCPEnabled {
			ctx.JSON(http.StatusNotFound, gin.H{"error": "MCP is not enabled."})
			return
		}
		protected.ServeHTTP(ctx.Writer, ctx.Request)
	}
}

// verifyToken validates an OAuth access token for the MCP resource and returns
// its identity + scopes for the SDK to attach to the request.
func verifyToken(_ context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	claims, err := pauth.ValidateOAuthAccessToken(token, config.MCPResource())
	if err != nil {
		return nil, mcpauth.ErrInvalidToken
	}
	info := &mcpauth.TokenInfo{
		UserID: claims.Subject,
		Scopes: strings.Fields(claims.Scope),
	}
	if claims.ExpiresAt != nil {
		info.Expiration = claims.ExpiresAt.Time
	}
	return info, nil
}

func buildServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "poenskelisten",
		Version: config.ConfigFile.PoenskelistenVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_wishlists",
		Description: "List the wishlists you can see: yours, ones you collaborate on and ones shared with you through your groups. Each has an id, name, description, date, owner and your role (owner, collaborator or group_member).",
	}, listWishlists)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_wishes",
		Description: "List the wishes on a wishlist you can see. Provide the wishlist_id (from list_wishlists). For group members, each wish says whether it is already claimed (and by whom, unless the list hides claimers); owners and collaborators never see claims.",
	}, listWishes)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_public_wishlist",
		Description: "Read a public wishlist from its public_hash (the UUID in a shared public link). Read-only.",
	}, getPublicWishlist)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_groups",
		Description: "List the groups you are a member of.",
	}, listGroups)

	registerWishlistWriteTools(server)
	registerWishWriteTools(server)
	registerClaimTools(server)
	registerGroupTools(server)

	return server
}

func listWishlists(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsRead)
	if err != nil {
		return nil, nil, err
	}

	wishlists, err := accessibleWishlists(userID)
	if err != nil {
		return nil, nil, errors.New("failed to load wishlists")
	}

	out := make([]wishlistOut, 0, len(wishlists))
	for _, rw := range wishlists {
		owner, err := database.GetUserInformation(rw.wishlist.OwnerID)
		if err != nil {
			// The owner is gone or disabled; the web app skips such lists too.
			continue
		}
		item := wishlistOut{
			ID:          rw.wishlist.ID.String(),
			Name:        rw.wishlist.Name,
			Description: rw.wishlist.Description,
			Owner:       displayName(owner.FirstName, owner.LastName),
			Role:        string(rw.role),
		}
		if rw.wishlist.Date != nil {
			item.Date = rw.wishlist.Date.Format("2006-01-02")
		}
		out = append(out, item)
	}
	return jsonResult(out), nil, nil
}

func listWishes(ctx context.Context, _ *mcp.CallToolRequest, args listWishesArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsRead)
	if err != nil {
		return nil, nil, err
	}

	wishlistID, err := uuid.Parse(strings.TrimSpace(args.WishlistID))
	if err != nil {
		return nil, nil, errors.New("invalid wishlist_id")
	}

	r, err := resolveRole(userID, wishlistID)
	if err != nil {
		return nil, nil, publicAccessError(err)
	}

	out, err := wishesOf(wishlistID, userID, r)
	if err != nil {
		return nil, nil, errors.New("failed to load wishes")
	}
	return jsonResult(out), nil, nil
}

func getPublicWishlist(ctx context.Context, _ *mcp.CallToolRequest, args publicWishlistArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsRead)
	if err != nil {
		return nil, nil, err
	}

	hash, err := uuid.Parse(strings.TrimSpace(args.PublicHash))
	if err != nil {
		return nil, nil, errors.New("invalid public_hash")
	}

	found, wishlist, err := database.GetPublicWishListByWishlistHash(hash)
	if err != nil {
		return nil, nil, errors.New("failed to load wishlist")
	}
	if !found {
		return nil, nil, errors.New("public wishlist not found")
	}

	owner, err := database.GetUserInformation(wishlist.OwnerID)
	if err != nil {
		return nil, nil, errors.New("public wishlist not found")
	}

	// The caller may well be the owner or a collaborator opening their own
	// public link: they must not see claims there either.
	r, err := resolveRole(userID, wishlist.ID)
	if err != nil && !errors.Is(err, errNoAccess) {
		return nil, nil, errors.New("failed to load wishlist")
	}

	wishes, err := wishesOf(wishlist.ID, userID, r)
	if err != nil {
		return nil, nil, errors.New("failed to load wishes")
	}

	out := publicWishlistOut{
		Name:        wishlist.Name,
		Description: wishlist.Description,
		Owner:       displayName(owner.FirstName, owner.LastName),
		Wishes:      wishes,
	}
	if wishlist.Date != nil {
		out.Date = wishlist.Date.Format("2006-01-02")
	}
	return jsonResult(out), nil, nil
}

// wishesOf loads a wishlist's wishes through the web app's own conversion, so
// claim visibility and hide_claimers anonymisation follow exactly the rules of
// controllers.ConvertWishToWishObject. The caller must already have been
// authorised on the wishlist; r is their role on it ("" for a stranger reading
// a public list).
func wishesOf(wishlistID, userID uuid.UUID, r role) ([]wishOut, error) {
	_, wishes, err := database.GetWishesFromWishlist(wishlistID)
	if err != nil {
		return nil, err
	}

	objects, err := controllers.ConvertWishesToWishObjects(wishes, &userID)
	if err != nil {
		return nil, err
	}

	out := make([]wishOut, 0, len(objects))
	for _, w := range objects {
		item := wishOut{ID: w.ID.String(), Name: w.Name, Note: w.Note, URL: w.URL, Price: w.Price}
		if claimsVisible(r, w.Owner.ID, userID) {
			claimed := len(w.WishClaim) > 0
			item.Claimed = &claimed
			if claimed {
				item.ClaimedBy = displayName(w.WishClaim[0].User.FirstName, w.WishClaim[0].User.LastName)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// publicAccessError keeps "no access" distinguishable from a failed lookup
// without leaking internals.
func publicAccessError(err error) error {
	if errors.Is(err, errNoAccess) {
		return errNoAccess
	}
	return errors.New("failed to verify access")
}

func displayName(first, last string) string {
	return strings.TrimSpace(first + " " + last)
}

func listGroups(ctx context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeGroupsRead)
	if err != nil {
		return nil, nil, err
	}

	groups, err := database.GetGroupsAUserIsAMemberOf(userID)
	if err != nil {
		return nil, nil, errors.New("failed to load groups")
	}

	out := make([]groupOut, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupOut{ID: g.ID.String(), Name: g.Name, Description: g.Description})
	}
	return jsonResult(out), nil, nil
}

// requireScope resolves the authenticated user and enforces that the token
// carries the given scope.
func requireScope(ctx context.Context, scope string) (uuid.UUID, error) {
	info := mcpauth.TokenInfoFromContext(ctx)
	if info == nil {
		return uuid.Nil, errors.New("not authenticated")
	}
	if !containsScope(info.Scopes, scope) {
		return uuid.Nil, fmt.Errorf("missing required scope: %s", scope)
	}
	userID, err := uuid.Parse(info.UserID)
	if err != nil {
		return uuid.Nil, errors.New("invalid token subject")
	}
	return userID, nil
}

func containsScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		b = []byte("[]")
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}
