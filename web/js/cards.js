(function () {
    "use strict";
    var h = P.h;

    function roleLabel(wishlist, role) {
        if (role === "owner") {
            return P.t("role.owner");
        }
        if (role === "collaborator") {
            return P.t("role.collaborator", { name: P.displayName(wishlist.owner) });
        }
        return P.t("role.member", { name: P.displayName(wishlist.owner) });
    }

    P.expiryText = function (wishlist) {
        if (!wishlist.expires || !wishlist.date) {
            return "";
        }
        var day = P.fmtDay(wishlist.date);
        return P.isExpired(wishlist) ? P.t("list.expiredOn", { date: day }) : P.t("list.until", { date: day });
    };

    P.listCard = function (wishlist, userID) {
        var role = P.wishlistRole(wishlist, userID);
        var wishes = wishlist.wishes || [];
        var pictured = wishes.filter(function (wish) { return wish.image; }).slice(0, 2);
        var cover = h("div", { class: "collection-cover" + (pictured.length < 2 ? " single" : ""), attrs: { "aria-hidden": "true" } });
        if (pictured.length === 0) {
            cover.appendChild(h("div", { class: "cover-empty" }, P.icon("gift")));
        }
        pictured.forEach(function (wish) {
            var slot = h("div", { class: "cover-slot" });
            cover.appendChild(slot);
            P.lazyImage(slot, function () { return P.wishImage(wish.id, true); }, "");
        });

        var expiry = P.expiryText(wishlist);
        return h("article", { class: "collection" + (P.isExpired(wishlist) ? " expired" : "") },
            h("a", { href: "/wishlists/" + encodeURIComponent(wishlist.id) },
                cover,
                h("div", { class: "collection-body" },
                    h("div", { class: "row spread" },
                        h("span", { class: "eyebrow", text: roleLabel(wishlist, role) }),
                        wishlist.public ? h("span", { class: "badge" }, P.icon("globe"), P.t("list.public")) : null),
                    h("h3", { text: wishlist.name }),
                    wishlist.description ? h("p", { class: "clamp", text: wishlist.description }) : null,
                    h("div", { class: "list-meta" },
                        h("span", null, P.icon("gift"), P.tn("list.wishCount", wishes.length)),
                        expiry ? h("span", null, P.icon("calendar"), expiry) : null))));
    };

    function statusBadge(state) {
        if (state === "free") {
            return h("span", { class: "badge free", text: P.t("wish.available") });
        }
        if (state === "mine") {
            return h("span", { class: "badge ok" }, P.icon("check"), P.t("wish.claimedByYou"));
        }
        return h("span", { class: "badge reserved" }, P.icon("lock"), P.t("wish.claimed"));
    }

    function imageBox(wish, ctx) {
        var box;
        if (ctx.mode === "preview") {
            box = h("div", { class: "wish-image" });
            if (ctx.previewImage) {
                box.appendChild(h("img", { src: ctx.previewImage, alt: "" }));
            } else {
                box.appendChild(h("span", { class: "placeholder" }, P.icon("image")));
            }
            return box;
        }
        if (!wish.image) {
            return h("div", { class: "wish-image" }, h("span", { class: "placeholder" }, P.icon("gift")));
        }
        box = h("button", {
            type: "button",
            class: "wish-image",
            "aria-label": P.t("wish.enlarge", { name: wish.name }),
            on: { click: function () {
                P.lightbox(function () { return P.wishImage(wish.id, false); }, wish.name);
            } }
        });
        // The picture goes into its own slot so the status badge stays.
        var slot = h("span", { class: "img-slot" }, h("span", { class: "placeholder" }, P.icon("image")));
        box.appendChild(slot);
        P.lazyImage(slot, function () { return P.wishImage(wish.id, true); }, wish.name);
        return box;
    }

    // ctx.mode: "manage" (owner, collaborators), "member" (group member, may claim),
    // "public" (public link) or "preview" (the wish form).
    P.wishCard = function (wish, ctx) {
        var claim = P.claimState(wish, ctx.userID);
        var claimable = !!wish.wish_claimable;
        var showClaims = ctx.showClaims && claimable && ctx.mode !== "manage" && ctx.mode !== "preview";
        var image = imageBox(wish, ctx);
        if (showClaims) {
            image.appendChild(statusBadge(claim.state));
        }

        var currency = { currency: wish.currency, padding: wish.currency_padding, left: wish.currency_left };
        var price = P.fmtPrice(wish.price, ctx.currency || currency);
        var foot = h("div", { class: "wish-foot" });

        if (ctx.mode === "member" && claimable) {
            if (claim.state === "free") {
                foot.appendChild(P.button(P.t("wish.claim"), function () { ctx.onClaim(wish); }, "primary grow", "gift"));
            } else if (claim.state === "mine") {
                foot.appendChild(P.button(P.t("wish.unclaim"), function () { ctx.onUnclaim(wish); }, "grow"));
            } else {
                foot.appendChild(h("span", { class: "wish-status grow", text: ctx.hideClaimers ? P.t("wish.claimedHidden") : P.t("wish.claimedBy", { name: P.displayName(claim.claimer) }) }));
            }
        }
        if (ctx.mode === "public" && claimable && claim.state === "free" && ctx.signInHref) {
            foot.appendChild(h("a", { class: "btn primary small grow", href: ctx.signInHref, text: P.t("wish.signInToClaim") }));
        }
        if (ctx.mode !== "preview") {
            var link = P.externalLink(wish.url, P.t("wish.openLink"), "btn small");
            if (link) {
                foot.appendChild(link);
            }
        }
        if (ctx.mode === "manage") {
            foot.appendChild(h("a", { class: "btn small", href: "/wishes/" + encodeURIComponent(wish.id) + "/edit" }, P.icon("edit"), P.t("action.edit")));
            foot.appendChild(P.button(P.t("wish.delete", { name: wish.name }), function () { ctx.onDelete(wish); }, "small icon danger-quiet", "trash"));
        }

        var updated = ctx.mode !== "preview" && wish.updated_at ? P.fmtDate(wish.updated_at) : "";
        return h("article", { class: "wish-card" + (showClaims && claim.state === "other" ? " claimed-other" : ""), id: wish.id ? "wish-" + wish.id : null },
            image,
            h("div", { class: "wish-body" },
                h("div", { class: "row spread" },
                    h("span", { class: "eyebrow", text: wish.category ? wish.category.name : "" }),
                    price ? h("span", { class: "price", text: price }) : null),
                h("h3", { text: wish.name }),
                wish.note ? h("p", { class: "note", text: wish.note }) : null,
                updated ? h("p", { class: "wish-meta", text: P.t("wish.updated", { date: updated }) }) : null,
                foot.childNodes.length ? foot : null));
    };

    function matchesQuery(wish, query) {
        if (!query) {
            return true;
        }
        var text = [wish.name, wish.note, wish.category ? wish.category.name : ""].join(" ").toLocaleLowerCase(P.locale());
        return text.indexOf(query) !== -1;
    }

    P.wishBoard = function (ctx) {
        var prefs = P.loadSortPrefs();
        var wishes = [];
        var onlyAvailable = false;
        var collapsed = {};

        var search = h("input", { type: "search", name: "q", attrs: { autocomplete: "off" } });
        var sortLabels = {
            date_desc: P.t("sort.dateDesc"),
            date_asc: P.t("sort.dateAsc"),
            price_asc: P.t("sort.priceAsc"),
            price_desc: P.t("sort.priceDesc")
        };
        var sort = h("select", null, P.SORTS.map(function (value) {
            return h("option", { value: value, text: sortLabels[value], selected: prefs.sort === value });
        }));
        var layout = h("select", null,
            h("option", { value: "grouped", text: P.t("layout.grouped") }),
            h("option", { value: "flat", text: P.t("layout.flat") }));
        var layoutField = h("label", { class: "compact" }, P.t("layout.label"), layout);
        var allChip = h("button", { type: "button", class: "chip", text: P.t("filter.all"), "aria-pressed": "true" });
        var freeChip = h("button", { type: "button", class: "chip", text: P.t("filter.available"), "aria-pressed": "false" });
        var chips = h("div", { class: "row", role: "group", "aria-label": P.t("filter.label") }, allChip, freeChip);
        var count = h("p", { class: "muted small", role: "status" });
        var results = h("div");

        var draw = function () {
            var query = search.value.trim().toLocaleLowerCase(P.locale());
            var visible = wishes.filter(function (wish) {
                return matchesQuery(wish, query) && (!onlyAvailable || P.claimState(wish, ctx.userID).state === "free");
            });
            var effective = P.effectiveLayout(wishes, prefs);
            layout.value = effective;
            layoutField.hidden = !P.hasCategories(wishes);
            chips.hidden = !(ctx.mode === "member" && wishes.some(function (wish) { return wish.wish_claimable; }));
            count.textContent = query || onlyAvailable ? P.tn("board.matching", visible.length) : P.tn("list.wishCount", wishes.length);

            if (wishes.length === 0) {
                P.mount(results, ctx.empty || P.emptyState(P.t("board.empty")));
                return;
            }
            if (visible.length === 0) {
                P.mount(results, P.emptyState(P.t("board.noMatch"), P.button(P.t("board.clear"), function () {
                    search.value = "";
                    onlyAvailable = false;
                    allChip.setAttribute("aria-pressed", "true");
                    freeChip.setAttribute("aria-pressed", "false");
                    draw();
                })));
                return;
            }
            var card = function (wish) { return P.wishCard(wish, ctx); };
            if (effective === "flat") {
                P.mount(results, h("div", { class: "wishgrid" }, P.sortWishes(visible, prefs.sort).map(card)));
                return;
            }
            var buckets = P.bucketWishes(visible);
            var sections = buckets.categories.map(function (category) {
                return { id: category.id, name: category.name, wishes: category.wishes };
            });
            if (buckets.uncategorized.length) {
                sections.push({ id: "none", name: P.t("board.other"), wishes: buckets.uncategorized });
            }
            P.mount(results, sections.map(function (section) {
                var details = h("details", { class: "category-section", open: !collapsed[section.id] },
                    h("summary", null,
                        h("h2", { text: section.name }),
                        h("span", { class: "row" }, h("span", { class: "badge", text: String(section.wishes.length) }), P.icon("chevronDown", "icon-svg chev"))),
                    h("div", { class: "wishgrid" }, P.sortWishes(section.wishes, prefs.sort).map(card)));
                details.addEventListener("toggle", function () { collapsed[section.id] = !details.open; });
                return details;
            }));
        };

        search.addEventListener("input", draw);
        sort.addEventListener("change", function () {
            prefs.sort = sort.value;
            P.saveSortPrefs(prefs);
            draw();
        });
        layout.addEventListener("change", function () {
            prefs.layout = layout.value;
            prefs.explicit = true;
            P.saveSortPrefs(prefs);
            draw();
        });
        allChip.addEventListener("click", function () {
            onlyAvailable = false;
            allChip.setAttribute("aria-pressed", "true");
            freeChip.setAttribute("aria-pressed", "false");
            draw();
        });
        freeChip.addEventListener("click", function () {
            onlyAvailable = true;
            freeChip.setAttribute("aria-pressed", "true");
            allChip.setAttribute("aria-pressed", "false");
            draw();
        });

        var el = h("section", { "aria-label": P.t("board.label") },
            h("div", { class: "filters" },
                h("label", { class: "search-box" }, P.t("board.search"), search),
                chips,
                h("div", { class: "row" },
                    h("label", { class: "compact" }, P.t("sort.label"), sort),
                    layoutField)),
            count,
            results);

        return {
            el: el,
            update: function (list) {
                wishes = list || [];
                draw();
            }
        };
    };
})();
