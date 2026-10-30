package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type groupMembersArgs struct {
	GroupID string `json:"group_id" jsonschema:"the ID of a group you belong to (from list_groups)"`
}

// userOut names another user: the ID the other tools take, and a display
// name. Nothing else from their account (e-mail above all) ever leaves.
type userOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// errNoGroup is the same for "doesn't exist" and "you're not in it", so the
// group tools can't be used to probe for group IDs.
var errNoGroup = errors.New("group not found or not accessible")

// memberGroup returns a live group the user belongs to, or errNoGroup.
// database.VerifyUserMembershipToGroup alone would also accept a deleted
// group, whose memberships are left enabled.
func memberGroup(userID, groupID uuid.UUID) (models.Group, error) {
	groups, err := database.GetGroupsAUserIsAMemberOf(userID)
	if err != nil {
		return models.Group{}, errors.New("failed to load groups")
	}
	for _, g := range groups {
		if g.ID == groupID {
			return g, nil
		}
	}
	return models.Group{}, errNoGroup
}

func registerGroupTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_group_members",
		Description: "List the members of a group you belong to (group_id from list_groups), each with an id and a name (the id is what add_collaborator takes). Groups you are not in are reported as not found.",
	}, listGroupMembers)
}

// listGroupMembers follows controllers.GetGroupMembers: only members may list
// a group, and disabled accounts are left out.
func listGroupMembers(ctx context.Context, _ *mcp.CallToolRequest, args groupMembersArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeGroupsRead)
	if err != nil {
		return nil, nil, err
	}
	groupID, err := parseID("group_id", args.GroupID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := memberGroup(userID, groupID); err != nil {
		return nil, nil, err
	}

	memberships, err := database.GetGroupMembershipsFromGroup(groupID)
	if err != nil {
		return nil, nil, errors.New("failed to load group members")
	}
	out := make([]userOut, 0, len(memberships))
	for _, m := range memberships {
		member, err := database.GetUserInformation(m.MemberID)
		if errors.Is(err, database.ErrUserNotFound) {
			continue
		} else if err != nil {
			return nil, nil, errors.New("failed to load group members")
		}
		out = append(out, userOut{ID: member.ID.String(), Name: displayName(member.FirstName, member.LastName)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return jsonResult(out), nil, nil
}
