(function () {
    "use strict";
    var h = P.h;

    function newsSection(user) {
        var grid = h("div", { class: "news-grid" }, P.loading());
        var section = h("section", { "aria-labelledby": "news-title" },
            h("div", { class: "section-top" },
                h("h2", { id: "news-title", text: P.t("news.title") }),
                user.admin ? h("a", { class: "btn quiet", href: "/admin#news" }, P.icon("edit"), P.t("news.manage")) : null),
            grid);
        P.api("GET", "auth/news").then(function (result) {
            // Admins also receive scheduled posts; the home page shows only
            // what everyone sees, the admin page lists the rest.
            var now = Date.now();
            var posts = (result.news || []).filter(function (post) {
                var date = P.parseDate(post.date);
                return !date || date.getTime() <= now;
            }).sort(function (a, b) {
                return Date.parse(b.date) - Date.parse(a.date);
            });
            if (posts.length === 0) {
                P.mount(grid, P.emptyState(P.t("news.empty")));
                return;
            }
            P.mount(grid, posts.map(function (post) {
                return h("article", { class: "news-post" },
                    h("span", { class: "eyebrow", text: P.fmtDay(post.date) }),
                    h("h3", { text: post.title }),
                    h("p", { text: post.body }));
            }));
        }, function (err) {
            P.mount(grid, P.emptyState(err.message));
        });
        return section;
    }

    P.page({
        session: "required",
        title: "nav.home",
        init: function (main, user) {
            var lists = h("div", { class: "grid three" }, P.loading());
            P.mount(main,
                h("section", { class: "hero" },
                    h("div", { class: "intro" },
                        h("span", { class: "eyebrow", text: P.t("home.hello", { name: user.first_name || P.displayName(user) }) }),
                        h("h1", { text: P.t("home.title"), tabIndex: -1 }),
                        h("p", { text: P.t("home.intro") }),
                        h("div", { class: "row" },
                            h("a", { class: "btn primary", href: "/wishlists/new" }, P.icon("plus"), P.t("list.create")),
                            h("a", { class: "btn", href: "/wishes/new?from=home" }, P.icon("gift"), P.t("wish.add")))),
                    h("div", { class: "hero-art", attrs: { "aria-hidden": "true" } },
                        h("img", { src: "/assets/illustrations/lamp.svg", alt: "", width: 640, height: 520 }),
                        h("img", { class: "mini-art", src: "/assets/illustrations/cup.svg", alt: "", width: 640, height: 520 }))),
                h("section", { "aria-labelledby": "recent-title" },
                    h("div", { class: "section-top" },
                        h("h2", { id: "recent-title", text: P.t("home.recent") }),
                        h("a", { class: "btn quiet", href: "/wishlists", text: P.t("home.allLists") })),
                    lists),
                newsSection(user),
                h("div", { class: "callout" },
                    h("div", null, h("h3", { text: P.t("home.groupsTitle") }), h("p", { text: P.t("home.groupsIntro") })),
                    h("a", { class: "btn", href: "/groups" }, P.icon("users"), P.t("nav.groups"))));

            P.api("GET", "auth/wishlists?top=5&expired=false").then(function (result) {
                var wishlists = result.wishlists || [];
                if (wishlists.length === 0) {
                    P.mount(lists, P.emptyState(P.t("home.noLists"), h("a", { class: "btn primary", href: "/wishlists/new" }, P.icon("plus"), P.t("list.create"))));
                    return;
                }
                P.mount(lists, wishlists.map(function (wishlist) { return P.listCard(wishlist, user.id); }));
            }, function (err) {
                P.mount(lists, P.emptyState(err.message));
            });
        }
    });
})();
