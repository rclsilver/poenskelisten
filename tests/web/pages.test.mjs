// Smoke tests of the pages: each script runs in the fake DOM against a fake
// API, renders, and sends the requests the real API expects. They catch
// runtime errors and wrong requests; layout and styling still need a browser.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { loadScripts, dictionary, root } from "./harness.mjs";

// pageScripts lists the scripts web/html/<page>.html loads after the shared
// ones, so a page missing one of its dependencies fails here too.
function pageScripts(page) {
    const html = fs.readFileSync(path.join(root, "web/html", page + ".html"), "utf8");
    return [...html.matchAll(/src="\/js\/([\w.-]+\.js)"/g)].map((m) => m[1]);
}

const ORIGIN = "https://wishes.example";
const errors = [];
process.on("unhandledRejection", (err) => errors.push(err));

const ME = { id: "me", first_name: "Camille", last_name: "Martin", email: "camille@example.test", admin: false };
const ADMIN = { ...ME, admin: true };
const BOB = { id: "bob", first_name: "Bob", last_name: "Durand" };
const CAROL = { id: "carol", first_name: "Carol", last_name: "Petit" };

function wish(id, extra = {}) {
    return {
        id, name: "Wish " + id, note: "", price: 20, url: "", image: false, wish_claimable: true, wishclaim: [],
        updated_at: "2026-10-01T10:00:00Z", currency: "€", currency_padding: true, currency_left: false,
        owner_id: BOB, wishlist_owner: BOB, collaborators: [], category: null, wishlist_id: "l-bob", ...extra
    };
}

function list(id, owner, extra = {}) {
    return {
        id, name: "List " + id, description: "", owner, collaborators: [], members: [], wishes: [],
        expires: false, date: "2026-01-01T00:00:00Z", claimable: true, hide_claimers: false, public: false,
        public_hash: "hash-" + id, created_at: "2026-01-01T00:00:00Z", ...extra
    };
}

const OWN = list("l-own", ME, { name: "Birthday", public: true });
const COLLAB = list("l-collab", BOB, { name: "Home", collaborators: [{ user: ME, enabled: true }] });
const BOBS = list("l-bob", BOB, { name: "Bob's Christmas", members: [{ id: "g1", name: "Family" }] });
const OLD = list("l-old", ME, { name: "Old", expires: true, date: "2020-01-01T00:00:00Z" });

const settle = async () => {
    for (let i = 0; i < 30; i++) {
        await new Promise((resolve) => setImmediate(resolve));
    }
};

function respond(status, body) {
    return Promise.resolve({
        ok: status >= 200 && status < 300,
        status,
        json: () => Promise.resolve(body),
        text: () => Promise.resolve(JSON.stringify(body))
    });
}

// openPage loads a page's scripts the way its template does and waits for
// it to render. routes maps "METHOD /path?query" (or "METHOD /path") to a
// JSON body, or to a function of the request body returning [status, body].
async function openPage(page, { path = "/", user = ME, routes = {}, localLogin = true, lang = "en" } = {}) {
    const calls = [];
    const url = new URL(path, ORIGIN);
    const fetch = (target, options = {}) => {
        const request = new URL(target, ORIGIN);
        const method = (options.method || "GET").toUpperCase();
        const body = options.body ? JSON.parse(options.body) : null;
        calls.push({ method, path: request.pathname + request.search, body, headers: options.headers || {} });
        if (request.pathname.startsWith("/json/lang-")) {
            return respond(200, dictionary(request.pathname.slice("/json/lang-".length, -".json".length)));
        }
        const handler = routes[`${method} ${request.pathname}${request.search}`] ?? routes[`${method} ${request.pathname}`];
        if (handler === undefined) {
            return respond(404, { error: `no fixture for ${method} ${request.pathname}${request.search}` });
        }
        if (typeof handler === "function") {
            const [status, json] = handler(body);
            return respond(status, json);
        }
        return respond(200, handler);
    };
    const location = { origin: ORIGIN, href: url.href, pathname: url.pathname, search: url.search, hash: url.hash, reload() { location.reloaded = true; } };
    const env = loadScripts(["core.js", ...pageScripts(page)], {
        languages: [lang],
        globals: {
            fetch,
            location,
            LOCAL_LOGIN_ENABLED: localLogin,
            OIDC_PROVIDER_NAME: "Authelia",
            startSession: () => Promise.resolve(user),
            set_cookie() {},
            logout() { location.loggedOut = true; },
            handleOAuthCallback() { location.callback = true; },
            navigator: { languages: [lang], clipboard: { writeText: () => Promise.resolve() } }
        },
        setup(document) {
            document.body.setAttribute("data-nav", "test");
            for (const [tag, id] of [["nav", "nav-links"], ["div", "nav-account"], ["main", "main"], ["div", "toasts"]]) {
                const el = document.createElement(tag);
                el.id = id;
                document.body.appendChild(el);
            }
        }
    });
    await settle();
    const main = env.document.getElementById("main");
    return { ...env, main, calls, location };
}

const text = (el) => el.textContent.replace(/\s+/g, " ");
const byText = (root, tag, label) => root.querySelectorAll(tag).find((el) => text(el).includes(label) || el.getAttribute("aria-label") === label);
const click = (el) => el.dispatch("click", { type: "click", target: el, preventDefault() {} });
const submit = (form) => form.dispatch("submit", { type: "submit", target: form, preventDefault() {} });
function type(el, value) {
    el.value = value;
    el.dispatch("input", { type: "input", target: el });
    el.dispatch("change", { type: "change", target: el });
}
const openDialog = (document) => document.body.querySelectorAll("dialog").filter((d) => d.open).pop();
const requests = (calls, method, prefix) => calls.filter((call) => call.method === method && call.path.startsWith(prefix));

test.afterEach(() => {
    const seen = errors.splice(0);
    assert.deepEqual(seen.map(String), [], "no uncaught error while rendering");
});

test("home greets the user and shows recent lists and published news only", async () => {
    const { main, document } = await openPage("frontpage", {
        routes: {
            "GET /api/auth/wishlists?top=5&expired=false": { wishlists: [OWN, BOBS] },
            "GET /api/auth/news": { news: [
                { id: "n1", title: "Welcome", body: "Hello all", date: "2026-01-01T00:00:00Z" },
                { id: "n2", title: "Future post", body: "Later", date: "2999-01-01T00:00:00Z" }
            ] }
        }
    });
    assert.ok(text(main).includes("Hello, Camille"));
    assert.ok(text(main).includes("Birthday") && text(main).includes("Bob's Christmas"));
    assert.ok(text(main).includes("Welcome"));
    assert.ok(!text(main).includes("Future post"), "scheduled posts stay off the home page");
    assert.ok(byText(main, "a", "Add a wish").getAttribute("href").startsWith("/wishes/new"));
    assert.equal(document.getElementById("nav-links").hidden, false);
});

test("the home page and navigation follow the French dictionary", async () => {
    const { main, document } = await openPage("frontpage", {
        lang: "fr-FR",
        routes: { "GET /api/auth/wishlists?top=5&expired=false": { wishlists: [] }, "GET /api/auth/news": { news: [] } }
    });
    assert.equal(document.documentElement.lang, "fr");
    assert.ok(text(main).includes("Bonjour, Camille"));
    assert.ok(text(document.getElementById("nav-links")).includes("Accueil"));
});

test("lists page separates my lists from shared ones and hides expired ones", async () => {
    const { main } = await openPage("wishlists", { path: "/wishlists", routes: { "GET /api/auth/wishlists": { wishlists: [OWN, COLLAB, BOBS, OLD] } } });
    const [mine, shared] = main.querySelectorAll("section");
    assert.ok(text(mine).includes("Birthday") && text(mine).includes("Home"));
    assert.ok(!text(mine).includes("Old"));
    assert.ok(text(shared).includes("Bob's Christmas"));
    const toggle = main.querySelectorAll("input").find((input) => input.type === "checkbox");
    toggle.checked = true;
    toggle.dispatch("change");
    assert.ok(text(mine).includes("Old"));
});

test("a group member claims a wish after confirming, and sees who claimed others", async () => {
    const wishes = [
        wish("w1"),
        wish("w2", { wishclaim: [{ user: ME, enabled: true }] }),
        wish("w3", { wishclaim: [{ user: CAROL, enabled: true }] })
    ];
    const { main, document, calls } = await openPage("wishlist", {
        path: "/wishlists/l-bob",
        routes: {
            "GET /api/auth/wishlists/l-bob": { wishlist: BOBS, public_url: ORIGIN },
            "GET /api/auth/wishes?wishlist=l-bob": { wishes, owner_id: "bob", collaborators: [] },
            "POST /api/auth/wishes/w1/claim": { message: "Wish claimed." }
        }
    });
    assert.ok(!byText(main, "a", "Add a wish"), "members can't add wishes");
    assert.ok(!byText(main, "button", "Delete the list"));
    assert.ok(text(main).includes("Claimed by Carol Petit"));
    assert.ok(byText(main, "button", "Cancel my claim"));
    click(byText(main, "button", "I'll give it"));
    const dialog = openDialog(document);
    assert.ok(dialog, "a confirmation opens");
    click(byText(dialog, "button", "I'll give it"));
    await settle();
    const claim = requests(calls, "POST", "/api/auth/wishes/w1/claim");
    assert.equal(claim.length, 1);
    assert.deepEqual(claim[0].body, { wishlist_id: "l-bob" });
    assert.ok(claim[0].headers.Authorization === undefined || claim[0].headers.Authorization.startsWith("Bearer "));
});

test("the owner manages wishes and sees no claims", async () => {
    const mine = { ...OWN, members: [{ id: "g1", name: "Family" }] };
    const { main, document, calls } = await openPage("wishlist", {
        path: "/wishlists/l-own",
        routes: {
            "GET /api/auth/wishlists/l-own": { wishlist: mine, public_url: ORIGIN },
            "GET /api/auth/wishes?wishlist=l-own": { wishes: [wish("w9", { wishlist_owner: ME, owner_id: ME })], owner_id: "me", collaborators: [] },
            "DELETE /api/auth/wishes/w9": { message: "Wish deleted." }
        }
    });
    assert.equal(byText(main, "a", "Add a wish").getAttribute("href"), "/wishes/new?wishlist=l-own");
    assert.ok(byText(main, "a", "Edit"));
    assert.ok(byText(main, "button", "Delete the list"));
    assert.ok(!byText(main, "button", "I'll give it"));
    assert.equal(main.querySelectorAll(".badge").filter((b) => b.className.includes("free")).length, 0, "no claim badge");
    click(byText(main, "button", "Share"));
    const share = openDialog(document);
    const link = share.querySelectorAll("input")[0];
    assert.equal(link.value, ORIGIN + "/wishlists/public/hash-l-own");
    share.close();
    click(byText(main, "button", "Delete “Wish w9”"));
    click(byText(openDialog(document), "button", "Delete"));
    await settle();
    assert.equal(requests(calls, "DELETE", "/api/auth/wishes/w9").length, 1);
});

const formRoutes = (lists) => ({
    "GET /api/auth/wishlists": { wishlists: lists },
    "GET /api/auth/currency": { currency: "€", padding: true, left: false },
    "GET /api/auth/wishlists/l-own/categories": { categories: [{ id: "c1", name: "Books", sort_order: 1 }] },
    "GET /api/auth/wishlists/l-collab/categories": { categories: [] },
    "POST /api/auth/wishes": { message: "Wish saved.", wishes: [] }
});

test("a wish from home with several lists needs a list chosen first", async () => {
    const { main, calls, location } = await openPage("wishform", { path: "/wishes/new?from=home", routes: formRoutes([OWN, COLLAB, BOBS, OLD]) });
    const select = main.querySelectorAll("select").find((s) => s.name === "wishlist");
    assert.ok(select, "a list picker is shown");
    assert.equal(select.value, "", "nothing is preselected");
    assert.deepEqual(select.querySelectorAll("option").map((o) => o.value), ["", "l-own", "l-collab"], "only active owned or collaborated lists");
    const form = main.querySelector("form");
    type(main.querySelectorAll("input").find((i) => i.name === "name"), "A lamp");
    submit(form);
    await settle();
    assert.equal(requests(calls, "POST", "/api/auth/wishes").length, 0);
    assert.ok(text(form).includes("Choose the list this wish goes to."));
    select.value = "l-collab";
    select.dispatch("change");
    submit(form);
    await settle();
    const sent = requests(calls, "POST", "/api/auth/wishes?wishlist=l-collab");
    assert.equal(sent.length, 1);
    assert.equal(location.href, "/wishlists/l-collab");
});

test("with a single eligible list the wish goes there without asking", async () => {
    const { main } = await openPage("wishform", { path: "/wishes/new?from=home", routes: formRoutes([OWN, BOBS]) });
    assert.ok(!main.querySelectorAll("select").find((s) => s.name === "wishlist"), "no picker");
    assert.ok(text(main).includes("This wish goes to Birthday"));
    assert.ok(!byText(main, "button", "Change list"));
});

test("from a list the wish goes there, with an optional change of list", async () => {
    const { main, calls } = await openPage("wishform", { path: "/wishes/new?wishlist=l-own", routes: formRoutes([OWN, COLLAB]) });
    assert.ok(text(main).includes("This wish goes to Birthday"));
    const inputs = () => main.querySelectorAll("input");
    type(inputs().find((i) => i.name === "name"), "Slow Days");
    type(inputs().find((i) => i.name === "price"), "34.5");
    type(inputs().find((i) => i.name === "url"), "shop.example/book");
    type(main.querySelectorAll("textarea")[0], "Paperback");
    const category = main.querySelectorAll("select").find((s) => s.name === "category");
    category.value = "__new__";
    category.dispatch("change");
    type(inputs().find((i) => i.name === "category_name"), "Reading");
    const preview = main.querySelector("aside");
    assert.ok(text(preview).includes("Slow Days") && text(preview).includes("34,50 €".replace(",", ".")) || text(preview).includes("34.50"), "the preview follows the form");
    assert.ok(text(preview).includes("Reading"));
    submit(main.querySelector("form"));
    await settle();
    const [sent] = requests(calls, "POST", "/api/auth/wishes?wishlist=l-own");
    assert.deepEqual(sent.body, {
        name: "Slow Days", note: "Paperback", price: 34.5, url: "https://shop.example/book",
        image_data: "", category_id: null, category_name: "Reading"
    });
    click(byText(main, "button", "Change list"));
    assert.equal(main.querySelectorAll("select").find((s) => s.name === "wishlist").value, "l-own");
});

test("a wish link that isn't http(s) is refused before sending", async () => {
    const { main, calls } = await openPage("wishform", { path: "/wishes/new?wishlist=l-own", routes: formRoutes([OWN]) });
    type(main.querySelectorAll("input").find((i) => i.name === "name"), "Trap");
    type(main.querySelectorAll("input").find((i) => i.name === "url"), "javascript:alert(1)");
    submit(main.querySelector("form"));
    await settle();
    assert.equal(requests(calls, "POST", "/api/auth/wishes").length, 0);
    assert.ok(text(main).includes("http or https"));
});

test("editing a wish can remove its picture", async () => {
    const editable = wish("w5", { wishlist_id: "l-own", image: true, wishlist_owner: ME, owner_id: ME, category: { id: "c1", name: "Books" } });
    const { main, calls } = await openPage("wishform", {
        path: "/wishes/w5/edit",
        routes: { ...formRoutes([OWN]), "GET /api/auth/wishes/w5": { wish: editable }, "GET /api/both/wishes/w5/image?thumbnail=true": { image: "data:image/jpeg;base64,/9j/4AAQ" }, "POST /api/auth/wishes/w5": { message: "Wish updated." } }
    });
    assert.ok(text(main).includes("In the list Birthday"));
    assert.equal(main.querySelector("aside").querySelector("img").getAttribute("src"), "data:image/jpeg;base64,/9j/4AAQ");
    click(byText(main, "button", "Remove the picture"));
    submit(main.querySelector("form"));
    await settle();
    const [sent] = requests(calls, "POST", "/api/auth/wishes/w5");
    assert.equal(sent.body.image_delete, true);
    assert.equal(sent.body.category_id, "c1");
});

test("a new list from a group is shared with that group", async () => {
    const { main, calls, location } = await openPage("wishlistform", {
        path: "/wishlists/new?group=g1",
        routes: {
            "GET /api/auth/groups": { groups: [{ id: "g1", name: "Family" }, { id: "g2", name: "Friends" }] },
            "GET /api/auth/users": { users: [ME, BOB] },
            "POST /api/auth/wishlists": { wishlists: [{ ...OWN, id: "l-new", name: "Wedding", created_at: "2026-10-31T00:00:00Z" }] }
        }
    });
    const boxes = main.querySelectorAll("input").filter((i) => i.type === "checkbox");
    assert.equal(boxes.find((b) => b.value === "g1").checked, true);
    assert.equal(boxes.find((b) => b.value === "g2").checked, false);
    type(main.querySelectorAll("input").find((i) => i.name === "name"), "Wedding");
    submit(main.querySelector("form"));
    await settle();
    const [sent] = requests(calls, "POST", "/api/auth/wishlists");
    assert.deepEqual(sent.body.groups, ["g1"]);
    assert.equal(sent.body.expires, false);
    assert.equal(location.href, "/wishlists/l-new");
});

test("editing a list sends group and collaborator changes after the settings", async () => {
    const editable = { ...OWN, members: [{ id: "g1", name: "Family" }], collaborators: [{ user: BOB, enabled: true }] };
    const { main, calls } = await openPage("wishlistform", {
        path: "/wishlists/l-own/edit",
        routes: {
            "GET /api/auth/wishlists/l-own": { wishlist: editable },
            "GET /api/auth/groups": { groups: [{ id: "g1", name: "Family" }, { id: "g2", name: "Friends" }] },
            "GET /api/auth/users?notACollaboratorOfWishlistID=l-own": { users: [ME, CAROL] },
            "POST /api/auth/wishlists/l-own": { message: "Wishlist updated." },
            "POST /api/auth/wishlists/l-own/join": { message: "ok" },
            "POST /api/auth/wishlists/l-own/remove": { message: "ok" },
            "POST /api/auth/wishlists/l-own/collaborate": { message: "ok" },
            "POST /api/auth/wishlists/l-own/un-collaborate": { message: "ok" }
        }
    });
    const boxes = main.querySelectorAll("input").filter((i) => i.type === "checkbox");
    boxes.find((b) => b.value === "g1").checked = false;
    boxes.find((b) => b.value === "g2").checked = true;
    click(byText(main, "button", "Remove Bob Durand"));
    const pick = main.querySelectorAll("select").find((s) => s.getAttribute("aria-label") === "Add a collaborator");
    pick.value = "carol";
    click(byText(main, "button", "Add"));
    submit(main.querySelector("form"));
    await settle();
    const order = calls.filter((c) => c.method === "POST").map((c) => c.path);
    assert.deepEqual(order, ["/api/auth/wishlists/l-own", "/api/auth/wishlists/l-own/join", "/api/auth/wishlists/l-own/remove", "/api/auth/wishlists/l-own/collaborate", "/api/auth/wishlists/l-own/un-collaborate"]);
    assert.deepEqual(requests(calls, "POST", "/api/auth/wishlists/l-own/join")[0].body, { groups: ["g2"] });
    assert.deepEqual(requests(calls, "POST", "/api/auth/wishlists/l-own/remove")[0].body, { group_id: "g1" });
    assert.deepEqual(requests(calls, "POST", "/api/auth/wishlists/l-own/collaborate")[0].body, { users: ["carol"] });
    assert.deepEqual(requests(calls, "POST", "/api/auth/wishlists/l-own/un-collaborate")[0].body, { user_id: "bob" });
});

test("a public list shows claims without names and asks visitors to sign in", async () => {
    const publicList = { ...BOBS, public: true, wishes: [wish("w1"), wish("w2", { wishclaim: [{ user: CAROL, enabled: true }] })] };
    const { main } = await openPage("public", {
        path: "/wishlists/public/hash-l-bob",
        user: null,
        routes: { "GET /api/open/wishlists/public/hash-l-bob": { wishlist: publicList } }
    });
    assert.ok(text(main).includes("Bob's Christmas"));
    assert.ok(text(main).includes("Claimed"));
    assert.ok(!text(main).includes("Carol"), "claimer names stay hidden");
    assert.ok(byText(main, "a", "Sign in to give it"));
    assert.ok(!byText(main, "button", "I'll give it"));
});

test("a group owner manages members, a member can leave", async () => {
    const group = { id: "g1", name: "Family", description: "Us", owner: ME, members: [ME, BOB] };
    const routes = {
        "GET /api/auth/groups/g1": { group },
        "GET /api/auth/wishlists?group=g1": { wishlists: [OWN, BOBS] }
    };
    const owner = await openPage("group", { path: "/groups/g1", routes });
    assert.ok(byText(owner.main, "button", "Delete the group"));
    assert.ok(byText(owner.main, "button", "Remove Bob Durand"));
    assert.ok(byText(owner.main, "button", "Add members"));
    assert.equal(owner.main.querySelectorAll("button").filter((b) => text(b).includes("Remove from the group")).length, 1, "only my list can be taken out");
    assert.equal(byText(owner.main, "a", "New list for this group").getAttribute("href"), "/wishlists/new?group=g1");

    const member = await openPage("group", { path: "/groups/g1", user: { ...BOB }, routes });
    assert.ok(byText(member.main, "button", "Leave the group"));
    assert.ok(!byText(member.main, "button", "Delete the group"));
    assert.ok(!byText(member.main, "button", "Remove Camille Martin"));
});

test("creating a group sends members and lists in one request", async () => {
    const { main, document, calls } = await openPage("groups", {
        path: "/groups",
        routes: {
            "GET /api/auth/groups": { groups: [] },
            "GET /api/auth/users": { users: [ME, BOB] },
            "GET /api/auth/wishlists?owned=true": { wishlists: [OWN, COLLAB] },
            "POST /api/auth/groups": { groups: [{ id: "g9", name: "Friends", owner: ME, created_at: "2026-10-31T00:00:00Z" }] }
        }
    });
    click(byText(main, "button", "Create a group"));
    await settle();
    const dialog = openDialog(document);
    type(dialog.querySelectorAll("input").find((i) => i.name === "name"), "Friends");
    const boxes = dialog.querySelectorAll("input").filter((i) => i.type === "checkbox");
    assert.deepEqual(boxes.map((b) => b.value), ["bob", "l-own"], "other people, and lists I own");
    boxes.forEach((b) => { b.checked = true; });
    click(byText(dialog, "button", "Create a group"));
    await settle();
    assert.deepEqual(requests(calls, "POST", "/api/auth/groups")[0].body, { name: "Friends", description: "", members: ["bob"], wishlists: ["l-own"] });
});

test("account page: local accounts change password with the current one, OIDC ones can't", async () => {
    const local = await openPage("account", {
        path: "/account",
        routes: { "GET /api/auth/users/me": { user: { ...ME, created_at: "2025-01-01T00:00:00Z", mfa_enabled: false, auth_source: "local" } }, "GET /api/auth/connected-apps": { apps: [] } }
    });
    const names = local.main.querySelectorAll("input").map((i) => i.name);
    assert.ok(["password", "password_repeat", "password_original"].every((n) => names.includes(n)));
    assert.ok(byText(local.main, "button", "Turn on"));
    assert.ok(text(local.main).includes("can't be changed"));

    const oidc = await openPage("account", {
        path: "/account",
        routes: { "GET /api/auth/users/me": { user: { ...ME, created_at: "2025-01-01T00:00:00Z", auth_source: "oidc" } }, "GET /api/auth/connected-apps": { apps: [] } }
    });
    const oidcNames = oidc.main.querySelectorAll("input").map((i) => i.name);
    assert.ok(!oidcNames.includes("password") && !oidcNames.includes("password_original"));
    assert.ok(text(oidc.main).includes("Managed by Authelia"));
});

test("admin page shows the server's application name and scheduled news", async () => {
    const { main } = await openPage("admin", {
        path: "/admin",
        user: ADMIN,
        routes: {
            "POST /api/admin/server/info": { server: { app_name: "Nos envies", database_type: "sqlite", mfa_enforced: true } },
            "GET /api/auth/news": { news: [{ id: "n2", title: "Future post", body: "Later", date: "2999-01-01T00:00:00Z" }] },
            "GET /api/auth/users": { users: [ADMIN, BOB] },
            "GET /api/admin/invites": { invites: [{ id: "i1", invite_code: "ABC123", invite_used: false }] },
            "GET /api/admin/oauth/clients": { clients: [] },
            "GET /api/auth/currency": { currency: "€", padding: true, left: false }
        }
    });
    assert.ok(text(main).includes("Nos envies"));
    assert.ok(text(main).includes("Future post") && text(main).includes("Scheduled"));
    assert.ok(text(main).includes("ABC123"));
    const enforced = main.querySelectorAll("input").filter((i) => i.type === "checkbox").find((i) => i.parentNode && text(i.parentNode).includes("Require two-factor"));
    assert.equal(enforced.checked, true);

    const denied = await openPage("admin", { path: "/admin", user: ME });
    assert.ok(text(denied.main).includes("for administrators"));
});

test("login sends the credentials and only follows same-site destinations", async () => {
    const { main, calls, location } = await openPage("login", {
        path: "/login?next=//evil.example/steal",
        routes: { "POST /api/open/tokens/register": { message: "Logged in!" }, "GET /api/open/oidc/config": { enabled: false } }
    });
    type(main.querySelectorAll("input").find((i) => i.name === "email"), "camille@example.test");
    type(main.querySelectorAll("input").find((i) => i.name === "password"), "secret");
    submit(main.querySelector("form"));
    await settle();
    assert.deepEqual(requests(calls, "POST", "/api/open/tokens/register")[0].body, { email: "camille@example.test", password: "secret" });
    assert.equal(location.href, "/");
});

test("login moves to the MFA page with the challenge in session storage", async () => {
    const { main, location, sandbox } = await openPage("login", {
        path: "/login?next=/oauth/authorize?client_id=x",
        routes: { "POST /api/open/tokens/register": { mfa_required: true, mfa_token: "challenge" }, "GET /api/open/oidc/config": { enabled: true, provider_name: "Authelia", login_url: "/api/open/oidc/login" } }
    });
    assert.equal(byText(main, "a", "Sign in with Authelia").getAttribute("href"), "/api/open/oidc/login");
    type(main.querySelectorAll("input").find((i) => i.name === "email"), "camille@example.test");
    type(main.querySelectorAll("input").find((i) => i.name === "password"), "secret");
    submit(main.querySelector("form"));
    await settle();
    assert.equal(sandbox.sessionStorage.getItem("mfa_token"), "challenge");
    assert.equal(location.href, ORIGIN + "/login/mfa?next=%2Foauth%2Fauthorize%3Fclient_id%3Dx");
});

test("the login error message is shown as text", async () => {
    const { main } = await openPage("login", { path: "/login?error=<img src=x onerror=alert(1)>", routes: { "GET /api/open/oidc/config": { enabled: false } } });
    assert.equal(main.querySelectorAll("img").filter((img) => img.getAttribute("src") === "x").length, 0);
    assert.ok(text(main).includes("<img src=x onerror=alert(1)>"));
});

test("an OIDC-only server shows a single sign-on button and no password form", async () => {
    const { main } = await openPage("login", { path: "/login", localLogin: false });
    assert.equal(main.querySelectorAll("input").filter((i) => i.type === "password").length, 0);
    assert.equal(byText(main, "a", "Sign in with Authelia").getAttribute("href"), "/api/open/oidc/login");
});

test("registration checks the repeated password before sending", async () => {
    const { main, calls } = await openPage("register", { path: "/register?invite=XYZ" });
    const input = (name) => main.querySelectorAll("input").find((i) => i.name === name);
    assert.equal(input("invite_code").value, "XYZ");
    assert.equal(input("consent").required, true);
    type(input("password"), "one");
    type(input("password_repeat"), "two");
    submit(main.querySelector("form"));
    await settle();
    assert.equal(requests(calls, "POST", "/api/open/users").length, 0);
    assert.ok(text(main).includes("don't match"));
});

test("forced enrolment shows the QR code and sends the confirmation code", async () => {
    const { main, calls } = await openPage("enroll", {
        path: "/enroll",
        routes: {
            "POST /api/open/users/mfa/enroll": { secret: "ABCDEF", otpauth_url: "otpauth://totp/x?secret=ABCDEF", qr_code: "data:image/png;base64,iVBORw0KGgo=" },
            "POST /api/open/users/mfa/activate": { recovery_codes: ["c1", "c2"] }
        }
    });
    assert.equal(main.querySelector("img.qr").getAttribute("src"), "data:image/png;base64,iVBORw0KGgo=");
    type(main.querySelectorAll("input").find((i) => i.name === "code"), "123 456");
    submit(main.querySelectorAll("form").pop());
    await settle();
    assert.deepEqual(requests(calls, "POST", "/api/open/users/mfa/activate")[0].body, { code: "123456" });
    assert.ok(text(main).includes("c1") && text(main).includes("c2"));
});

test("the offline page and the callback page render", async () => {
    const offline = await openPage("offline", { path: "/offline" });
    assert.ok(text(offline.main).includes("You're offline."));
    const callback = await openPage("callback", { path: "/oauth/callback?code=x&state=y" });
    assert.equal(callback.location.callback, true);
});
