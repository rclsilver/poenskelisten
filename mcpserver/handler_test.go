package mcpserver

import (
	pauth "aunefyren/poenskelisten/auth"
	"aunefyren/poenskelisten/config"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/models"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

func init() {
	gin.SetMode(gin.TestMode)
	if logger.Log == nil {
		logger.Log = logrus.New()
	}
}

// setupMCPDB points database.Instance at a fresh in-memory SQLite DB with the
// tables the mcpserver tools touch.
func setupMCPDB(t *testing.T) {
	t.Helper()

	dbSQL, err := sql.Open("sqlite", "file:"+uuid.NewString()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { dbSQL.Close() })
	dbSQL.SetMaxOpenConns(1)

	instance, err := gorm.Open(sqlite.Dialector{Conn: dbSQL}, &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open gorm: %v", err)
	}
	if err := instance.AutoMigrate(
		&models.User{}, &models.Wishlist{}, &models.Wish{},
		&models.Group{}, &models.GroupMembership{},
		&models.WishlistMembership{}, &models.WishlistCollaborator{}, &models.WishClaim{}, &models.WishCategory{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	database.Instance = instance
	if err := database.EnsureOneClaimPerWish(); err != nil {
		t.Fatalf("failed to add the claim index: %v", err)
	}
}

// bearerRoundTripper injects a fixed Authorization header into every request,
// standing in for a real OAuth client since StreamableClientTransport has no
// simpler way to attach a bearer token.
type bearerRoundTripper struct {
	token string
}

func (rt bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return http.DefaultTransport.RoundTrip(req)
}

// startMCPTestServer wires Handler() into a real gin router served over HTTP,
// enables MCP, and returns the server URL plus a connected+authenticated
// client session ready to call tools.
func startMCPTestServer(t *testing.T, userID uuid.UUID, scope string) *sdkmcp.ClientSession {
	t.Helper()
	setupMCPConfig(t)
	config.ConfigFile.MCPEnabled = true

	router := gin.New()
	router.Any("/mcp", Handler())
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	token, err := pauth.GenerateOAuthAccessToken(userID, "mcp-client", config.MCPResource(), scope, false, true)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	httpClient := &http.Client{Transport: bearerRoundTripper{token: token}}
	transport := &sdkmcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", HTTPClient: httpClient}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)

	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session
}

func TestHandlerDisabledReturns404(t *testing.T) {
	setupMCPConfig(t)
	config.ConfigFile.MCPEnabled = false

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest("POST", "/mcp", nil)
	Handler()(ctx)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when MCP is disabled", w.Code)
	}
}

func TestHandlerRejectsMissingBearerToken(t *testing.T) {
	setupMCPConfig(t)
	config.ConfigFile.MCPEnabled = true

	router := gin.New()
	router.Any("/mcp", Handler())
	httpServer := httptest.NewServer(router)
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/mcp", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a bearer token", resp.StatusCode)
	}
}

func TestListWishlistsTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	now := time.Now()
	wishlist := models.Wishlist{Name: "Birthday", Enabled: true, OwnerID: owner.ID, Date: &now}
	wishlist.ID = uuid.New()
	if r := database.Instance.Create(&wishlist); r.Error != nil {
		t.Fatalf("failed to create wishlist: %v", r.Error)
	}

	session := startMCPTestServer(t, owner.ID, "mcp:wishlists.read")

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_wishlists"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call reported an error: %+v", result.Content)
	}

	text := firstText(t, result)
	var got []wishlistOut
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("failed to parse tool result: %v (raw: %s)", err, text)
	}
	if len(got) != 1 || got[0].Name != "Birthday" {
		t.Errorf("got %+v, want exactly the one 'Birthday' wishlist", got)
	}
}

func TestListWishlistsToolMissingScope(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")

	session := startMCPTestServer(t, owner.ID, "mcp:groups.read") // wrong scope

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_wishlists"})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	if !result.IsError {
		t.Error("expected an error result when the token lacks mcp:wishlists.read")
	}
}

func TestListWishesTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	now := time.Now()
	wishlist := models.Wishlist{Name: "Birthday", Enabled: true, OwnerID: owner.ID, Date: &now}
	wishlist.ID = uuid.New()
	if r := database.Instance.Create(&wishlist); r.Error != nil {
		t.Fatalf("failed to create wishlist: %v", r.Error)
	}
	wish := models.Wish{Name: "Headphones", Enabled: true, OwnerID: owner.ID, WishlistID: wishlist.ID}
	wish.ID = uuid.New()
	if r := database.Instance.Create(&wish); r.Error != nil {
		t.Fatalf("failed to create wish: %v", r.Error)
	}

	session := startMCPTestServer(t, owner.ID, "mcp:wishlists.read")

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "list_wishes",
		Arguments: map[string]any{"wishlist_id": wishlist.ID.String()},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call reported an error: %+v", result.Content)
	}

	text := firstText(t, result)
	var got []wishOut
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("failed to parse tool result: %v (raw: %s)", err, text)
	}
	if len(got) != 1 || got[0].Name != "Headphones" {
		t.Errorf("got %+v, want exactly the one 'Headphones' wish", got)
	}
}

func TestListWishesToolInvalidWishlistID(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")

	session := startMCPTestServer(t, owner.ID, "mcp:wishlists.read")

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "list_wishes",
		Arguments: map[string]any{"wishlist_id": "not-a-uuid"},
	})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	if !result.IsError {
		t.Error("expected an error result for an invalid wishlist_id")
	}
}

func TestListWishesToolNotOwner(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	outsider := createMCPTestUser(t, "Bob")
	now := time.Now()
	wishlist := models.Wishlist{Name: "Birthday", Enabled: true, OwnerID: owner.ID, Date: &now}
	wishlist.ID = uuid.New()
	if r := database.Instance.Create(&wishlist); r.Error != nil {
		t.Fatalf("failed to create wishlist: %v", r.Error)
	}

	session := startMCPTestServer(t, outsider.ID, "mcp:wishlists.read")

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "list_wishes",
		Arguments: map[string]any{"wishlist_id": wishlist.ID.String()},
	})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	if !result.IsError {
		t.Error("expected an error result when the caller doesn't own the wishlist")
	}
}

func TestListGroupsTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	group := models.Group{Name: "Family", Enabled: true, OwnerID: owner.ID}
	group.ID = uuid.New()
	if r := database.Instance.Create(&group); r.Error != nil {
		t.Fatalf("failed to create group: %v", r.Error)
	}
	membership := models.GroupMembership{GroupID: group.ID, MemberID: owner.ID, Enabled: true}
	membership.ID = uuid.New()
	if r := database.Instance.Create(&membership); r.Error != nil {
		t.Fatalf("failed to create membership: %v", r.Error)
	}

	session := startMCPTestServer(t, owner.ID, "mcp:groups.read")

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_groups"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call reported an error: %+v", result.Content)
	}

	text := firstText(t, result)
	var got []groupOut
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("failed to parse tool result: %v (raw: %s)", err, text)
	}
	if len(got) != 1 || got[0].Name != "Family" {
		t.Errorf("got %+v, want exactly the one 'Family' group", got)
	}
}

func firstText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("first content block is %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

func boolPtr(b bool) *bool { return &b }

// createMCPTestUser inserts an enabled user with a unique e-mail (required,
// NOT NULL in the schema).
func createMCPTestUser(t *testing.T, firstName string) models.User {
	t.Helper()
	email := uuid.NewString() + "@example.com"
	user := models.User{FirstName: firstName, Email: &email, Enabled: boolPtr(true)}
	user.ID = uuid.New()
	if r := database.Instance.Create(&user); r.Error != nil {
		t.Fatalf("failed to create user: %v", r.Error)
	}
	return user
}

// The SDK runs each request on a goroutine of its own, out of reach of gin's
// recovery: a tool that panics must answer with an error and leave the
// server, and the app, running.
func TestPanickingToolLeavesTheServerRunning(t *testing.T) {
	ctx := context.Background()
	server := buildServer()
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "panics"}, func(context.Context, *sdkmcp.CallToolRequest, noArgs) (*sdkmcp.CallToolResult, any, error) {
		panic("boom")
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	if _, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "panics"}); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Errorf("panicking tool error = %v, want an internal error", err)
	}
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Errorf("the server stopped answering after the panic: %v", err)
	}
}

func TestBuildServerRegistersTools(t *testing.T) {
	server := buildServer()
	if server == nil {
		t.Fatal("buildServer returned nil")
	}
}

func TestJSONResult(t *testing.T) {
	result := jsonResult([]wishlistOut{{ID: "1", Name: "Test"}})
	text := firstText(t, result)
	var got []wishlistOut
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("failed to parse jsonResult output: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Test" {
		t.Errorf("got %+v, want [{ID:1 Name:Test}]", got)
	}
}

func TestJSONResultUnmarshalableFallsBackToEmptyArray(t *testing.T) {
	// A Go channel cannot be marshaled to JSON; jsonResult should fall back to
	// "[]" rather than panicking or propagating the error.
	result := jsonResult(make(chan int))
	if firstText(t, result) != "[]" {
		t.Errorf("text = %q, want []", firstText(t, result))
	}
}

// callTool runs a tool and decodes its JSON text result into dst, failing the
// test on a transport error or a tool error.
func callTool(t *testing.T, userID uuid.UUID, name string, args map[string]any, dst any) {
	t.Helper()
	session := startMCPTestServer(t, userID, "mcp:wishlists.read")
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool call reported an error: %+v", result.Content)
	}
	if err := json.Unmarshal([]byte(firstText(t, result)), dst); err != nil {
		t.Fatalf("failed to parse tool result: %v", err)
	}
}

// callToolExpectError runs a tool and fails unless it reports an error; the
// error text is returned.
func callToolExpectError(t *testing.T, userID uuid.UUID, name string, args map[string]any) string {
	t.Helper()
	session := startMCPTestServer(t, userID, "mcp:wishlists.read")
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected an error result, got %s", firstText(t, result))
	}
	return firstText(t, result)
}

func TestListWishlistsToolIncludesSharedAndCollaborated(t *testing.T) {
	setupMCPDB(t)
	me := createMCPTestUser(t, "Ada")
	friend := createMCPTestUser(t, "Bob")
	stranger := createMCPTestUser(t, "Dan")
	createMCPWishlist(t, me, "Mine", false)
	shared := createMCPWishlist(t, friend, "Shared", false)
	shareMCPWishlist(t, friend, shared, me)
	collab := createMCPWishlist(t, friend, "Collab", false)
	createMCPCollaborator(t, me, collab)
	createMCPWishlist(t, stranger, "Secret", false)

	var got []wishlistOut
	callTool(t, me.ID, "list_wishlists", nil, &got)

	roles := map[string]wishlistOut{}
	for _, w := range got {
		roles[w.Name] = w
	}
	if len(got) != 3 {
		t.Fatalf("got %+v, want exactly Mine, Shared, Collab", got)
	}
	if roles["Mine"].Role != "owner" || roles["Shared"].Role != "group_member" || roles["Collab"].Role != "collaborator" {
		t.Errorf("unexpected roles: %+v", roles)
	}
	if roles["Shared"].Owner != "Bob" || roles["Mine"].Owner != "Ada" {
		t.Errorf("unexpected owners: %+v", roles)
	}
}

func TestListWishlistsToolStrangerSeesNothing(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	stranger := createMCPTestUser(t, "Dan")
	createMCPWishlist(t, owner, "Mine", false)

	var got []wishlistOut
	callTool(t, stranger.ID, "list_wishlists", nil, &got)
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestListWishesToolPerRole(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	member := createMCPTestUser(t, "Cleo")
	stranger := createMCPTestUser(t, "Dan")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	createMCPWish(t, owner, wishlist, "Headphones")
	createMCPCollaborator(t, collab, wishlist)
	shareMCPWishlist(t, owner, wishlist, member)
	args := map[string]any{"wishlist_id": wishlist.ID.String()}

	for name, user := range map[string]models.User{"owner": owner, "collaborator": collab, "group member": member} {
		var got []wishOut
		callTool(t, user.ID, "list_wishes", args, &got)
		if len(got) != 1 || got[0].Name != "Headphones" {
			t.Errorf("%s: got %+v, want the one wish", name, got)
		}
	}

	callToolExpectError(t, stranger.ID, "list_wishes", args)
}

func TestListWishesToolClaims(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	member := createMCPTestUser(t, "Cleo")
	claimer := createMCPTestUser(t, "Eve")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	wish := createMCPWish(t, owner, wishlist, "Headphones")
	createMCPCollaborator(t, collab, wishlist)
	shareMCPWishlist(t, owner, wishlist, member, claimer)
	createMCPClaim(t, claimer, wish)
	args := map[string]any{"wishlist_id": wishlist.ID.String()}

	// The surprise must survive: no claim information for owner or collaborator.
	for name, user := range map[string]models.User{"owner": owner, "collaborator": collab} {
		var got []wishOut
		callTool(t, user.ID, "list_wishes", args, &got)
		if len(got) != 1 || got[0].Claimed != nil || got[0].ClaimedBy != "" {
			t.Errorf("%s: claim leaked: %+v", name, got)
		}
	}

	var got []wishOut
	callTool(t, member.ID, "list_wishes", args, &got)
	if len(got) != 1 || got[0].Claimed == nil || !*got[0].Claimed || got[0].ClaimedBy != "Eve" {
		t.Errorf("group member: got %+v, want claimed by Eve", got)
	}
}

func TestListWishesToolHideClaimers(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	member := createMCPTestUser(t, "Cleo")
	claimer := createMCPTestUser(t, "Eve")
	wishlist := createMCPWishlist(t, owner, "Birthday", true)
	wish := createMCPWish(t, owner, wishlist, "Headphones")
	shareMCPWishlist(t, owner, wishlist, member, claimer)
	createMCPClaim(t, claimer, wish)

	var got []wishOut
	callTool(t, member.ID, "list_wishes", map[string]any{"wishlist_id": wishlist.ID.String()}, &got)
	if len(got) != 1 || got[0].Claimed == nil || !*got[0].Claimed {
		t.Fatalf("got %+v, want a claimed wish", got)
	}
	if got[0].ClaimedBy != "Hidden User" {
		t.Errorf("claimed_by = %q, want the anonymised \"Hidden User\"", got[0].ClaimedBy)
	}
}

func TestGetPublicWishlistTool(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	viewer := createMCPTestUser(t, "Dan")
	claimer := createMCPTestUser(t, "Eve")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	hash := uuid.New()
	if r := database.Instance.Model(&wishlist).Updates(map[string]any{"public": true, "public_hash": hash}); r.Error != nil {
		t.Fatalf("failed to publish wishlist: %v", r.Error)
	}
	wish := createMCPWish(t, owner, wishlist, "Headphones")
	createMCPClaim(t, claimer, wish)
	member := createMCPTestUser(t, "Fay")
	shareMCPWishlist(t, owner, wishlist, member, claimer)
	args := map[string]any{"public_hash": hash.String()}

	// A claim left from before the list was made public is never shown
	// through the link, whoever follows it.
	for _, reader := range []models.User{viewer, member, claimer, owner} {
		var got publicWishlistOut
		callTool(t, reader.ID, "get_public_wishlist", args, &got)
		if got.Name != "Birthday" || got.Owner != "Ada" || len(got.Wishes) != 1 {
			t.Fatalf("%s: got %+v, want the Birthday list with one wish", reader.FirstName, got)
		}
		if got.Wishes[0].Claimed != nil || got.Wishes[0].ClaimedBy != "" {
			t.Errorf("%s: claim leaked: %+v", reader.FirstName, got.Wishes[0])
		}
	}
}

func TestGetPublicWishlistToolInvalidAndUnknownHash(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	private := createMCPWishlist(t, owner, "Private", false) // not public
	privateHash := uuid.New()
	if r := database.Instance.Model(&private).Update("public_hash", privateHash); r.Error != nil {
		t.Fatalf("failed to set hash: %v", r.Error)
	}

	callToolExpectError(t, owner.ID, "get_public_wishlist", map[string]any{"public_hash": "nope"})
	callToolExpectError(t, owner.ID, "get_public_wishlist", map[string]any{"public_hash": uuid.NewString()})
	callToolExpectError(t, owner.ID, "get_public_wishlist", map[string]any{"public_hash": privateHash.String()})
}
