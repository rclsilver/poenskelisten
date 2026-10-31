// Visitors see that a wish is taken, not by whom. The owner and collaborators
// see no claim, as in the app.
(function () {
    "use strict";
    var h = P.h;

    function listHash() {
        var match = window.location.pathname.match(/^\/wishlists\/public\/([^\/]+)\/?$/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    P.page({
        session: "optional",
        title: "public.title",
        init: function (main, user) {
            P.api("GET", "open/wishlists/public/" + encodeURIComponent(listHash()), null, { auth: false }).then(function (result) {
                var wishlist = result.wishlist;
                var role = user ? P.wishlistRole(wishlist, user.id) : "";
                var insider = role === "owner" || role === "collaborator";
                P.setTitle(wishlist.name);

                var banner = h("div");
                var board = P.wishBoard({
                    mode: "public",
                    userID: user ? user.id : "",
                    showClaims: !insider,
                    signInHref: user ? null : "/login",
                    empty: P.emptyState(P.t("board.empty"))
                });
                var expiry = P.expiryText(wishlist);

                P.mount(main,
                    h("section", { class: "list-header" },
                        h("div", { class: "row spread" },
                            h("span", { class: "eyebrow", text: P.t("public.eyebrow") }),
                            h("span", { class: "badge" }, P.icon("globe"), P.t("list.public"))),
                        h("h1", { text: wishlist.name, tabIndex: -1 }),
                        wishlist.description ? h("p", { class: "muted pre", text: wishlist.description }) : null,
                        h("div", { class: "list-meta" },
                            h("span", null, P.icon("user"), P.t("list.by", { name: P.displayName(wishlist.owner) })),
                            expiry ? h("span", null, P.icon("calendar"), expiry) : null)),
                    banner,
                    board.el);
                board.update(wishlist.wishes || []);

                // Whoever can open the list in the app does their claiming there.
                if (user) {
                    P.api("GET", "auth/wishlists/" + encodeURIComponent(wishlist.id)).then(function () {
                        P.mount(banner, h("div", { class: "notice row spread" },
                            h("span", { text: P.t("public.haveAccess") }),
                            h("a", { class: "btn small primary", href: "/wishlists/" + encodeURIComponent(wishlist.id), text: P.t("public.openInApp") })));
                    }, function () {
                    });
                } else if (wishlist.claimable) {
                    P.mount(banner, h("p", { class: "notice", text: P.t("public.signInHint") }));
                }
            }, function (err) {
                P.mount(main, P.emptyState(err.status === 400 ? P.t("public.notFound") : err.message, h("a", { class: "btn", href: "/", text: P.t("nav.home") })));
            });
        }
    });
})();
