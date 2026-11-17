package database

import (
	"testing"
	"time"

	"aunefyren/poenskelisten/models"

	"github.com/google/uuid"
)

// A reservation is created the first time and refreshed (not duplicated) the
// second: the same row keeps being the single live mark, with its note and
// author updated.
func TestSetWishReservationCreatesThenUpdates(t *testing.T) {
	setupTestDB(t)

	owner := createTestUser(t)
	spouse := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createWishForOwner(t, wishlist.ID, owner.ID, "Reservable")

	if _, err := SetWishReservationInDB(wish.ID, owner.ID, "aunt Claire"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	live, err := GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if live == nil || live.Note != "aunt Claire" || live.ReservedByUserID != owner.ID || !live.Enabled {
		t.Fatalf("unexpected live reservation: %+v", live)
	}
	firstID := live.ID

	if _, err := SetWishReservationInDB(wish.ID, spouse.ID, "grandma"); err != nil {
		t.Fatalf("second set: %v", err)
	}
	live, err = GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("get live after update: %v", err)
	}
	if live == nil || live.ID != firstID {
		t.Fatalf("expected the same row to be updated, got %+v (firstID=%v)", live, firstID)
	}
	if live.Note != "grandma" || live.ReservedByUserID != spouse.ID {
		t.Fatalf("expected note/author updated, got %+v", live)
	}

	var count int64
	if err := Instance.Model(&models.WishReservation{}).
		Where("wish_id = ? AND enabled = ? AND deleted_at IS NULL", wish.ID, true).
		Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one live reservation, got %d", count)
	}
}

// Disabling the mark hides it from GetLiveWishReservation, and disabling again
// when there is no live row is a no-op rather than an error.
func TestDisableWishReservationForWish(t *testing.T) {
	setupTestDB(t)

	owner := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createWishForOwner(t, wishlist.ID, owner.ID, "Reservable")

	if _, err := SetWishReservationInDB(wish.ID, owner.ID, "note"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := DisableWishReservationForWish(wish.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	live, err := GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if live != nil {
		t.Fatalf("expected no live reservation, got %+v", live)
	}
	if err := DisableWishReservationForWish(wish.ID); err != nil {
		t.Fatalf("second disable should be a no-op: %v", err)
	}
}

// A fresh wish carries no mark.
func TestGetLiveWishReservationNilWhenNone(t *testing.T) {
	setupTestDB(t)

	owner := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createWishForOwner(t, wishlist.ID, owner.ID, "Fresh")

	live, err := GetLiveWishReservation(wish.ID)
	if err != nil || live != nil {
		t.Fatalf("expected (nil, nil), got (%+v, %v)", live, err)
	}
}

// When a race left two live marks on one wish, EnsureOneReservationPerWish
// keeps the oldest and the restored index refuses a second live insert.
func TestEnsureOneReservationPerWishKeepsOldest(t *testing.T) {
	setupTestDB(t)
	if err := Instance.Exec("DROP INDEX idx_wish_reservations_one_live").Error; err != nil {
		t.Fatalf("failed to drop the index: %v", err)
	}

	owner := createTestUser(t)
	other := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createWishForOwner(t, wishlist.ID, owner.ID, "Reserved twice")

	seed := func(userID uuid.UUID, at time.Time) uuid.UUID {
		res := models.WishReservation{WishID: wish.ID, ReservedByUserID: userID, Enabled: true}
		res.ID = uuid.New()
		res.CreatedAt = at
		if err := Instance.Create(&res).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		return res.ID
	}
	now := time.Now()
	oldest := seed(owner.ID, now.Add(-time.Hour))
	newer := seed(other.ID, now)

	for round := 1; round <= 2; round++ {
		if err := EnsureOneReservationPerWish(); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	for id, want := range map[uuid.UUID]bool{oldest: true, newer: false} {
		var res models.WishReservation
		if err := Instance.Where("id = ?", id).First(&res).Error; err != nil {
			t.Fatal(err)
		}
		if res.Enabled != want {
			t.Errorf("reservation %s enabled = %v, want %v", id, res.Enabled, want)
		}
	}

	dup := models.WishReservation{WishID: wish.ID, ReservedByUserID: other.ID, Enabled: true}
	dup.ID = uuid.New()
	if err := Instance.Create(&dup).Error; !isUniqueViolation(err) {
		t.Fatalf("expected a unique violation inserting a second live reservation, got %v", err)
	}
}
