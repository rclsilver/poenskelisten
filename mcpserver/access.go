package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// role is how a user relates to a wishlist. It decides both whether they can
// read it and whether they may see who claimed its wishes.
type role string

const (
	roleOwner        role = "owner"
	roleCollaborator role = "collaborator"
	roleGroupMember  role = "group_member"
)

// errNoAccess is deliberately the same for "doesn't exist" and "not yours" so
// a caller can't probe for wishlist IDs.
var errNoAccess = errors.New("wishlist not found or not accessible")

// resolveRole returns the caller's strongest role on a wishlist (owner, then
// collaborator, then group member), or errNoAccess. Any single place that
// reads a wishlist for the MCP must go through here.
func resolveRole(userID, wishlistID uuid.UUID) (role, error) {
	// Collaboration and group rows survive the deletion of a wishlist, so a
	// disabled one has to be ruled out before looking at them.
	if _, err := database.GetWishlist(wishlistID); errors.Is(err, database.ErrWishlistNotFound) {
		return "", errNoAccess
	} else if err != nil {
		return "", fmt.Errorf("failed to load wishlist: %w", err)
	}

	owns, err := database.VerifyUserOwnershipToWishlist(userID, wishlistID)
	if err != nil {
		return "", fmt.Errorf("failed to verify ownership: %w", err)
	}
	if owns {
		return roleOwner, nil
	}

	collaborates, err := database.VerifyWishlistCollaboratorToWishlist(wishlistID, userID)
	if err != nil {
		return "", fmt.Errorf("failed to verify collaboration: %w", err)
	}
	if collaborates {
		return roleCollaborator, nil
	}

	member, err := database.VerifyUserMembershipToGroupMembershipToWishlist(userID, wishlistID)
	if err != nil {
		return "", fmt.Errorf("failed to verify group membership: %w", err)
	}
	if member {
		return roleGroupMember, nil
	}

	return "", errNoAccess
}

// roledWishlist is a wishlist together with the caller's role on it.
type roledWishlist struct {
	wishlist models.Wishlist
	role     role
}

// accessibleWishlists lists every wishlist the user can read, one entry each,
// tagged with the strongest role. Mirrors controllers.GetAllWishlistObjects.
func accessibleWishlists(userID uuid.UUID) ([]roledWishlist, error) {
	owned, err := database.GetOwnedWishlists(userID)
	if err != nil {
		return nil, err
	}
	collaborated, err := database.GetWishlistsByUserIDThroughWishlistCollaborations(userID)
	if err != nil {
		return nil, err
	}
	shared, err := database.GetWishlistsByUserIDThroughWishlistMemberships(userID)
	if err != nil {
		return nil, err
	}

	seen := make(map[uuid.UUID]bool)
	out := make([]roledWishlist, 0, len(owned)+len(collaborated)+len(shared))
	add := func(wishlists []models.Wishlist, r role) {
		for _, w := range wishlists {
			if seen[w.ID] {
				continue
			}
			seen[w.ID] = true
			out = append(out, roledWishlist{wishlist: w, role: r})
		}
	}
	add(owned, roleOwner)
	add(collaborated, roleCollaborator)
	add(shared, roleGroupMember)
	return out, nil
}

// claimsVisible reports whether the caller may learn whether a wish is
// claimed. Owners and collaborators must never see it (it would spoil the
// surprise), nor the wish's own author; controllers.ConvertWishToWishObject
// enforces the same rule on the data, this keeps the output from implying
// "unclaimed" when the information was merely withheld.
func claimsVisible(r role, wishOwnerID, userID uuid.UUID) bool {
	return r != roleOwner && r != roleCollaborator && wishOwnerID != userID
}
