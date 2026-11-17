package models

import "github.com/google/uuid"

// WishReservation is the owner/collaborator "reserved" mark on a wish. It is a
// concept distinct from WishClaim's surprise claim: the wishlist owner and its
// collaborators set it themselves, see it, and may attach an internal note.
// At most one live (enabled, non-deleted) row exists per wish. The gorm tags
// mirror WishClaim; the table is wish_reservations.
type WishReservation struct {
	GormModel
	WishID           uuid.UUID `json:"-" gorm:"type:varchar(100);"`
	Wish             Wish      `json:"wish" gorm:"not null;"`
	ReservedByUserID uuid.UUID `json:"-" gorm:"type:varchar(100);"`
	ReservedByUser   User      `json:"reserved_by_user" gorm:"not null;"`
	Note             string    `json:"note"`
	Enabled          bool      `json:"enabled" gorm:"not null;default: true"`
}

// WishReservationObject is the serialized view of a reservation mark. It exposes
// only whether the wish is reserved and the internal note (which callers blank
// for anyone but the wishlist owner/collaborators). ReservedByUserID is internal
// and never serialized.
type WishReservationObject struct {
	Reserved bool   `json:"reserved"`
	Note     string `json:"note"`
}

// WishReservationCreationRequest is the body of the set-reservation endpoint.
type WishReservationCreationRequest struct {
	Note string `json:"note" sanitize:"trim,text"`
}
