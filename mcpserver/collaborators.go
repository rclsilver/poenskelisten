package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxCollaboratorsPerWishlist bounds what an agent can add; the web has no cap.
const maxCollaboratorsPerWishlist = 50

type collaboratorArgs struct {
	WishlistID string `json:"wishlist_id" jsonschema:"the ID of a wishlist you own (from list_wishlists)"`
	UserID     string `json:"user_id" jsonschema:"the ID of the user (from list_group_members, or get_wishlist_sharing for current collaborators)"`
}

// errNoUser is the same for "no such user" and "not someone you share a
// group with", so user IDs can't be probed.
var errNoUser = errors.New("user not found or not reachable")

// reachableUser returns an enabled user who shares a live group with the
// caller, or errNoUser. The web app lets an owner pick any account (its
// user picker lists everyone); the MCP limits it to people the caller can
// already see through list_group_members, so an agent can't be used to reach
// strangers.
func reachableUser(userID, otherID uuid.UUID) (models.User, error) {
	other, err := database.GetUserInformation(otherID)
	if errors.Is(err, database.ErrUserNotFound) {
		return models.User{}, errNoUser
	} else if err != nil {
		return models.User{}, errors.New("failed to load the user")
	}

	groups, err := database.GetGroupsAUserIsAMemberOf(userID)
	if err != nil {
		return models.User{}, errors.New("failed to load groups")
	}
	for _, g := range groups {
		member, err := database.VerifyUserMembershipToGroup(otherID, g.ID)
		if err != nil {
			return models.User{}, errors.New("failed to verify group membership")
		} else if member {
			return other, nil
		}
	}
	return models.User{}, errNoUser
}

func registerCollaboratorTools(server *mcp.Server) {
	no, yes := false, true

	mcp.AddTool(server, &mcp.Tool{
		Name: "add_collaborator",
		Description: "Make someone a collaborator on a wishlist you own: they can then edit it and its wishes, and never see who claimed what. " +
			"They must be in a group with you (user_id from list_group_members). Returns the wishlist's sharing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, addWishlistCollaborator)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_collaborator",
		Description: "Remove a collaborator from a wishlist you own (user_id from get_wishlist_sharing); they lose the right to edit it. Returns the wishlist's sharing.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, removeWishlistCollaborator)
}

// addWishlistCollaborator follows controllers.APICollaborateWishlist (owner
// only, not the owner themself, no duplicates), plus the reachability rule
// of reachableUser.
func addWishlistCollaborator(ctx context.Context, _ *mcp.CallToolRequest, args collaboratorArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}
	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	otherID, err := parseID("user_id", args.UserID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, true); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}
	if otherID == userID {
		return nil, nil, userError("The wishlist owner can't be a collaborator.")
	}
	if _, err := reachableUser(userID, otherID); err != nil {
		return nil, nil, writeError(err, "failed to verify the user")
	}

	already, err := database.VerifyWishlistCollaboratorToWishlist(wishlistID, otherID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's collaborators")
	} else if already {
		return nil, nil, userError("That user is already a collaborator on this wishlist.")
	}
	collaborators, err := database.GetWishlistCollaboratorsFromWishlist(wishlistID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's collaborators")
	} else if len(collaborators) >= maxCollaboratorsPerWishlist {
		return nil, nil, userError("The wishlist has too many collaborators.")
	}

	collaborator := models.WishlistCollaborator{UserID: otherID, WishlistID: wishlistID}
	collaborator.ID = uuid.New()
	if err := database.CreateWishlistCollaboratorInDB(collaborator); err != nil {
		return nil, nil, errors.New("failed to add the collaborator")
	}
	return sharingResult(wishlistID, userID)
}

// removeWishlistCollaborator follows controllers.APIUnCollaborateWishlist:
// owner only. It doesn't need the collaborator to be reachable any more, so
// access can always be taken back.
func removeWishlistCollaborator(ctx context.Context, _ *mcp.CallToolRequest, args collaboratorArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}
	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	otherID, err := parseID("user_id", args.UserID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, true); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	notCollaborator := userError("That user is not a collaborator on this wishlist.")
	collaborates, err := database.VerifyWishlistCollaboratorToWishlist(wishlistID, otherID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's collaborators")
	} else if !collaborates {
		return nil, nil, notCollaborator
	}
	collaborator, err := database.GetWishlistCollaboratorByUserIDAndWishlistID(wishlistID, otherID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist's collaborators")
	} else if collaborator.ID == uuid.Nil {
		return nil, nil, notCollaborator
	}
	if err := database.DeleteWishlistCollaboratorByWishlistCollaboratorID(collaborator.ID); err != nil {
		return nil, nil, errors.New("failed to remove the collaborator")
	}
	return sharingResult(wishlistID, userID)
}
