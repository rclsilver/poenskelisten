package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const groupScopes = "mcp:groups.read"

func groupMembers(t *testing.T, userID uuid.UUID, groupID string) (*sdkmcp.CallToolResult, []userOut) {
	t.Helper()
	session := startMCPTestServer(t, userID, groupScopes)
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "list_group_members", Arguments: map[string]any{"group_id": groupID},
	})
	if err != nil {
		t.Fatalf("CallTool transport-level error: %v", err)
	}
	var out []userOut
	if !result.IsError {
		if err := json.Unmarshal([]byte(firstText(t, result)), &out); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}
	}
	return result, out
}

func TestListGroupMembersTool(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	bob := createMCPTestUser(t, "Bob")
	gone := createMCPTestUser(t, "Gone")
	group := shareWithGroup(t, uuid.Nil, ada.ID, bob.ID, gone.ID)
	database.Instance.Model(&models.User{}).Where("id = ?", gone.ID).Update("enabled", false)

	result, out := groupMembers(t, bob.ID, group.ID.String())
	if result.IsError {
		t.Fatalf("member was refused: %s", firstText(t, result))
	}
	if len(out) != 2 || out[0].ID != ada.ID.String() || out[0].Name != "Ada" || out[1].ID != bob.ID.String() {
		t.Fatalf("members = %+v, want Ada and Bob only (disabled account left out)", out)
	}
	if text := firstText(t, result); strings.Contains(text, "@") || strings.Contains(text, "email") {
		t.Errorf("result leaks an e-mail: %s", text)
	}
}

func TestListGroupMembersToolAccess(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	stranger := createMCPTestUser(t, "Eve")
	group := shareWithGroup(t, uuid.Nil, ada.ID)
	deleted := shareWithGroup(t, uuid.Nil, ada.ID)
	database.Instance.Model(&models.Group{}).Where("id = ?", deleted.ID).Update("enabled", false)

	cases := []struct {
		name    string
		userID  uuid.UUID
		groupID string
		want    string
	}{
		{"stranger", stranger.ID, group.ID.String(), errNoGroup.Error()},
		{"unknown group", ada.ID, uuid.NewString(), errNoGroup.Error()},
		{"deleted group", ada.ID, deleted.ID.String(), errNoGroup.Error()},
		{"malformed id", ada.ID, "nope", "invalid group_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, _ := groupMembers(t, c.userID, c.groupID)
			if !result.IsError || firstText(t, result) != c.want {
				t.Errorf("got %q (error=%v), want %q", firstText(t, result), result.IsError, c.want)
			}
		})
	}
}

func TestListGroupMembersNeedsScope(t *testing.T) {
	setupMCPDB(t)
	ada := createMCPTestUser(t, "Ada")
	group := shareWithGroup(t, uuid.Nil, ada.ID)

	result := callWriteTool(t, ada.ID, "mcp:wishlists.read", "list_group_members", map[string]any{"group_id": group.ID.String()})
	if !result.IsError {
		t.Errorf("list_group_members succeeded without mcp:groups.read")
	}
}
