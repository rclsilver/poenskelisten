package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type wishlistSharingArgs struct {
	WishlistID string `json:"wishlist_id" jsonschema:"the ID of a wishlist you own (from list_wishlists)"`
}

type wishlistGroupArgs struct {
	WishlistID string `json:"wishlist_id" jsonschema:"the ID of a wishlist you own (from list_wishlists)"`
	GroupID    string `json:"group_id" jsonschema:"the ID of the group (from list_groups or get_wishlist_sharing)"`
}

// sharedGroupOut is a group a wishlist is shared with. Member is false when
// the owner is no longer in it (leaving a group keeps the share on the web
// too); the name is then withheld, as it is outside the owner's circle.
type sharedGroupOut struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Member bool   `json:"member"`
}

// sharingOut is who a wishlist is shared with. Only its owner gets it.
type sharingOut struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Public        bool             `json:"public"`
	PublicHash    string           `json:"public_hash,omitempty"`
	Groups        []sharedGroupOut `json:"groups"`
	Collaborators []userOut        `json:"collaborators"`
}

func sharingOf(wishlist models.Wishlist, userID uuid.UUID) (sharingOut, error) {
	out := sharingOut{
		ID:            wishlist.ID.String(),
		Name:          wishlist.Name,
		Public:        wishlist.Public != nil && *wishlist.Public,
		Groups:        []sharedGroupOut{},
		Collaborators: []userOut{},
	}
	if out.Public {
		out.PublicHash = wishlist.PublicHash.String()
	}

	shared, err := database.GetGroupsFromWishlist(wishlist.ID)
	if err != nil {
		return sharingOut{}, err
	}
	mine, err := database.GetGroupsAUserIsAMemberOf(userID)
	if err != nil {
		return sharingOut{}, err
	}
	member := make(map[uuid.UUID]bool, len(mine))
	for _, g := range mine {
		member[g.ID] = true
	}
	for _, g := range shared {
		item := sharedGroupOut{ID: g.ID.String(), Member: member[g.ID]}
		if item.Member {
			item.Name = g.Name
		}
		out.Groups = append(out.Groups, item)
	}

	collaborators, err := database.GetWishlistCollaboratorsFromWishlist(wishlist.ID)
	if err != nil {
		return sharingOut{}, err
	}
	for _, c := range collaborators {
		user, err := database.GetUserInformation(c.UserID)
		if errors.Is(err, database.ErrUserNotFound) {
			continue
		} else if err != nil {
			return sharingOut{}, err
		}
		out.Collaborators = append(out.Collaborators, userOut{ID: user.ID.String(), Name: displayName(user.FirstName, user.LastName)})
	}
	return out, nil
}

// sharingResult reloads the wishlist so the result reflects what was stored.
func sharingResult(wishlistID, userID uuid.UUID) (*mcp.CallToolResult, any, error) {
	wishlist, err := database.GetWishlist(wishlistID)
	if err != nil {
		return nil, nil, errors.New("failed to load the wishlist")
	}
	out, err := sharingOf(wishlist, userID)
	if err != nil {
		return nil, nil, errors.New("failed to load the wishlist's sharing")
	}
	return jsonResult(out), nil, nil
}

func registerSharingTools(server *mcp.Server) {
	no, yes := false, true

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_wishlist_sharing",
		Description: "Show who a wishlist you own is shared with: its groups, its collaborators and, when it is public, its public_hash. " +
			"Only the owner can see this. A group marked member=false is one you are not in; detach it to stop it seeing the wishlist. " +
			"The public link is managed with update_wishlist: setting public to false disables it, and setting it back to true issues a new link.",
	}, getWishlistSharing)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "attach_wishlist_to_group",
		Description: "Share a wishlist you own with a group you belong to, so its members can see it (and claim its wishes if it is claimable). Returns the wishlist's sharing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, attachWishlistToGroup)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "detach_wishlist_from_group",
		Description: "Stop sharing a wishlist you own with a group; its members lose access unless they reach the wishlist another way. Claims already made are kept. Returns the wishlist's sharing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, detachWishlistFromGroup)
}

func getWishlistSharing(ctx context.Context, _ *mcp.CallToolRequest, args wishlistSharingArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsRead)
	if err != nil {
		return nil, nil, err
	}
	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, true); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}
	return sharingResult(wishlistID, userID)
}

// attachWishlistToGroup follows controllers.APIAddWishlistsToGroup and
// JoinWishlist: only the owner may share a wishlist, and only with a group
// they belong to.
func attachWishlistToGroup(ctx context.Context, _ *mcp.CallToolRequest, args wishlistGroupArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}
	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	groupID, err := parseID("group_id", args.GroupID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, true); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}
	if _, err := memberGroup(userID, groupID); err != nil {
		return nil, nil, err
	}

	shared, err := database.VerifyGroupMembershipToWishlist(wishlistID, groupID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's groups")
	} else if shared {
		return nil, nil, userError("The wishlist is already shared with this group.")
	}
	groups, err := database.GetGroupsFromWishlist(wishlistID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's groups")
	} else if len(groups) >= maxGroupsPerWishlist {
		return nil, nil, userError("The wishlist is shared with too many groups.")
	}

	membership := models.WishlistMembership{GroupID: groupID, WishlistID: wishlistID}
	membership.ID = uuid.New()
	if _, err := database.CreateWishlistMembershipInDB(membership); err != nil {
		return nil, nil, errors.New("failed to share the wishlist with the group")
	}
	return sharingResult(wishlistID, userID)
}

// detachWishlistFromGroup follows controllers.RemoveFromWishlist: owner only.
// Unlike attaching, the owner needn't be in the group: withdrawing a share
// must stay possible after leaving it.
func detachWishlistFromGroup(ctx context.Context, _ *mcp.CallToolRequest, args wishlistGroupArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}
	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	groupID, err := parseID("group_id", args.GroupID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, true); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	found, membership, err := database.GetMembershipIDForGroupToWishlist(wishlistID, groupID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's groups")
	} else if !found || membership.ID == uuid.Nil {
		return nil, nil, userError("The wishlist is not shared with this group.")
	}
	if err := database.DeleteWishlistMembership(membership.ID); err != nil {
		return nil, nil, errors.New("failed to stop sharing the wishlist with the group")
	}
	return sharingResult(wishlistID, userID)
}
