(function () {
    "use strict";
    var h = P.h;

    P.page({
        session: "required",
        title: "lists.title",
        init: function (main, user) {
            var showExpired = P.checkbox(P.t("lists.showExpired"), false);
            var mine = h("div", { class: "grid three" }, P.loading());
            var shared = h("div", { class: "grid three" });
            var sharedSection = h("section", { "aria-labelledby": "shared-title", hidden: true },
                h("div", { class: "section-top" }, h("h2", { id: "shared-title", text: P.t("lists.shared") })),
                shared);
            var all = [];

            var render = function () {
                var visible = all.filter(function (wishlist) { return showExpired.input.checked || !P.isExpired(wishlist); });
                var own = visible.filter(function (wishlist) { return P.wishlistRole(wishlist, user.id) !== "group_member"; });
                var others = visible.filter(function (wishlist) { return P.wishlistRole(wishlist, user.id) === "group_member"; });
                P.mount(mine, own.length ? own.map(function (wishlist) { return P.listCard(wishlist, user.id); }) :
                    P.emptyState(P.t("lists.empty"), h("a", { class: "btn primary", href: "/wishlists/new" }, P.icon("plus"), P.t("list.create"))));
                P.mount(shared, others.map(function (wishlist) { return P.listCard(wishlist, user.id); }));
                sharedSection.hidden = others.length === 0;
            };
            showExpired.input.addEventListener("change", render);

            P.mount(main,
                P.pageHeader(P.t("lists.eyebrow"), P.t("lists.title"), P.t("lists.intro"),
                    h("a", { class: "btn primary", href: "/wishlists/new" }, P.icon("plus"), P.t("list.create"))),
                h("section", { "aria-labelledby": "mine-title" },
                    h("div", { class: "section-top" }, h("h2", { id: "mine-title", text: P.t("lists.mine") }), showExpired.el),
                    mine),
                sharedSection);

            P.api("GET", "auth/wishlists").then(function (result) {
                all = result.wishlists || [];
                render();
            }, function (err) {
                P.mount(mine, P.emptyState(err.message));
            });
        }
    });
})();
