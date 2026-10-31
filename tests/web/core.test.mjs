// Unit tests for web/js/core.js. Run with: node --test tests/web/
import test from "node:test";
import assert from "node:assert/strict";
import { loadCore, memoryStorage } from "./harness.mjs";

// Values built inside the VM have its prototypes; compare their JSON form.
const plain = (value) => JSON.parse(JSON.stringify(value));

test("detectLang prefers the saved choice, then the browser, then English", () => {
    const { P } = loadCore();
    assert.equal(P.detectLang("fr", ["en-US"]), "fr");
    assert.equal(P.detectLang(null, ["de-DE", "fr-CA"]), "fr");
    assert.equal(P.detectLang("xx", ["de"]), "en");
    assert.equal(P.detectLang(null, []), "en");
});

test("t fills placeholders and the application name, and shows unknown keys as is", () => {
    const { P } = loadCore({ appName: "Nos envies" });
    P.setDictionary("fr", { "login.withProvider": "Se connecter avec {provider}", "register.title": "Rejoignez {app}." });
    assert.equal(P.t("login.withProvider", { provider: "Authelia" }), "Se connecter avec Authelia");
    assert.equal(P.t("register.title"), "Rejoignez Nos envies.");
    assert.equal(P.t("no.such.key"), "no.such.key");
});

test("tn follows each language's plural rules", () => {
    const { P } = loadCore();
    P.setDictionary("fr", { "x.one": "{n} souhait", "x.other": "{n} souhaits" });
    assert.equal(P.tn("x", 0), "0 souhait");
    assert.equal(P.tn("x", 1), "1 souhait");
    assert.equal(P.tn("x", 2), "2 souhaits");
    P.setDictionary("en", { "x.one": "{n} wish", "x.other": "{n} wishes" });
    assert.equal(P.tn("x", 0), "0 wishes");
    assert.equal(P.tn("x", 1), "1 wish");
});

test("appName falls back to Pønskelisten when the server sent none", () => {
    assert.equal(loadCore().P.appName(), "Pønskelisten");
    assert.equal(loadCore({ appName: "  " }).P.appName(), "Pønskelisten");
    assert.equal(loadCore({ appName: "A <b>" }).P.appName(), "A <b>");
});

test("h sets text as text, never as markup", () => {
    const { P } = loadCore();
    const el = P.h("p", { class: "note" }, "<img src=x onerror=alert(1)>", P.h("strong", { text: "<b>bold</b>" }));
    assert.equal(el.childNodes.length, 2);
    assert.equal(el.childNodes[0].nodeType, 3);
    assert.equal(el.textContent, "<img src=x onerror=alert(1)><b>bold</b>");
    assert.equal(el.innerHTML, '&lt;img src=x onerror=alert(1)&gt;<strong>&lt;b&gt;bold&lt;/b&gt;</strong>');
});

test("h refuses inline handlers and markup injection", () => {
    const { P } = loadCore();
    assert.throws(() => P.h("div", { onclick: "alert(1)" }));
    assert.throws(() => P.h("div", { innerHTML: "<b>x</b>" }));
    assert.throws(() => P.h("div", { attrs: { onmouseover: "alert(1)" } }));
    let clicked = false;
    const button = P.h("button", { on: { click: () => { clicked = true; } } });
    button.dispatch("click");
    assert.ok(clicked);
});

test("h only builds safe links and image sources", () => {
    const { P } = loadCore();
    assert.equal(P.h("a", { href: "javascript:alert(1)" }).getAttribute("href"), "#");
    assert.equal(P.h("a", { href: "//evil.example/x" }).getAttribute("href"), "https://evil.example/x");
    assert.equal(P.h("a", { href: "/wishlists/1" }).getAttribute("href"), "/wishlists/1");
    assert.equal(P.h("a", { href: "https://shop.example/a?b=1" }).getAttribute("href"), "https://shop.example/a?b=1");
    assert.equal(P.h("img", { src: "https://tracker.example/p.gif" }).getAttribute("src"), null);
    assert.equal(P.h("img", { src: "data:text/html;base64,PGI+" }).getAttribute("src"), null);
    assert.equal(P.h("img", { src: "data:image/png;base64,iVBORw0KGgo=" }).getAttribute("src"), "data:image/png;base64,iVBORw0KGgo=");
    assert.equal(P.h("img", { src: "/assets/illustrations/cup.svg" }).getAttribute("src"), "/assets/illustrations/cup.svg");
});

test("safeExternalUrl keeps http(s) only, like the server reads bare domains", () => {
    const { P } = loadCore();
    assert.equal(P.safeExternalUrl("https://shop.example/item"), "https://shop.example/item");
    assert.equal(P.safeExternalUrl("shop.example/item"), "https://shop.example/item");
    assert.equal(P.safeExternalUrl("  http://shop.example  "), "http://shop.example/");
    for (const bad of ["javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,x", "ftp://files.example", "https://user:pw@shop.example", "", "   ", "vbscript:x"]) {
        assert.equal(P.safeExternalUrl(bad), null, bad);
    }
});

test("externalLink opens shops in a new tab without referrer or opener", () => {
    const { P } = loadCore();
    const link = P.externalLink("https://shop.example/item", "Voir");
    assert.equal(link.getAttribute("href"), "https://shop.example/item");
    assert.equal(link.target, "_blank");
    assert.equal(link.rel, "noopener noreferrer");
    assert.equal(P.externalLink("javascript:alert(1)", "x"), null);
});

test("safeLocalPath only follows paths on this site", () => {
    const { P } = loadCore();
    assert.equal(P.safeLocalPath("/oauth/authorize?client_id=x&state=y"), "/oauth/authorize?client_id=x&state=y");
    for (const bad of ["//evil.example", "/\\evil.example", "https://evil.example/", "javascript:alert(1)", "", null, "/\nx"]) {
        assert.equal(P.safeLocalPath(bad), "/", String(bad));
    }
});

test("image checks accept JPEG and PNG up to 10 MB", () => {
    const { P } = loadCore();
    assert.equal(P.checkImageFile({ type: "image/png", size: 10000000 }), null);
    assert.equal(P.checkImageFile({ type: "image/jpeg", size: 1 }), null);
    assert.equal(P.checkImageFile({ type: "image/gif", size: 1 }), "image.badType");
    assert.equal(P.checkImageFile({ type: "image/svg+xml", size: 1 }), "image.badType");
    assert.equal(P.checkImageFile({ type: "image/png", size: 10000001 }), "image.tooBig");
    assert.ok(P.isImageDataUrl("data:image/jpeg;base64,/9j/4AAQ"));
    assert.ok(!P.isImageDataUrl("data:image/jpeg;base64,/9j/\"><script>"));
    assert.ok(!P.isImageDataUrl("javascript:alert(1)"));
});

test("fmtPrice follows the currency settings and hides empty prices", () => {
    const { P } = loadCore();
    P.setDictionary("en", {});
    assert.equal(P.fmtPrice(12, { currency: "NOK", padding: true, left: false }), "12 NOK");
    assert.equal(P.fmtPrice(12.5, { currency: "$", padding: false, left: true }), "$12.50");
    assert.equal(P.fmtPrice(0, { currency: "€" }), "");
    assert.equal(P.fmtPrice(null, { currency: "€" }), "");
    P.setDictionary("fr", {});
    assert.equal(P.fmtPrice(1234.5, { currency: "€", padding: true }).replace(/\s/g, " "), "1 234,50 €");
});

test("calendar days travel as midnight UTC both ways", () => {
    const { P } = loadCore();
    assert.equal(P.dayToApi("2026-12-24"), "2026-12-24T00:00:00.000Z");
    assert.equal(P.dayInputValue("2026-12-24T00:00:00Z"), "2026-12-24");
    assert.equal(P.dayToApi("24/12/2026"), null);
});

const wish = (id, updated, price, category) => ({ id, updated_at: updated, price, category });

test("sortWishes sorts by date and price, price-less wishes last", () => {
    const { P } = loadCore();
    const list = [wish("a", "2026-01-02", 20), wish("b", "2026-01-01", null), wish("c", "2026-01-03", 5), wish("d", "2026-01-04", 0)];
    assert.deepEqual(P.sortWishes(list, "date_asc").map((w) => w.id), ["b", "a", "c", "d"]);
    assert.deepEqual(P.sortWishes(list, "date_desc").map((w) => w.id), ["d", "c", "a", "b"]);
    assert.deepEqual(P.sortWishes(list, "price_asc").map((w) => w.id).slice(0, 2), ["c", "a"]);
    assert.deepEqual(P.sortWishes(list, "price_desc").map((w) => w.id).slice(0, 2), ["a", "c"]);
    assert.deepEqual(list.map((w) => w.id), ["a", "b", "c", "d"], "input untouched");
});

test("categories keep their manual order, uncategorized last", () => {
    const { P } = loadCore();
    const books = { id: "k1", name: "Books", sort_order: 2 };
    const home = { id: "k2", name: "Home", sort_order: 1 };
    const buckets = P.bucketWishes([wish("a", "", 1, books), wish("b", "", 1, null), wish("c", "", 1, home), wish("d", "", 1, books)]);
    assert.deepEqual(plain(buckets.categories.map((c) => c.name)), ["Home", "Books"]);
    assert.deepEqual(plain(buckets.categories[1].wishes.map((w) => w.id)), ["a", "d"]);
    assert.deepEqual(plain(buckets.uncategorized.map((w) => w.id)), ["b"]);
});

test("layout preferences use the stored format and only group when there are categories", () => {
    const storage = memoryStorage();
    const { P } = loadCore({ storage });
    let prefs = P.loadSortPrefs();
    assert.deepEqual([prefs.sort, prefs.explicit], ["date_asc", false]);
    const withCategory = [wish("a", "", 1, { id: "k", name: "K" })];
    assert.equal(P.effectiveLayout(withCategory, prefs), "grouped");
    assert.equal(P.effectiveLayout([wish("a", "", 1, null)], prefs), "flat");
    P.saveSortPrefs({ sort: "price_desc", layout: "flat", explicit: true });
    assert.equal(storage.getItem("poenskelistenWishSort"), '{"sort":"price_desc","layout":"flat"}');
    prefs = P.loadSortPrefs();
    assert.equal(P.effectiveLayout(withCategory, prefs), "flat");
    storage.setItem("poenskelistenWishSort", "not json");
    assert.equal(P.loadSortPrefs().sort, "date_asc");
});

test("only preferences are written to localStorage", () => {
    const storage = memoryStorage();
    const { P } = loadCore({ storage });
    P.setTheme("dark");
    P.saveSortPrefs({ sort: "date_desc" });
    P.setTheme("system");
    assert.deepEqual([...storage.data.keys()].sort(), ["poenskelistenWishSort"]);
});

const me = "u-me";
const list = (id, owner, extra = {}) => ({ id, owner: { id: owner }, collaborators: [], expires: false, date: null, ...extra });

test("roles and eligible lists for a new wish", () => {
    const { P } = loadCore();
    const now = new Date("2026-10-31T12:00:00Z");
    const lists = [
        list("own", me),
        list("collab", "u-other", { collaborators: [{ user: { id: me }, enabled: true }] }),
        list("member", "u-other"),
        list("expired", me, { expires: true, date: "2026-10-01T00:00:00Z" }),
        list("future", me, { expires: true, date: "2026-12-24T00:00:00Z" })
    ];
    assert.equal(P.wishlistRole(lists[0], me), "owner");
    assert.equal(P.wishlistRole(lists[1], me), "collaborator");
    assert.equal(P.wishlistRole(lists[2], me), "group_member");
    assert.ok(P.isExpired(lists[3], now));
    assert.deepEqual(P.eligibleWishlists(lists, me, now).map((l) => l.id), ["own", "collab", "future"]);
});

test("wishDestination: pick from home, fixed for one list or from a list", () => {
    const { P } = loadCore();
    const two = [{ id: "a" }, { id: "b" }];
    assert.deepEqual(plain(P.wishDestination("home", two, "a")), { mode: "pick", listID: "", canChange: false });
    assert.deepEqual(plain(P.wishDestination("home", [{ id: "a" }], "")), { mode: "fixed", listID: "a", canChange: false });
    assert.deepEqual(plain(P.wishDestination("list", two, "b")), { mode: "fixed", listID: "b", canChange: true });
    assert.deepEqual(plain(P.wishDestination("list", [{ id: "b" }], "b")), { mode: "fixed", listID: "b", canChange: false });
    assert.deepEqual(plain(P.wishDestination("list", two, "z")), { mode: "pick", listID: "", canChange: false });
    assert.deepEqual(plain(P.wishDestination("home", [], "")), { mode: "none", listID: "", canChange: false });
});

test("claimState tells free, mine and someone else's claims apart", () => {
    const { P } = loadCore();
    assert.equal(P.claimState({ wishclaim: [] }, me).state, "free");
    assert.equal(P.claimState({ wishclaim: [{ user: { id: me }, enabled: true }] }, me).state, "mine");
    const other = P.claimState({ wishclaim: [{ user: { id: "u-2", first_name: "Hidden", last_name: "User" }, enabled: true }] }, me);
    assert.equal(other.state, "other");
    assert.equal(other.claimer.first_name, "Hidden");
    assert.equal(P.claimState({ wishclaim: [{ user: { id: "u-2" }, enabled: false }] }, me).state, "free");
});

test("canManageWish: list owner, collaborators and the wish's author", () => {
    const { P } = loadCore();
    const w = { wishlist_owner: { id: "o" }, owner_id: { id: "a" }, collaborators: [{ user: { id: "c" } }] };
    assert.ok(P.canManageWish(w, "o"));
    assert.ok(P.canManageWish(w, "a"));
    assert.ok(P.canManageWish(w, "c"));
    assert.ok(!P.canManageWish(w, "m"));
    assert.ok(!P.canManageWish(w, ""));
});
