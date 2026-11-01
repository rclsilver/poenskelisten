package mcpserver

import (
	"aunefyren/poenskelisten/controllers"
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"context"
	"errors"
	"image"
	"math"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errNoWish is the wish counterpart of errNoAccess: the same answer whether
// the wish doesn't exist or isn't the caller's to touch.
var errNoWish = userError("wish not found or not accessible")

type createWishArgs struct {
	WishlistID   string   `json:"wishlist_id" jsonschema:"the ID of the wishlist to add the wish to (from list_wishlists); you must own it or collaborate on it"`
	Name         string   `json:"name" jsonschema:"the wish name, 5 to 200 characters, unique within the wishlist; must not contain < > \" or backticks"`
	Note         string   `json:"note,omitempty" jsonschema:"optional note"`
	URL          string   `json:"url,omitempty" jsonschema:"optional http(s) link to the item"`
	Price        *float64 `json:"price,omitempty" jsonschema:"optional price, zero or more"`
	CategoryName string   `json:"category_name,omitempty" jsonschema:"optional category: an existing one of the wishlist is reused by name, otherwise it is created"`
	Image        string   `json:"image,omitempty" jsonschema:"optional picture as a base64 data URI (data:image/jpeg;base64,... or data:image/png;base64,...), at most 10 MB decoded"`
}

type updateWishArgs struct {
	WishID       string   `json:"wish_id" jsonschema:"the ID of the wish to edit (from list_wishes)"`
	Name         *string  `json:"name,omitempty" jsonschema:"new name, same rules as when creating"`
	Note         *string  `json:"note,omitempty" jsonschema:"new note; an empty string clears it"`
	URL          *string  `json:"url,omitempty" jsonschema:"new http(s) link; an empty string clears it"`
	Price        *float64 `json:"price,omitempty" jsonschema:"new price, zero or more"`
	CategoryName *string  `json:"category_name,omitempty" jsonschema:"move the wish to this category (created if needed); an empty string removes it from its category"`
	Image        *string  `json:"image,omitempty" jsonschema:"set or replace the picture: a base64 data URI (data:image/jpeg;base64,... or data:image/png;base64,...), at most 10 MB decoded"`
	ImageDelete  bool     `json:"image_delete,omitempty" jsonschema:"remove the wish's picture; cannot be combined with image"`
}

type deleteWishArgs struct {
	WishID string `json:"wish_id" jsonschema:"the ID of the wish to delete (from list_wishes)"`
}

// wishWriteOut is wishOut plus where the wish lives. Claims are left out: only
// owners and collaborators get here, and they must not learn about them.
type wishWriteOut struct {
	wishOut
	WishlistID string `json:"wishlist_id"`
	Category   string `json:"category,omitempty"`
}

func newWishWriteOut(w models.Wish) wishWriteOut {
	out := wishWriteOut{
		wishOut:    wishOut{ID: w.ID.String(), Name: w.Name, Note: w.Note, URL: w.URL, Price: w.Price},
		WishlistID: w.WishlistID.String(),
	}
	if w.CategoryID != nil {
		if category, err := database.GetWishCategoryByID(*w.CategoryID); err == nil && category != nil {
			out.Category = category.Name
		}
	}
	return out
}

func registerWishWriteTools(server *mcp.Server) {
	no, yes := false, true

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_wish",
		Description: "Add a wish to a wishlist you own or collaborate on. Names must be unique within the wishlist. A picture can be given as a base64 data URI. Returns the new wish.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no},
	}, createWish)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_wish",
		Description: "Edit a wish on a wishlist you own or collaborate on. Only the fields you provide change; image sets or replaces the picture and image_delete removes it. Returns the updated wish.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, updateWish)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_wish",
		Description: "Delete a wish from a wishlist you own or collaborate on. It disappears from the wishlist but is only disabled in the database, not erased. Nobody who claimed it is notified.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true},
	}, deleteWish)
}

// writableWish loads a live wish the caller may change: web rules, i.e. the
// owner or a collaborator of its wishlist (not the wish's author, not a group
// member).
func writableWish(userID, wishID uuid.UUID) (models.Wish, error) {
	wish, err := database.GetWishByWishID(wishID)
	if err != nil {
		return models.Wish{}, errors.New("failed to load wish")
	} else if wish == nil {
		return models.Wish{}, errNoWish
	}
	if _, _, err := writableWishlist(userID, wish.WishlistID, false); err != nil {
		if errors.Is(err, errNoAccess) {
			return models.Wish{}, errNoWish
		}
		return models.Wish{}, err
	}
	return *wish, nil
}

func createWish(ctx context.Context, _ *mcp.CallToolRequest, args createWishArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}

	wishlistID, err := parseID("wishlist_id", args.WishlistID)
	if err != nil {
		return nil, nil, err
	}
	if _, _, err := writableWishlist(userID, wishlistID, false); err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	name := strings.TrimSpace(args.Name)
	note := strings.TrimSpace(args.Note)
	link := strings.TrimSpace(args.URL)
	if err := validateName("wish", name); err != nil {
		return nil, nil, writeError(err, "failed to validate the name")
	}
	if err := validateText("note", note, maxNoteLength); err != nil {
		return nil, nil, writeError(err, "failed to validate the note")
	}
	if err := validateWishURL(link); err != nil {
		return nil, nil, writeError(err, "failed to validate the URL")
	}
	if args.Price != nil {
		if err := validatePrice(*args.Price); err != nil {
			return nil, nil, err
		}
	}

	// Decoded before anything is written, so a refused image leaves no wish
	// behind.
	var wishImage image.Image
	if args.Image != "" {
		wishImage, err = controllers.DecodeImageUpload(args.Image)
		if err != nil {
			return nil, nil, userError(err.Error())
		}
	}

	unique, err := database.VerifyUniqueWishNameInWishlist(name, wishlistID)
	if err != nil {
		return nil, nil, errors.New("failed to verify the wish name")
	} else if !unique {
		return nil, nil, userError("There is already a wish with that name in this wishlist.")
	}

	categoryID, categoryError, err := controllers.ResolveWishCategoryForWish(wishlistID, userID, nil, args.CategoryName)
	if err != nil {
		return nil, nil, errors.New("failed to resolve the category")
	} else if categoryError != "" {
		return nil, nil, userError(categoryError)
	}

	wish := models.Wish{
		Name: name, Note: note, URL: link, Price: args.Price, Enabled: true,
		OwnerID: userID, WishlistID: wishlistID, CategoryID: categoryID,
	}
	wish.ID = uuid.New()
	created, err := database.CreateWishInDB(wish)
	if err != nil {
		return nil, nil, errors.New("failed to create the wish")
	}
	if wishImage != nil {
		if err := controllers.StoreWishImage(created.ID, wishImage); err != nil {
			return nil, nil, errors.New("the wish was created but its picture could not be saved")
		}
	}
	return jsonResult(newWishWriteOut(created)), nil, nil
}

func updateWish(ctx context.Context, _ *mcp.CallToolRequest, args updateWishArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}

	wishID, err := parseID("wish_id", args.WishID)
	if err != nil {
		return nil, nil, err
	}
	wish, err := writableWish(userID, wishID)
	if err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	var wishImage image.Image
	if args.Image != nil && *args.Image != "" {
		if args.ImageDelete {
			return nil, nil, userError("Give either image or image_delete, not both.")
		}
		wishImage, err = controllers.DecodeImageUpload(*args.Image)
		if err != nil {
			return nil, nil, userError(err.Error())
		}
	}

	if args.Name != nil {
		name := strings.TrimSpace(*args.Name)
		if name != wish.Name {
			if err := validateName("wish", name); err != nil {
				return nil, nil, writeError(err, "failed to validate the name")
			}
			unique, err := database.VerifyUniqueWishNameInWishlistExcludingWish(name, wish.WishlistID, wish.ID)
			if err != nil {
				return nil, nil, errors.New("failed to verify the wish name")
			} else if !unique {
				return nil, nil, userError("There is already a wish with that name in this wishlist.")
			}
			wish.Name = name
		}
	}
	if args.URL != nil {
		link := strings.TrimSpace(*args.URL)
		if link != wish.URL {
			if err := validateWishURL(link); err != nil {
				return nil, nil, writeError(err, "failed to validate the URL")
			}
			wish.URL = link
		}
	}
	if args.Note != nil {
		note := strings.TrimSpace(*args.Note)
		if note != wish.Note {
			if err := validateText("note", note, maxNoteLength); err != nil {
				return nil, nil, writeError(err, "failed to validate the note")
			}
			wish.Note = note
		}
	}
	if args.Price != nil {
		if err := validatePrice(*args.Price); err != nil {
			return nil, nil, err
		}
		wish.Price = args.Price
	}

	previousCategoryID := wish.CategoryID
	if args.CategoryName != nil {
		categoryID, categoryError, err := controllers.ResolveWishCategoryForWish(wish.WishlistID, userID, nil, *args.CategoryName)
		if err != nil {
			return nil, nil, errors.New("failed to resolve the category")
		} else if categoryError != "" {
			return nil, nil, userError(categoryError)
		}
		wish.CategoryID = categoryID
	}

	updated, err := database.UpdateWishInDB(wish)
	if err != nil {
		return nil, nil, errors.New("failed to update the wish")
	}

	if wishImage != nil {
		if err := controllers.StoreWishImage(updated.ID, wishImage); err != nil {
			return nil, nil, errors.New("the wish was updated but its picture could not be saved")
		}
	} else if args.ImageDelete {
		if err := controllers.DeleteWishImage(updated.ID); err != nil {
			return nil, nil, errors.New("the wish was updated but its picture could not be removed")
		}
	}

	// Like the web app, drop the category the wish left if it is now empty.
	if previousCategoryID != nil && (updated.CategoryID == nil || *previousCategoryID != *updated.CategoryID) {
		controllers.CleanupWishCategoryIfEmpty(*previousCategoryID)
	}
	return jsonResult(newWishWriteOut(updated)), nil, nil
}

func deleteWish(ctx context.Context, _ *mcp.CallToolRequest, args deleteWishArgs) (*mcp.CallToolResult, any, error) {
	userID, err := requireScope(ctx, scopeWishlistsWrite)
	if err != nil {
		return nil, nil, err
	}

	wishID, err := parseID("wish_id", args.WishID)
	if err != nil {
		return nil, nil, err
	}
	wish, err := writableWish(userID, wishID)
	if err != nil {
		return nil, nil, writeError(err, "failed to verify access")
	}

	// Same soft delete as the web app, minus its two irreversible side
	// effects: the image file is kept, and the users who claimed the wish are
	// not e-mailed (the MCP has no external side effects).
	wish.Enabled = false
	if _, err := database.UpdateWishInDB(wish); err != nil {
		return nil, nil, errors.New("failed to delete the wish")
	}
	if wish.CategoryID != nil {
		controllers.CleanupWishCategoryIfEmpty(*wish.CategoryID)
	}
	return jsonResult(deletedOut{ID: wishID.String(), Deleted: true}), nil, nil
}

// Upper bounds on free text specific to wishes; see the wishlist ones in write.go.
const (
	maxNoteLength = 2000
	maxURLLength  = 2048
)

// validateWishURL accepts the same URLs as the web app (anything that parses
// as a URL or as a host name) and refuses overlong ones.
func validateWishURL(value string) error {
	if err := validateText("URL", value, maxURLLength); err != nil {
		return err
	}
	if value == "" {
		return nil
	}
	// The web app ends up requiring a scheme and a host; the MCP also limits
	// the scheme to web links since the URL is rendered as a link.
	u, err := url.ParseRequestURI(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return userError("Invalid URL given.")
	}
	return nil
}

func validatePrice(price float64) error {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return userError("The price must be a positive number.")
	}
	return nil
}
