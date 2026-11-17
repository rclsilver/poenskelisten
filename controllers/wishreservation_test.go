package controllers

import (
	"aunefyren/poenskelisten/database"
	"bytes"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// reservationRefusalBody is the exact 400 body shared by every refused
// reservation request, whatever the reason (unauthorized caller, unknown wish,
// nil/invalid wish id) - so the endpoint never leaks whether a wish exists.
const reservationRefusalBody = `{"error":"You can only reserve wishes on wishlists you own or collaborate on."}`

func TestOwnerCanSetAndClearReservation(t *testing.T) {
	setupControllersDB(t)
	owner := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createTestWish(t, owner.ID, wishlist.ID)

	w, ctx := wishCtx("POST", "/api/wishes/"+wish.ID.String()+"/reservation", `{"note":"aunt Claire"}`)
	ctx.Params = gin.Params{{Key: "wish_id", Value: wish.ID.String()}}
	ctx.Request.Header.Set("Authorization", authHeader(t, owner.ID, false))
	SetWishReservation(ctx)
	if w.Code != 200 {
		t.Fatalf("set status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	live, err := database.GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("GetLiveWishReservation error: %v", err)
	}
	if live == nil {
		t.Fatalf("expected a live reservation after set, got nil")
	}
	if live.Note != "aunt Claire" {
		t.Errorf("note = %q, want \"aunt Claire\"", live.Note)
	}
	if live.ReservedByUserID != owner.ID {
		t.Errorf("reserved_by = %v, want owner %v", live.ReservedByUserID, owner.ID)
	}

	w2, ctx2 := wishCtx("DELETE", "/api/wishes/"+wish.ID.String()+"/reservation", "")
	ctx2.Params = gin.Params{{Key: "wish_id", Value: wish.ID.String()}}
	ctx2.Request.Header.Set("Authorization", authHeader(t, owner.ID, false))
	RemoveWishReservation(ctx2)
	if w2.Code != 200 {
		t.Fatalf("remove status = %d, want 200; body=%s", w2.Code, w2.Body.String())
	}

	live2, err := database.GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("GetLiveWishReservation error after remove: %v", err)
	}
	if live2 != nil {
		t.Errorf("expected no live reservation after remove, got %+v", live2)
	}
}

func TestCollaboratorCanSetReservation(t *testing.T) {
	setupControllersDB(t)
	owner := createTestUser(t)
	collaborator := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	// A collaborator who is NOT a group member of the list.
	addWishCollaborator(t, wishlist.ID, collaborator.ID)
	wish := createTestWish(t, owner.ID, wishlist.ID)

	w, ctx := wishCtx("POST", "/api/wishes/"+wish.ID.String()+"/reservation", `{"note":"grandma"}`)
	ctx.Params = gin.Params{{Key: "wish_id", Value: wish.ID.String()}}
	ctx.Request.Header.Set("Authorization", authHeader(t, collaborator.ID, false))
	SetWishReservation(ctx)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 for a collaborator; body=%s", w.Code, w.Body.String())
	}

	live, err := database.GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("GetLiveWishReservation error: %v", err)
	}
	if live == nil || live.Note != "grandma" {
		t.Fatalf("expected a live reservation with the collaborator's note, got %+v", live)
	}
}

func TestGroupMemberCannotReserve(t *testing.T) {
	setupControllersDB(t)
	owner := createTestUser(t)
	member := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	group := createTestGroup(t, owner.ID)
	addGroupMembership(t, group.ID, member.ID)
	addWishlistMembership(t, wishlist.ID, group.ID)
	wish := createTestWish(t, owner.ID, wishlist.ID)

	w, ctx := wishCtx("POST", "/api/wishes/"+wish.ID.String()+"/reservation", `{"note":"sneaky"}`)
	ctx.Params = gin.Params{{Key: "wish_id", Value: wish.ID.String()}}
	ctx.Request.Header.Set("Authorization", authHeader(t, member.ID, false))
	SetWishReservation(ctx)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for a plain group member; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != reservationRefusalBody {
		t.Errorf("body = %s, want the shared refusal %s", got, reservationRefusalBody)
	}

	live, err := database.GetLiveWishReservation(wish.ID)
	if err != nil {
		t.Fatalf("GetLiveWishReservation error: %v", err)
	}
	if live != nil {
		t.Errorf("expected no reservation created by a refused group member, got %+v", live)
	}
}

func TestStrangerAndMissingWishIdenticalRefusal(t *testing.T) {
	setupControllersDB(t)
	owner := createTestUser(t)
	stranger := createTestUser(t)
	wishlist := createTestWishlist(t, owner.ID)
	wish := createTestWish(t, owner.ID, wishlist.ID)

	// An unrelated user on a real wish.
	wStranger, ctxStranger := wishCtx("POST", "/api/wishes/"+wish.ID.String()+"/reservation", `{"note":"x"}`)
	ctxStranger.Params = gin.Params{{Key: "wish_id", Value: wish.ID.String()}}
	ctxStranger.Request.Header.Set("Authorization", authHeader(t, stranger.ID, false))
	SetWishReservation(ctxStranger)
	if wStranger.Code != 400 {
		t.Fatalf("stranger status = %d, want 400; body=%s", wStranger.Code, wStranger.Body.String())
	}

	// Any user on an unknown wish id.
	unknown := uuid.NewString()
	wUnknown, ctxUnknown := wishCtx("POST", "/api/wishes/"+unknown+"/reservation", `{"note":"x"}`)
	ctxUnknown.Params = gin.Params{{Key: "wish_id", Value: unknown}}
	ctxUnknown.Request.Header.Set("Authorization", authHeader(t, owner.ID, false))
	SetWishReservation(ctxUnknown)
	if wUnknown.Code != 400 {
		t.Fatalf("unknown-wish status = %d, want 400; body=%s", wUnknown.Code, wUnknown.Body.String())
	}

	// A nil wish id must be refused the same way (no existence leak).
	nilID := uuid.Nil.String()
	wNil, ctxNil := wishCtx("POST", "/api/wishes/"+nilID+"/reservation", `{"note":"x"}`)
	ctxNil.Params = gin.Params{{Key: "wish_id", Value: nilID}}
	ctxNil.Request.Header.Set("Authorization", authHeader(t, owner.ID, false))
	SetWishReservation(ctxNil)
	if wNil.Code != 400 {
		t.Fatalf("nil-wish status = %d, want 400; body=%s", wNil.Code, wNil.Body.String())
	}

	if !bytes.Equal(wStranger.Body.Bytes(), wUnknown.Body.Bytes()) {
		t.Errorf("stranger and unknown-wish bodies differ:\n stranger=%s\n unknown =%s", wStranger.Body.String(), wUnknown.Body.String())
	}
	if !bytes.Equal(wStranger.Body.Bytes(), wNil.Body.Bytes()) {
		t.Errorf("stranger and nil-wish bodies differ:\n stranger=%s\n nil     =%s", wStranger.Body.String(), wNil.Body.String())
	}
	if got := wStranger.Body.String(); got != reservationRefusalBody {
		t.Errorf("refusal body = %s, want %s", got, reservationRefusalBody)
	}
}
