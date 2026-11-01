package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const scopeWishesClaim = "mcp:wishes.claim"

type claimArgs struct {
	WishID string `json:"wish_id" jsonschema:"the ID of the wish (from list_wishes)"`
}

// claimOut deliberately says nothing about who holds the claim: the caller
// either just took it or just released it.
type claimOut struct {
	ID      string `json:"id"`
	Claimed bool   `json:"claimed"`
}

func registerClaimTools(server *mcp.Server) {
	no := false

	mcp.AddTool(server, &mcp.Tool{
		Name: "claim_wish",
		Description: "Claim (reserve) a wish on a claimable wishlist shared with you through a group, so nobody else buys it. " +
			"You cannot claim your own wishes, nor wishes on wishlists you own or collaborate on, and a wish can only have one claimer. " +
			"The wishlist's owner is not notified. Undo with unclaim_wish.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, claimWish)

	mcp.AddTool(server, &mcp.Tool{
		Name: "unclaim_wish",
		Description: "Release a claim you made on a wish, making it available again. " +
			"It only works on your own claim and fails (without changing anything) if you have none, so calling it twice reports an error the second time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, unclaimWish)
}

// claimableWish loads a live wish the caller may claim or unclaim on, with the
// web app's rules (RegisterWishClaim / RemoveWishClaim): the wishlist must be
// claimable and the caller a group member of it, neither its owner nor a
// collaborator, since a claim would spoil their surprise. Callers who can't
// see the wish get errNoWish, as for the other wish tools.
func claimableWish(userID, wishID uuid.UUID) (models.Wish, error) {
	wish, err := database.GetWishByWishID(wishID)
	if err != nil {
		return models.Wish{}, errors.New("failed to load wish")
	} else if wish == nil {
		return models.Wish{}, errNoWish
	}

	r, err := resolveRole(userID, wish.WishlistID)
	if errors.Is(err, errNoAccess) {
		return models.Wish{}, errNoWish
	} else if err != nil {
		return models.Wish{}, err
	}
	if r != roleGroupMember {
		return models.Wish{}, userError("You cannot claim wishes on a wishlist you own or collaborate on.")
	}

	wishlist, err := database.GetWishlist(wish.WishlistID)
	if err != nil {
		return models.Wish{}, errors.New("failed to load wishlist")
	}
	if wishlist.Claimable == nil || !*wishlist.Claimable {
		return models.Wish{}, userError("Wishes in the wishlist are not marked as claimable.")
	}
	return *wish, nil
}

func claimWish(ctx context.Context, _ *mcp.CallToolRequest, args claimArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishesClaim)
	if err != nil {
		return nil, nil, err
	}
	wishID, err := parseID("wish_id", args.WishID)
	if err != nil {
		return nil, nil, err
	}
	wish, err := claimableWish(userID, wishID)
	if err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	if wish.OwnerID == userID {
		return nil, nil, userError("You cannot claim your own wish.")
	}
	claimed, err := database.VerifyWishIsClaimed(wishID)
	if err != nil {
		return nil, nil, errors.New("failed to verify claim status")
	} else if claimed {
		return nil, nil, userError("Wish is already claimed.")
	}

	claim := models.WishClaim{UserID: userID, WishID: wishID}
	claim.ID = uuid.New()
	if _, err := database.CreateWishClaimInDB(claim); err != nil {
		return nil, nil, errors.New("failed to claim the wish")
	}
	return jsonResult(claimOut{ID: wishID.String(), Claimed: true}), nil, nil
}

func unclaimWish(ctx context.Context, _ *mcp.CallToolRequest, args claimArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishesClaim)
	if err != nil {
		return nil, nil, err
	}
	wishID, err := parseID("wish_id", args.WishID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := claimableWish(userID, wishID); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	mine, err := database.VerifyUserOwnershipToWishClaimByWish(userID, wishID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the claim")
	} else if !mine {
		return nil, nil, userError("You cannot unclaim a wish you haven't claimed.")
	}
	if err := database.DeleteWishClaimByUserAndWish(wishID, userID); err != nil {
		return nil, nil, errors.New("failed to release the claim")
	}
	return jsonResult(claimOut{ID: wishID.String(), Claimed: false}), nil, nil
}
