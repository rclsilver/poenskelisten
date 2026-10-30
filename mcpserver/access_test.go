package mcpserver

import (
	"aunefyren/poenskelisten/database"
	"aunefyren/poenskelisten/models"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func createMCPWishlist(t *testing.T, owner models.User, name string, hideClaimers bool) models.Wishlist {
	t.Helper()
	now := time.Now()
	truth := true
	wishlist := models.Wishlist{
		Name: name, Enabled: true, OwnerID: owner.ID, Date: &now,
		Claimable: &truth, HideClaimers: &hideClaimers,
	}
	wishlist.ID = uuid.New()
	if r := database.Instance.Create(&wishlist); r.Error != nil {
		t.Fatalf("failed to create wishlist: %v", r.Error)
	}
	return wishlist
}

func createMCPWish(t *testing.T, owner models.User, wishlist models.Wishlist, name string) models.Wish {
	t.Helper()
	wish := models.Wish{Name: name, Enabled: true, OwnerID: owner.ID, WishlistID: wishlist.ID}
	wish.ID = uuid.New()
	if r := database.Instance.Create(&wish); r.Error != nil {
		t.Fatalf("failed to create wish: %v", r.Error)
	}
	return wish
}

func createMCPClaim(t *testing.T, claimer models.User, wish models.Wish) {
	t.Helper()
	claim := models.WishClaim{WishID: wish.ID, UserID: claimer.ID, Enabled: true}
	claim.ID = uuid.New()
	if r := database.Instance.Create(&claim); r.Error != nil {
		t.Fatalf("failed to create claim: %v", r.Error)
	}
}

func createMCPCollaborator(t *testing.T, user models.User, wishlist models.Wishlist) {
	t.Helper()
	collab := models.WishlistCollaborator{UserID: user.ID, WishlistID: wishlist.ID, Enabled: true}
	collab.ID = uuid.New()
	if r := database.Instance.Create(&collab); r.Error != nil {
		t.Fatalf("failed to create collaborator: %v", r.Error)
	}
}

// shareMCPWishlist puts the wishlist in a new group that the given users belong to.
func shareMCPWishlist(t *testing.T, owner models.User, wishlist models.Wishlist, members ...models.User) {
	t.Helper()
	group := models.Group{Name: "Group " + uuid.NewString(), Enabled: true, OwnerID: owner.ID}
	group.ID = uuid.New()
	if r := database.Instance.Create(&group); r.Error != nil {
		t.Fatalf("failed to create group: %v", r.Error)
	}
	for _, m := range members {
		gm := models.GroupMembership{GroupID: group.ID, MemberID: m.ID, Enabled: true}
		gm.ID = uuid.New()
		if r := database.Instance.Create(&gm); r.Error != nil {
			t.Fatalf("failed to create membership: %v", r.Error)
		}
	}
	wm := models.WishlistMembership{GroupID: group.ID, WishlistID: wishlist.ID, Enabled: true}
	wm.ID = uuid.New()
	if r := database.Instance.Create(&wm); r.Error != nil {
		t.Fatalf("failed to create wishlist membership: %v", r.Error)
	}
}

func TestResolveRole(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	member := createMCPTestUser(t, "Cleo")
	stranger := createMCPTestUser(t, "Dan")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	createMCPCollaborator(t, collab, wishlist)
	shareMCPWishlist(t, owner, wishlist, member)

	cases := []struct {
		name string
		user models.User
		want role
	}{
		{"owner", owner, roleOwner},
		{"collaborator", collab, roleCollaborator},
		{"group member", member, roleGroupMember},
	}
	for _, c := range cases {
		got, err := resolveRole(c.user.ID, wishlist.ID)
		if err != nil || got != c.want {
			t.Errorf("%s: got (%q, %v), want %q", c.name, got, err, c.want)
		}
	}

	if _, err := resolveRole(stranger.ID, wishlist.ID); !errors.Is(err, errNoAccess) {
		t.Errorf("stranger: err = %v, want errNoAccess", err)
	}
	if _, err := resolveRole(owner.ID, uuid.New()); !errors.Is(err, errNoAccess) {
		t.Errorf("unknown wishlist: err = %v, want errNoAccess", err)
	}
}

func TestResolveRolePrefersOwnerOverMembership(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	shareMCPWishlist(t, owner, wishlist, owner)

	if got, _ := resolveRole(owner.ID, wishlist.ID); got != roleOwner {
		t.Errorf("role = %q, want owner", got)
	}
}

func TestResolveRoleMemberOfTwoGroupsSharingTheList(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	member := createMCPTestUser(t, "Cleo")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	shareMCPWishlist(t, owner, wishlist, member)
	shareMCPWishlist(t, owner, wishlist, member)

	if got, err := resolveRole(member.ID, wishlist.ID); err != nil || got != roleGroupMember {
		t.Errorf("got (%q, %v), want group_member", got, err)
	}
}

func TestAccessibleWishlistsDeduplicatesWithStrongestRole(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	other := createMCPTestUser(t, "Bob")
	mine := createMCPWishlist(t, owner, "Mine", false)
	shareMCPWishlist(t, owner, mine, owner) // owned AND shared with own group
	theirs := createMCPWishlist(t, other, "Theirs", false)
	createMCPCollaborator(t, owner, theirs)
	shareMCPWishlist(t, other, theirs, owner) // collaborator AND group member
	createMCPWishlist(t, other, "Invisible", false)

	got, err := accessibleWishlists(owner.ID)
	if err != nil {
		t.Fatalf("accessibleWishlists: %v", err)
	}
	roles := map[string]role{}
	for _, rw := range got {
		roles[rw.wishlist.Name] = rw.role
	}
	if len(got) != 2 || roles["Mine"] != roleOwner || roles["Theirs"] != roleCollaborator {
		t.Errorf("got %+v, want Mine=owner and Theirs=collaborator only", roles)
	}
}

func TestClaimsVisible(t *testing.T) {
	author, viewer := uuid.New(), uuid.New()
	cases := []struct {
		name   string
		r      role
		wishBy uuid.UUID
		want   bool
	}{
		{"owner", roleOwner, author, false},
		{"collaborator", roleCollaborator, author, false},
		{"group member", roleGroupMember, author, true},
		{"group member who wrote the wish", roleGroupMember, viewer, false},
		{"public stranger", "", author, true},
	}
	for _, c := range cases {
		if got := claimsVisible(c.r, c.wishBy, viewer); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestResolveRoleDisabledWishlist(t *testing.T) {
	setupMCPDB(t)
	owner := createMCPTestUser(t, "Ada")
	collab := createMCPTestUser(t, "Bob")
	member := createMCPTestUser(t, "Cleo")
	wishlist := createMCPWishlist(t, owner, "Birthday", false)
	createMCPCollaborator(t, collab, wishlist)
	shareMCPWishlist(t, owner, wishlist, member)
	database.Instance.Model(&models.Wishlist{}).Where("id = ?", wishlist.ID).Update("enabled", false)

	// Collaboration and group rows outlive a deleted wishlist; none of them
	// may keep granting access to it.
	for _, u := range []models.User{owner, collab, member} {
		if _, err := resolveRole(u.ID, wishlist.ID); !errors.Is(err, errNoAccess) {
			t.Errorf("%s: err = %v, want errNoAccess", u.FirstName, err)
		}
	}
}
