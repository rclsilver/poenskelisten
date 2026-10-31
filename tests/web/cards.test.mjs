// Tests for web/js/cards.js: what a wish card offers depends on who looks.
import test from "node:test";
import assert from "node:assert/strict";
import { loadScripts, dictionary } from "./harness.mjs";

function setup() {
    const env = loadScripts(["core.js", "cards.js"]);
    env.P.setDictionary("en", dictionary("en"));
    return env;
}

const buttons = (el) => el.querySelectorAll("button").map((b) => b.textContent || b.getAttribute("aria-label"));
const links = (el) => el.querySelectorAll("a").map((a) => a.getAttribute("href"));

function wish(extra = {}) {
    return {
        id: "w1", name: "Lamp", note: "Terracotta", price: 79, url: "https://shop.example/lamp",
        image: false, wish_claimable: true, wishclaim: [], updated_at: "2026-10-01T10:00:00Z",
        currency: "€", currency_padding: true, currency_left: false,
        owner_id: { id: "owner" }, wishlist_owner: { id: "owner" }, collaborators: [],
        ...extra
    };
}

test("a group member can claim a free wish", () => {
    const { P } = setup();
    const card = P.wishCard(wish(), { mode: "member", userID: "me", showClaims: true, onClaim() {}, onUnclaim() {} });
    assert.deepEqual(buttons(card), ["I'll give it"]);
    assert.ok(card.textContent.includes("Available"));
    assert.ok(card.textContent.includes("79 €"));
    assert.deepEqual(links(card), ["https://shop.example/lamp"]);
});

test("the claimer can cancel, others see who claimed unless hidden", () => {
    const { P } = setup();
    const claimed = wish({ wishclaim: [{ user: { id: "bob", first_name: "Bob", last_name: "Martin" }, enabled: true }] });
    const mine = P.wishCard(claimed, { mode: "member", userID: "bob", showClaims: true, onClaim() {}, onUnclaim() {} });
    assert.deepEqual(buttons(mine), ["Cancel my claim"]);
    const other = P.wishCard(claimed, { mode: "member", userID: "carol", showClaims: true });
    assert.deepEqual(buttons(other), []);
    assert.ok(other.textContent.includes("Claimed by Bob Martin"));
    assert.ok(other.className.includes("claimed-other"));
    const hidden = P.wishCard(claimed, { mode: "member", userID: "carol", showClaims: true, hideClaimers: true });
    assert.ok(hidden.textContent.includes("Someone is giving it"));
    assert.ok(!hidden.textContent.includes("Bob"));
});

test("owners and collaborators edit and delete, and see no claim", () => {
    const { P } = setup();
    const card = P.wishCard(wish(), { mode: "manage", userID: "owner", showClaims: false, onDelete() {} });
    assert.deepEqual(buttons(card), ["Delete “Lamp”"]);
    assert.ok(links(card).includes("/wishes/w1/edit"));
    assert.ok(!card.textContent.includes("Available"));
    assert.ok(!card.textContent.includes("I'll give it"));
});

test("public visitors see that a wish is taken, never by whom", () => {
    const { P } = setup();
    const claimed = wish({ wishclaim: [{ user: { id: "bob", first_name: "Bob", last_name: "Martin" }, enabled: true }] });
    const card = P.wishCard(claimed, { mode: "public", userID: "", showClaims: true, signInHref: "/login" });
    assert.ok(card.textContent.includes("Claimed"));
    assert.ok(!card.textContent.includes("Bob"));
    assert.deepEqual(buttons(card), []);
    const free = P.wishCard(wish(), { mode: "public", userID: "", showClaims: true, signInHref: "/login" });
    assert.ok(links(free).includes("/login"));
});

test("a wish link is only shown for http(s) URLs", () => {
    const { P } = setup();
    const card = P.wishCard(wish({ url: "javascript:alert(document.cookie)" }), { mode: "member", userID: "me", showClaims: true, onClaim() {} });
    assert.deepEqual(links(card), []);
});

test("user text stays text in a card", () => {
    const { P } = setup();
    const card = P.wishCard(wish({ name: "<img src=x onerror=alert(1)>", note: "<script>x</script>" }), { mode: "member", userID: "me", showClaims: true, onClaim() {} });
    assert.equal(card.querySelectorAll("img").length, 0);
    assert.equal(card.querySelectorAll("script").length, 0);
    assert.ok(card.textContent.includes("<img src=x onerror=alert(1)>"));
});

test("a wish with a picture opens it enlarged", () => {
    const { P } = setup();
    const card = P.wishCard(wish({ image: true }), { mode: "member", userID: "me", showClaims: true, onClaim() {} });
    const opener = card.querySelector("button.wish-image") || card.querySelectorAll("button").find((b) => b.className.includes("wish-image"));
    assert.ok(opener, "the picture is a button");
    assert.equal(opener.getAttribute("aria-label"), "Enlarge the picture of Lamp");
});

test("the form preview shows the chosen picture and no actions", () => {
    const { P } = setup();
    const data = "data:image/png;base64,iVBORw0KGgo=";
    const card = P.wishCard({ name: "Draft", price: null, wish_claimable: false }, { mode: "preview", previewImage: data, currency: {} });
    assert.equal(card.querySelector("img").getAttribute("src"), data);
    assert.deepEqual(buttons(card), []);
    assert.deepEqual(links(card), []);
});

test("list cards link to the list and count its wishes", () => {
    const { P } = setup();
    const card = P.listCard({ id: "l1", name: "Birthday", description: "", owner: { id: "me" }, collaborators: [], wishes: [wish(), wish({ id: "w2" })], public: true, expires: false }, "me");
    assert.deepEqual(links(card), ["/wishlists/l1"]);
    assert.ok(card.textContent.includes("2 wishes"));
    assert.ok(card.textContent.includes("Your list"));
    assert.ok(card.textContent.includes("Public link"));
});
