package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"aunefyren/poenskelisten/utilities"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const scopeWishlistsWrite = "mcp:wishlists.write"

// Upper bounds on free text. The web app has none, but an agent can produce
// arbitrarily long input, so the MCP tools cap what they will store.
const (
	maxNameLength        = 200
	maxDescriptionLength = 2000
	maxGroupsPerWishlist = 50
)

// errNotOwner and errNotEditor are only returned to callers who can already
// read the wishlist, so they don't reveal anything about its existence.
var (
	errNotOwner  = errors.New("only the owner of a wishlist can do this")
	errNotEditor = errors.New("only the owner or a collaborator of a wishlist can do this")
)

// userError is a validation problem the caller can fix; it is shown as is,
// unlike internal failures which are replaced by a generic message.
type userError string

func (e userError) Error() string { return string(e) }

// isUserError reports whether err is safe to show to the caller verbatim.
func isUserError(err error) bool {
	var ue userError
	return errors.As(err, &ue) ||
		errors.Is(err, errNoAccess) || errors.Is(err, errNotOwner) || errors.Is(err, errNotEditor) ||
		errors.Is(err, errNoGroup)
}

// writableWishlist loads a live wishlist the caller may edit. needOwner
// mirrors the web routes: deleting a wishlist is owner-only, editing it (and
// its wishes) is open to collaborators too. A caller with no access at all
// gets errNoAccess, exactly like the read tools.
func writableWishlist(userID, wishlistID uuid.UUID, needOwner bool) (models.Wishlist, role, error) {
	r, err := resolveRole(userID, wishlistID)
	if err != nil {
		return models.Wishlist{}, "", err
	}
	if needOwner && r != roleOwner {
		return models.Wishlist{}, "", errNotOwner
	}
	if r != roleOwner && r != roleCollaborator {
		return models.Wishlist{}, "", errNotEditor
	}

	wishlist, err := database.GetWishlist(wishlistID)
	if errors.Is(err, database.ErrWishlistNotFound) {
		return models.Wishlist{}, "", errNoAccess
	} else if err != nil {
		return models.Wishlist{}, "", fmt.Errorf("failed to load wishlist: %w", err)
	}
	return wishlist, r, nil
}

// validateText applies the web app's character rules plus a length cap.
func validateText(label, value string, maxLength int) error {
	if utf8.RuneCountInString(value) > maxLength {
		return userError(fmt.Sprintf("The %s must be at most %d characters.", label, maxLength))
	}
	ok, requirements, err := utilities.ValidateTextCharacters(value)
	if err != nil {
		return fmt.Errorf("failed to validate text: %w", err)
	}
	if !ok {
		return userError(requirements)
	}
	return nil
}

// validateName is validateText plus the web app's five-letter minimum.
func validateName(label, value string) error {
	if len(value) < 5 {
		return userError(fmt.Sprintf("The name of the %s must be five or more letters.", label))
	}
	return validateText("name of the "+label, value, maxNameLength)
}

// parseDate reads a YYYY-MM-DD date as midnight UTC, which is what the web
// form's date input ends up sending.
func parseDate(value string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, userError("The date must be formatted YYYY-MM-DD.")
	}
	return d, nil
}

func parseID(label, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, userError("invalid " + label)
	}
	return id, nil
}

// writeError turns an internal failure into the message the caller sees.
func writeError(err error, generic string) error {
	if isUserError(err) {
		return err
	}
	return errors.New(generic)
}

func ptrTo[T any](v T) *T { return &v }
