package controllers

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/logger"
	"aunefyren/poenskelisten/middlewares"
	"aunefyren/poenskelisten/models"
	"aunefyren/poenskelisten/utilities"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// wishReservationRefusal is the single 400 message every refused reservation
// request receives, whatever the reason: an unauthorized caller (including a
// plain group member), or a malformed, nil, or unknown wish id. Keeping the
// body byte-identical in all these cases means the endpoint never leaks whether
// a given wish exists.
const wishReservationRefusal = "You can only reserve wishes on wishlists you own or collaborate on."

// refuseWishReservation writes the shared refusal and aborts. All callers that
// are not owner-or-collaborator on an existing wish funnel through here, so the
// response is identical regardless of why access was denied.
func refuseWishReservation(context *gin.Context) {
	context.JSON(http.StatusBadRequest, gin.H{"error": wishReservationRefusal})
	context.Abort()
}

// authorizeWishReservation resolves the :wish_id, confirms it names a real wish,
// and reports whether userID may set or clear that wish's reservation (i.e. owns
// or collaborates on its wishlist). A malformed, nil, or unknown wish id, and a
// caller who is neither owner nor collaborator, all yield authorized=false with
// a nil error, so the handler refuses them identically and leaks nothing about
// which wishes exist. A non-nil error is a genuine database failure the handler
// should surface as a 500.
func authorizeWishReservation(wishIDParam string, userID uuid.UUID) (wishID uuid.UUID, authorized bool, err error) {
	wishID, parseErr := utilities.ParseID(wishIDParam)
	if parseErr != nil {
		return uuid.Nil, false, nil
	}

	wish, err := database.GetWishByWishID(wishID)
	if err != nil {
		return wishID, false, err
	} else if wish == nil {
		return wishID, false, nil
	}

	owner, err := database.VerifyUserOwnershipToWishlist(userID, wish.WishlistID)
	if err != nil {
		return wishID, false, err
	} else if owner {
		return wishID, true, nil
	}

	// Note the argument order differs from the ownership helper above.
	collaborator, err := database.VerifyWishlistCollaboratorToWishlist(wish.WishlistID, userID)
	if err != nil {
		return wishID, false, err
	}

	return wishID, collaborator, nil
}

// SetWishReservation handles POST /api/auth/wishes/:wish_id/reservation. The
// wishlist owner or a collaborator sets (or updates) the wish's "reserved" mark,
// optionally with an internal note. Anyone else - including a plain group member
// or a caller on an unknown/nil wish id - gets the shared silent refusal.
func SetWishReservation(context *gin.Context) {
	var wish_id = context.Param("wish_id")
	var request models.WishReservationCreationRequest

	if !bindJSON(context, &request) {
		return
	}

	// Get user ID
	UserID, err := middlewares.GetAuthUsername(context.GetHeader("Authorization"))
	if err != nil {
		logger.Log.Error("Failed to get user ID. Error: " + err.Error())
		context.JSON(http.StatusBadRequest, gin.H{"error": "Failed to get user ID."})
		context.Abort()
		return
	}

	wishID, authorized, err := authorizeWishReservation(wish_id, UserID)
	if err != nil {
		logger.Log.Error("Failed to authorize wish reservation. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify access to the wish."})
		context.Abort()
		return
	} else if !authorized {
		refuseWishReservation(context)
		return
	}

	_, err = database.SetWishReservationInDB(wishID, UserID, request.Note)
	if err != nil {
		logger.Log.Error("Failed to set wish reservation. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set the wish reservation."})
		context.Abort()
		return
	}

	context.JSON(http.StatusOK, gin.H{
		"message":     "Wish reservation set.",
		"reservation": models.WishReservationObject{Reserved: true, Note: request.Note},
	})
}

// RemoveWishReservation handles DELETE /api/auth/wishes/:wish_id/reservation. The
// wishlist owner or a collaborator clears the wish's "reserved" mark. Clearing is
// idempotent. Anyone else, and callers on an unknown/nil wish id, get the shared
// silent refusal.
func RemoveWishReservation(context *gin.Context) {
	var wish_id = context.Param("wish_id")

	// Get user ID
	UserID, err := middlewares.GetAuthUsername(context.GetHeader("Authorization"))
	if err != nil {
		logger.Log.Error("Failed to get user ID. Error: " + err.Error())
		context.JSON(http.StatusBadRequest, gin.H{"error": "Failed to get user ID."})
		context.Abort()
		return
	}

	wishID, authorized, err := authorizeWishReservation(wish_id, UserID)
	if err != nil {
		logger.Log.Error("Failed to authorize wish reservation. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify access to the wish."})
		context.Abort()
		return
	} else if !authorized {
		refuseWishReservation(context)
		return
	}

	if err := database.DisableWishReservationForWish(wishID); err != nil {
		logger.Log.Error("Failed to remove wish reservation. Error: " + err.Error())
		context.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove the wish reservation."})
		context.Abort()
		return
	}

	context.JSON(http.StatusOK, gin.H{"message": "Wish reservation removed."})
}
