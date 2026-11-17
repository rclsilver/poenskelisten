# Owner/Collaborator Reserved Mark — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a wishlist's owner and collaborators mark a wish as "reserved" (with an internal note), a concept separate from the existing surprise claim.

**Architecture:** A new `WishReservation` table, independent of `WishClaim`, holds a per-wish live mark plus an internal note. New authenticated endpoints set/clear it (owner or collaborator only). The mark is surfaced on `WishObject.Reservation`; the note is blanked for anyone but the wishlist owner/collaborators (so the public view shows "reserved" without the note). The surprise-claim logic, its secrecy, its unique index and the public-view purge are left untouched. One UI badge "Réservé" (reused word) covers both concepts, with visibility — not vocabulary — carrying the secret.

**Tech Stack:** Go + GORM (SQLite/PostgreSQL/MySQL), Gin; vanilla JS front-end (`web/js`), node:test for web.

**Spec:** `docs/superpowers/specs/2026-11-17-owner-reservation-mark-design.md`

## Global Constraints

- Base branch: `feature/owner-reservation-mark`, off `origin/contrib` (`de0676a`). Do not push or deploy without explicit approval.
- Build/test gate (verbatim): `go test -tags nodynamic -race -covermode=atomic -coverprofile=coverage.out -timeout 20m ./...`; `gofmt` empty; `go vet -tags nodynamic ./...`. Web: `node --test "tests/web/*.test.mjs"`.
- Go tests sit next to the code, same package (`foo.go` → `foo_test.go`). DB tests use `setupTestDB(t)`; controller tests use `setupControllersDB(t)` (in-memory modernc SQLite). Shared fixtures in `database/helpers_test.go` and `controllers/helpers_test.go`.
- Front-end carries **no inline script/style**: build with `P.h`, attach listeners with `addEventListener`, set text with `textContent`. Never splice API values into markup.
- Reuse the existing badge word: i18n key `wish.claimed` ("Réservé" / "Reserved"). Do **not** introduce a new word for the mark's badge.
- `WishReservation.ReservedByUserID` is internal and **never** serialized into any JSON object.
- No commit message may contain AI mentions or `Co-Authored-By` unless the project's attribution rule says otherwise (this repo's session attribution adds `Co-Authored-By: Claude Opus 4.8` — keep it).

## Review Focus

- A **group member who is neither owner nor collaborator** POSTing/DELETEing the reservation endpoint must be refused identically to any other unauthorized user → Task 3 tests.
- A **non-existent or nil `wish_id`** must get the same silent refusal as "unauthorized" (no existence leak) → Task 3 tests.
- **Concurrent set** on the same wish must leave at most one live reservation on every engine (partial index on SQLite/PG, `FOR UPDATE` tx on MySQL) → Task 1 tests (SQLite index path) + code parity with claims for MySQL.
- The **public view** must expose `Reservation.Reserved == true` but `Note == ""` → Task 2 tests.
- **No surprise-claim regression:** on a claimable shared list the wishlist owner still sees no `WishClaim`, yet does see their own `Reservation.Reserved` → Task 2 tests.

---

### Task 1: Data model, migration, and DB layer

**Files:**
- Create: `models/wishreservation.go`
- Create: `database/wishreservation.go`
- Modify: `database/client.go` (AutoMigrate list near line 136; `EnsureOne*` startup calls near lines 159-165)
- Test: `database/wishreservation_test.go`

**Interfaces:**
- Produces:
  - `models.WishReservation{ GormModel; WishID uuid.UUID; Wish Wish; ReservedByUserID uuid.UUID; ReservedByUser User; Note string; Enabled bool }` (mirror `WishClaim`'s gorm tags; table `wish_reservations`).
  - `models.WishReservationObject{ Reserved bool \`json:"reserved"\`; Note string \`json:"note"\` }`
  - `models.WishReservationCreationRequest{ Note string \`json:"note"\` }`
  - `database.GetLiveWishReservation(wishID uuid.UUID) (*models.WishReservation, error)` — the single enabled, non-deleted row for the wish, or `nil`.
  - `database.SetWishReservationInDB(wishID, reservedByUserID uuid.UUID, note string) (models.WishReservation, error)` — if a live row exists, update its `Note` and `ReservedByUserID`; else create one. Create path uses the same race guard as `CreateWishClaimInDB` (MySQL: `Transaction` + `clause.Locking{Strength:"UPDATE"}` on the wish, count live, else insert; others: insert and map a unique-violation to a sentinel).
  - `database.DisableWishReservationForWish(wishID uuid.UUID) error` — set `enabled=false` on the live row (idempotent: no live row → no error).
  - `database.EnsureOneReservationPerWish() error` — MySQL: `return nil`. Else: disable all but the oldest live row per `wish_id` (same `ROW_NUMBER() OVER (PARTITION BY wish_id ORDER BY created_at, id)` pattern as `EnsureOneClaimPerWish`), then `CREATE UNIQUE INDEX IF NOT EXISTS idx_wish_reservations_one_live ON wish_reservations (wish_id) WHERE enabled AND deleted_at IS NULL`.
  - Reuse the existing `ErrWishAlreadyClaimed`-style sentinel pattern; define `ErrWishAlreadyReserved` in `database/wishreservation.go`.

- [ ] **Step 1: Write failing tests** in `database/wishreservation_test.go` (use `setupTestDB(t)`, `createTestUser`, `createTestWishlist`, `createWishForOwner`):
  - `TestSetWishReservationCreatesThenUpdates`: `SetWishReservationInDB(wish.ID, owner.ID, "aunt Claire")` → `GetLiveWishReservation` returns `Reserved`-equivalent row with `Note=="aunt Claire"`, `ReservedByUserID==owner.ID`; a second `SetWishReservationInDB(wish.ID, spouse.ID, "grandma")` updates the **same** row (still exactly one live row; `Note=="grandma"`, `ReservedByUserID==spouse.ID`).
  - `TestDisableWishReservationForWish`: after set, `DisableWishReservationForWish(wish.ID)` → `GetLiveWishReservation` returns `nil`; calling it again returns no error.
  - `TestGetLiveWishReservationNilWhenNone`: fresh wish → `nil, nil`.
  - `TestEnsureOneReservationPerWishKeepsOldest`: insert two enabled rows for one wish directly, run `EnsureOneReservationPerWish()`, assert exactly one stays enabled (the older `created_at`) and the index creation errors a duplicate insert afterward (SQLite path).
- [ ] **Step 2: Run** `go test -tags nodynamic ./database/ -run TestSetWishReservation -v` → FAIL (undefined symbols).
- [ ] **Step 3: Implement** `models/wishreservation.go` and `database/wishreservation.go` per the Interfaces block; add `Instance.AutoMigrate(&models.WishReservation{})` to the join in `database/client.go` (after the `WishClaim` migrate), and call `EnsureOneReservationPerWish()` alongside the existing `EnsureOneClaimPerWish()` startup calls (same error handling).
- [ ] **Step 4: Run** `go test -tags nodynamic -race ./database/ -run 'Reservation' -v` → PASS.
- [ ] **Step 5: Commit** `feat: store an owner/collaborator reservation mark on wishes`.

---

### Task 2: Expose the mark on `WishObject` with note visibility and public view

**Files:**
- Modify: `models/wish.go` (`WishObject`)
- Modify: `controllers/wish.go` (`ConvertWishToWishObject`, around lines 116-233)
- Modify: `controllers/wishlist.go` (`GetPublicWishlist`, lines 1448-1503 — verify only)
- Test: `controllers/wish_test.go`, `controllers/wishlist_test.go`

**Interfaces:**
- Consumes: `database.GetLiveWishReservation` (Task 1).
- Produces: `models.WishObject.Reservation *models.WishReservationObject \`json:"reservation"\`` — `nil` when no live mark; otherwise `{Reserved:true, Note:...}` with `Note` kept only for the wishlist owner or a collaborator, blank otherwise.

Note visibility keys on the **wishlist owner + collaborators**, reusing the already-computed `wishlistOwnerUser.ID` and `wishlistCollabObjects` in `ConvertWishToWishObject` (NOT `wish.OwnerID`). `requestUserID == nil` ⇒ note blank. `Reserved` is set for every viewer; only `Note` is gated. Do not alter the existing `WishClaim` purge (lines 169-197).

- [ ] **Step 1: Write failing tests**:
  - In `controllers/wish_test.go`, `TestConvertWishShowsReservationNoteOnlyToOwnerSide`: create owner, collaborator, group member; set a reservation with note "aunt Claire"; call `ConvertWishToWishObject(wish, &viewerID)` once per viewer and assert `Reservation.Reserved==true` for all three (and for `requestUserID==nil`), `Note=="aunt Claire"` for owner and collaborator, `Note==""` for the group member and for `nil`.
  - `TestConvertWishOwnerStillBlindToClaims`: on a claimable list, a surprise claim by a member plus an owner reservation mark → for the wishlist owner, `WishClaim` is empty **and** `Reservation.Reserved==true` (regression guard for the secret).
  - In `controllers/wishlist_test.go`, `TestPublicWishlistShowsReservedWithoutNote`: a public wishlist with a reserved wish (note set) → `GetPublicWishlist` response has the wish's `Reservation.Reserved==true`, `Note==""`, and (unchanged) `WishClaim` empty.
- [ ] **Step 2: Run** the three tests → FAIL (field/behavior absent).
- [ ] **Step 3: Implement**: add the `Reservation` field to `WishObject`; in `ConvertWishToWishObject`, after collaborators are resolved, load `GetLiveWishReservation(wish.ID)`, build the object, blank `Note` unless the requester is the wishlist owner or a collaborator. Confirm `GetPublicWishlist`'s cleanup loop (lines 1489-1495) leaves `Reservation` intact (note is already blank via `requestUserID==nil`); add no line that clears it.
- [ ] **Step 4: Run** `go test -tags nodynamic -race ./controllers/ -run 'Reservation|Blind' -v` → PASS.
- [ ] **Step 5: Commit** `feat: surface the reservation mark, hiding its note outside the owner side`.

---

### Task 3: Set/clear endpoints with owner-or-collaborator authorization

**Files:**
- Create: `controllers/wishreservation.go`
- Modify: `main.go` (route block near lines 299-302)
- Test: `controllers/wishreservation_test.go`

**Interfaces:**
- Consumes: `database.GetWishByWishID`, `database.SetWishReservationInDB`, `database.DisableWishReservationForWish` (Task 1); `database.VerifyUserOwnershipToWishlist(UserID, WishlistID)` and `database.VerifyWishlistCollaboratorToWishlist(WishlistID, UserID)` (mind the argument order — it differs between the two).
- Produces:
  - `controllers.SetWishReservation(context *gin.Context)` — `POST /api/auth/wishes/:wish_id/reservation`, body `WishReservationCreationRequest`. Loads the wish (absent ⇒ same refusal as unauthorized), derives its `WishlistID`, requires owner **or** collaborator, then `SetWishReservationInDB(wishID, callerID, note)`; 200 with `{"message": ..., "reservation": WishReservationObject{Reserved:true, Note:note}}`.
  - `controllers.RemoveWishReservation(context *gin.Context)` — `DELETE /api/auth/wishes/:wish_id/reservation`; same auth; `DisableWishReservationForWish`; 200.
  - Unauthorized/not-found/nil-id all return the **same** 400 body `{"error":"You can only reserve wishes on wishlists you own or collaborate on."}` (no existence leak). Reuse `utilities.ParseID` / `utilities.HasNilID` as the claim handlers do.

- [ ] **Step 1: Write failing tests** in `controllers/wishreservation_test.go` (`setupControllersDB(t)`, the shared router/auth fixtures used by `controllers/wish_test.go` for claim routes):
  - `TestOwnerCanSetAndClearReservation`: owner POSTs with note → 200, `GetLiveWishReservation` present; DELETE → 200, live row gone.
  - `TestCollaboratorCanSetReservation`: a collaborator (not group member) POSTs → 200.
  - `TestGroupMemberCannotReserve`: a plain group member POSTs → 400 with the exact shared error; no row created.
  - `TestStrangerAndMissingWishIdenticalRefusal`: an unrelated user, and any user on a nil/unknown `wish_id`, both get the same 400 body; assert the two responses are byte-identical.
- [ ] **Step 2: Run** `go test -tags nodynamic ./controllers/ -run 'Reserve|Reservation' -v` → FAIL.
- [ ] **Step 3: Implement** `controllers/wishreservation.go` and register the two routes in `main.go` next to the claim routes.
- [ ] **Step 4: Run** `go test -tags nodynamic -race ./controllers/ -run 'Reserve|Reservation' -v` → PASS.
- [ ] **Step 5: Commit** `feat: add endpoints to set and clear a wish reservation mark`.

---

### Task 4: Web — badge, owner/collaborator control, i18n

**Files:**
- Modify: `web/js/cards.js` (`P.wishCard`, around lines 92-140)
- Modify: `web/js/wishlist.js` (wire `onReserve`/`onUnreserve` to the endpoints)
- Modify: `web/json/lang-fr.json`, `web/json/lang-en.json`
- Test: `tests/web/cards.test.mjs`

**Interfaces:**
- Consumes: `wish.reservation` = `{reserved: bool, note: string}` or absent (Task 2); `ctx.mode` in `manage|member|public|preview`; new `ctx.onReserve(wish)`, `ctx.onUnreserve(wish)` callbacks (manage mode only).
- Produces: a single "Réservé" badge shown when the wish is reserved; a reserve/clear control + note display in manage mode.

Rules:
- Badge: show the `wish.claimed` badge when `wish.reservation && wish.reservation.reserved` (every non-preview mode), OR, as today, when `showClaims` and the surprise claim is taken. Never render two badges: if `reservation.reserved`, it alone drives the badge.
- Manage mode: add a button `wish.reserve` ("Marquer comme réservé" / "Mark as reserved") when not reserved, `wish.unreserve` ("Retirer la réservation" / "Remove reservation") when reserved, calling `ctx.onReserve`/`ctx.onUnreserve`; when reserved and `reservation.note`, show it (text node, `textContent`). The note input/prompt detail is the implementer's choice; keep it CSP-safe.
- New i18n keys (both files): `wish.reserve`, `wish.unreserve`, `wish.reservationNote` ("Note (interne)" / "Internal note"). Reuse `wish.claimed` for the badge.

- [ ] **Step 1: Write failing tests** in `tests/web/cards.test.mjs` (extend the `wish()` fixture with `reservation`):
  - `"a reserved wish shows the Réservé badge on the public link"`: `wishCard(wish({wish_claimable:false, reservation:{reserved:true, note:""}}), {mode:"public"})` → `card.textContent` includes the `wish.claimed` label; the note is absent.
  - `"the owner sees a clear-reservation control and the internal note"`: `mode:"manage"`, `reservation:{reserved:true, note:"aunt Claire"}`, with `onReserve/onUnreserve` → buttons include "Remove reservation"; text includes "aunt Claire".
  - `"an unreserved wish in manage mode offers to reserve"`: `mode:"manage"`, no reservation → buttons include "Mark as reserved".
  - `"only one badge when a wish is both claimed and owner-reserved"`: `mode:"member"`, `showClaims:true`, a taken `wishclaim` **and** `reservation.reserved:true` → exactly one badge element rendered.
- [ ] **Step 2: Run** `node --test tests/web/cards.test.mjs` → FAIL.
- [ ] **Step 3: Implement** the badge/control in `cards.js`, the new i18n keys, and wire `onReserve/onUnreserve` in `wishlist.js` to `POST`/`DELETE /api/auth/wishes/:id/reservation` then refresh.
- [ ] **Step 4: Run** `node --test "tests/web/*.test.mjs"` → PASS (all, no regression).
- [ ] **Step 5: Commit** `feat: show and toggle the reservation mark in the web UI`.

---

### Task 5: Full verification and docs

**Files:**
- Modify: `README.md` or `docs/` if the reservation concept is documented there (check `docs/ui.md`); otherwise none.

- [ ] **Step 1:** `gofmt -l .` → empty.
- [ ] **Step 2:** `go vet -tags nodynamic ./...` → clean.
- [ ] **Step 3:** `go test -tags nodynamic -race -covermode=atomic -coverprofile=coverage.out -timeout 20m ./...` → PASS; `go tool cover -func=coverage.out | tail -1` ≥ CI floor.
- [ ] **Step 4:** `node --test "tests/web/*.test.mjs"` → all PASS.
- [ ] **Step 5:** If `docs/ui.md` describes claims, add a short paragraph on the owner/collaborator reservation mark (internal note, visible as "Réservé" on the public link). Commit `docs: note the owner/collaborator reservation mark`.
