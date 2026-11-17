package database

import (
	"aunefyren/poenskelisten/models"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrWishAlreadyReserved means the wish got a live reservation before this one
// could be stored. It mirrors ErrWishAlreadyClaimed for the owner/collaborator
// reservation mark.
var ErrWishAlreadyReserved = errors.New("Wish is already reserved.")

// GetLiveWishReservation returns the single enabled, non-deleted reservation for
// a wish, or nil when the wish carries no live mark.
func GetLiveWishReservation(wishID uuid.UUID) (*models.WishReservation, error) {
	var reservation models.WishReservation

	record := Instance.
		Where("wish_id = ? AND enabled = ?", wishID, true).
		Find(&reservation)

	if record.Error != nil {
		return nil, record.Error
	} else if record.RowsAffected != 1 {
		return nil, nil
	}

	return &reservation, nil
}

// SetWishReservationInDB sets or refreshes the owner/collaborator reservation
// mark on a wish. When a live row exists its note and author are updated;
// otherwise a new row is created under the same race guard as
// CreateWishClaimInDB: on MySQL, which has no partial index, the wish is locked
// while its live reservations are counted; on the others the partial unique
// index from EnsureOneReservationPerWish turns a losing insert into
// ErrWishAlreadyReserved.
func SetWishReservationInDB(wishID, reservedByUserID uuid.UUID, note string) (models.WishReservation, error) {
	existing, err := GetLiveWishReservation(wishID)
	if err != nil {
		return models.WishReservation{}, err
	}
	if existing != nil {
		existing.Note = note
		existing.ReservedByUserID = reservedByUserID
		if err := Instance.Save(existing).Error; err != nil {
			return models.WishReservation{}, err
		}
		return *existing, nil
	}

	reservation := models.WishReservation{
		WishID:           wishID,
		ReservedByUserID: reservedByUserID,
		Note:             note,
		Enabled:          true,
	}
	reservation.ID = uuid.New()

	if Instance.Dialector.Name() == "mysql" {
		err = Instance.Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", wishID).Find(&models.Wish{}).Error; err != nil {
				return err
			}
			var live int64
			if err := tx.Model(&models.WishReservation{}).
				Where("wish_id = ? AND enabled = ?", wishID, true).
				Count(&live).Error; err != nil {
				return err
			} else if live > 0 {
				return ErrWishAlreadyReserved
			}
			return insertWishReservation(tx, &reservation)
		})
	} else {
		err = insertWishReservation(Instance, &reservation)
	}
	if err != nil {
		return models.WishReservation{}, err
	}
	return reservation, nil
}

func insertWishReservation(db *gorm.DB, reservation *models.WishReservation) error {
	record := db.Create(reservation)
	if isUniqueViolation(record.Error) {
		return ErrWishAlreadyReserved
	} else if record.Error != nil {
		return record.Error
	} else if record.RowsAffected != 1 {
		return errors.New("WishReservation not added to database.")
	}
	return nil
}

// DisableWishReservationForWish clears the live reservation mark on a wish by
// disabling its row. It is idempotent: a wish with no live mark is left as is
// without error.
func DisableWishReservationForWish(wishID uuid.UUID) error {
	return Instance.Model(&models.WishReservation{}).
		Where("wish_id = ? AND enabled = ?", wishID, true).
		Update("enabled", false).Error
}

// EnsureOneReservationPerWish adds the unique index that refuses a second live
// reservation on a wish. Reservations that would break it are released first:
// all but the oldest live reservation of a wish reserved twice by an earlier
// race. MySQL has no partial index; there the FOR UPDATE path in
// SetWishReservationInDB guards uniqueness, so this is a no-op.
func EnsureOneReservationPerWish() error {
	if Instance.Dialector.Name() == "mysql" {
		return nil
	}
	if err := Instance.Exec(`UPDATE wish_reservations SET enabled = ? WHERE id IN (
		SELECT id FROM (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY wish_id ORDER BY created_at, id) AS position
			FROM wish_reservations WHERE enabled = ? AND deleted_at IS NULL
		) ranked WHERE position > 1)`, false, true).Error; err != nil {
		return err
	}
	return Instance.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_reservations_one_live ON wish_reservations (wish_id) WHERE enabled AND deleted_at IS NULL").Error
}
