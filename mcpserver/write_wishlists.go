package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type createWishlistArgs struct {
	Name         string   `json:"name" jsonschema:"the wishlist name, 5 to 200 characters, unique among your wishlists; must not contain < > \" or backticks"`
	Description  string   `json:"description,omitempty" jsonschema:"optional description"`
	Date         string   `json:"date,omitempty" jsonschema:"optional expiration date formatted YYYY-MM-DD, must be in the future; without it the wishlist does not expire"`
	GroupIDs     []string `json:"group_ids,omitempty" jsonschema:"optional IDs of groups (from list_groups) you belong to, to share the wishlist with"`
	Claimable    *bool    `json:"claimable,omitempty" jsonschema:"whether group members can claim wishes (default true, or false when public); cannot be combined with public"`
	HideClaimers *bool    `json:"hide_claimers,omitempty" jsonschema:"hide who claimed a wish from group members (default false)"`
	Public       *bool    `json:"public,omitempty" jsonschema:"make the wishlist readable by anyone with its public link, accounts or not (default false); cannot be combined with claimable"`
}

type updateWishlistArgs struct {
	WishlistID   string  `json:"wishlist_id" jsonschema:"the ID of the wishlist to edit (from list_wishlists)"`
	Name         *string `json:"name,omitempty" jsonschema:"new name, same rules as when creating"`
	Description  *string `json:"description,omitempty" jsonschema:"new description; an empty string clears it"`
	Date         *string `json:"date,omitempty" jsonschema:"new expiration date formatted YYYY-MM-DD; also turns expiration on"`
	Expires      *bool   `json:"expires,omitempty" jsonschema:"set false to make the wishlist stop expiring"`
	Claimable    *bool   `json:"claimable,omitempty" jsonschema:"whether group members can claim wishes; cannot be combined with public"`
	HideClaimers *bool   `json:"hide_claimers,omitempty" jsonschema:"hide who claimed a wish from group members"`
	Public       *bool   `json:"public,omitempty" jsonschema:"make the wishlist readable by anyone with its public link; turning it back on after making it private issues a new link"`
}

type deleteWishlistArgs struct {
	WishlistID string `json:"wishlist_id" jsonschema:"the ID of the wishlist to delete (from list_wishlists)"`
}

// wishlistWriteOut is what the write tools return about a wishlist. Only
// owners and collaborators reach it, so the sharing settings and the public
// link are theirs to see.
type wishlistWriteOut struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Date         string `json:"date,omitempty"`
	Role         string `json:"role"`
	Claimable    bool   `json:"claimable"`
	HideClaimers bool   `json:"hide_claimers"`
	Public       bool   `json:"public"`
	PublicHash   string `json:"public_hash,omitempty"`
}

type deletedOut struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func newWishlistWriteOut(w models.Wishlist, r role) wishlistWriteOut {
	out := wishlistWriteOut{
		ID:           w.ID.String(),
		Name:         w.Name,
		Description:  w.Description,
		Role:         string(r),
		Claimable:    w.Claimable != nil && *w.Claimable,
		HideClaimers: w.HideClaimers != nil && *w.HideClaimers,
		Public:       w.Public != nil && *w.Public,
	}
	if w.Expires != nil && *w.Expires && w.Date != nil {
		out.Date = w.Date.Format("2006-01-02")
	}
	if out.Public {
		out.PublicHash = w.PublicHash.String()
	}
	return out
}

func valueOr(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}

func registerWishlistWriteTools(server *mcp.Server) {
	no, yes := false, true

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_wishlist",
		Description: "Create a wishlist owned by you, optionally shared with groups you belong to. Names must be unique among your wishlists. Returns the new wishlist.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, createWishlist)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_wishlist",
		Description: "Edit a wishlist you own or collaborate on. Only the fields you provide change. Returns the updated wishlist.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, updateWishlist)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_wishlist",
		Description: "Delete a wishlist you own. It disappears for everyone (owner, collaborators, groups and its public link) but is only disabled in the database, not erased. Only the owner can do this.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, deleteWishlist)
}

func createWishlist(ctx context.Context, _ *mcp.CallToolRequest, args createWishlistArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}

	name := strings.TrimSpace(args.Name)
	description := strings.TrimSpace(args.Description)

	groupIDs, err := parseGroupIDs(args.GroupIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, groupID := range groupIDs {
		member, err := database.VerifyUserMembershipToGroup(userID, groupID)
		if err != nil {
			return nil, nil, errors.New("failed to verify membership to group")
		} else if !member {
			return nil, nil, userError("You are not a member of one/more groups.")
		}
	}

	if err := validateName("wishlist", name); err != nil {
		return nil, nil, writeError(err, "failed to validate the name")
	}
	if err := validateText("description", description, maxDescriptionLength); err != nil {
		return nil, nil, writeError(err, "failed to validate the description")
	}

	now := time.Now()
	wishlist := models.Wishlist{
		Name:         name,
		Description:  description,
		OwnerID:      userID,
		Date:         &now,
		Expires:      ptrTo(false),
		Claimable:    ptrTo(valueOr(args.Claimable, !valueOr(args.Public, false))),
		HideClaimers: ptrTo(valueOr(args.HideClaimers, false)),
		Public:       ptrTo(valueOr(args.Public, false)),
		PublicHash:   uuid.New(),
		Enabled:      true,
	}
	wishlist.ID = uuid.New()

	if strings.TrimSpace(args.Date) != "" {
		date, err := parseDate(args.Date)
		if err != nil {
			return nil, nil, err
		}
		if now.After(date) {
			return nil, nil, userError("The date of the wishlist must be in the future.")
		}
		wishlist.Date = &date
		wishlist.Expires = ptrTo(true)
	}

	if *wishlist.Public && *wishlist.Claimable {
		return nil, nil, userError("A wishlist cannot have claimable wishes and be public to users without accounts.")
	}

	unique, err := database.VerifyUniqueWishlistNameForUser(name, userID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wishlist name")
	} else if !unique {
		return nil, nil, userError("There is already a wishlist with that name on your profile.")
	}

	created, err := database.CreateWishlistInDB(wishlist)
	if err != nil {
		return nil, nil, errors.New("failed to create the wishlist")
	}

	for _, groupID := range groupIDs {
		membership := models.WishlistMembership{GroupID: groupID, WishlistID: created.ID}
		membership.ID = uuid.New()
		if _, err := database.CreateWishlistMembershipInDB(membership); err != nil {
			return nil, nil, errors.New("the wishlist was created but sharing it with a group failed")
		}
	}

	return jsonResult(newWishlistWriteOut(created, roleOwner)), nil, nil
}

// parseGroupIDs parses and de-duplicates group IDs so one group can't be
// attached twice.
func parseGroupIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) > maxGroupsPerWishlist {
		return nil, userError("Too many groups given.")
	}
	seen := make(map[uuid.UUID]bool, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := parseID("group_ids entry", s)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func updateWishlist(ctx context.Context, _ *mcp.CallToolRequest, args updateWishlistArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}

	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}

	wishlist, r, err := writableWishlist(userID, wishlistID, false)
	if err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	if args.Name != nil {
		name := strings.TrimSpace(*args.Name)
		if name != wishlist.Name {
			if err := validateName("wishlist", name); err != nil {
				return nil, nil, writeError(err, "failed to validate the name")
			}
			// Names are unique per owner, and a collaborator edits on the
			// owner's behalf, so check against the owner's wishlists.
			unique, err := database.VerifyUniqueWishlistNameForUser(name, wishlist.OwnerID)
			if err != nil {
				return nil, nil, errors.New("failed to verify the wishlist name")
			} else if !unique {
				return nil, nil, userError("There is already a wishlist with that name on your profile.")
			}
			wishlist.Name = name
		}
	}

	if args.Description != nil {
		description := strings.TrimSpace(*args.Description)
		if description != wishlist.Description {
			if err := validateText("description", description, maxDescriptionLength); err != nil {
				return nil, nil, writeError(err, "failed to validate the description")
			}
			wishlist.Description = description
		}
	}

	if args.Date != nil && strings.TrimSpace(*args.Date) != "" {
		date, err := parseDate(*args.Date)
		if err != nil {
			return nil, nil, err
		}
		wishlist.Date = &date
		wishlist.Expires = ptrTo(true)
	}
	if args.Expires != nil {
		wishlist.Expires = ptrTo(*args.Expires)
	}
	if args.Claimable != nil {
		wishlist.Claimable = ptrTo(*args.Claimable)
	}
	if args.HideClaimers != nil {
		wishlist.HideClaimers = ptrTo(*args.HideClaimers)
	}
	if args.Public != nil {
		// A fresh public link is only issued when a private wishlist is made
		// public again, so making it private and re-publishing revokes a
		// leaked link while ordinary edits keep shared links working.
		wasPublic := wishlist.Public != nil && *wishlist.Public
		if *args.Public && !wasPublic {
			wishlist.PublicHash = uuid.New()
		}
		wishlist.Public = ptrTo(*args.Public)
	}

	if wishlist.Public != nil && *wishlist.Public && wishlist.Claimable != nil && *wishlist.Claimable {
		return nil, nil, userError("A wishlist cannot have claimable wishes and be public to users without accounts.")
	}

	updated, err := database.UpdateWishlistInDB(wishlist)
	if err != nil {
		return nil, nil, errors.New("failed to update the wishlist")
	}
	return jsonResult(newWishlistWriteOut(updated, r)), nil, nil
}

func deleteWishlist(ctx context.Context, _ *mcp.CallToolRequest, args deleteWishlistArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
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

	// Same soft delete as the web app. Image files are left on disk: the web
	// route removes them, but that part can't be undone and the MCP has no way
	// to upload images, so it is not worth the risk from an agent.
	if err := database.DeleteWishlist(wishlistID); err != nil {
		return nil, nil, errors.New("failed to delete the wishlist")
	}
	return jsonResult(deletedOut{ID: wishlistID.String(), Deleted: true}), nil, nil
}
