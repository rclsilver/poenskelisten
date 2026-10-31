// A wishlist (/wishlists/:id): its details, the wishes as picture cards, and
// what the viewer may do. Owners and collaborators add, edit and delete
// wishes; owners also delete the list. Group members claim and unclaim; the
// API never tells owners and collaborators who claimed what.
(function () {
    "use strict";
    var h = P.h;

    function listID() {
        var match = window.location.pathname.match(/^\/wishlists\/([^\/]+)\/?$/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    function shareDialog(wishlist, publicURL, role) {
        var content = [];
        if (wishlist.public) {
            var url = P.publicWishlistUrl(publicURL, wishlist.public_hash);
            var input = h("input", { type: "text", readOnly: true, value: url, "aria-label": P.t("share.publicLink") });
            content.push(h("div", { class: "stack tight" },
                h("p", { text: P.t("share.publicHelp") }),
                h("div", { class: "share-url" }, input, P.button(P.t("share.copy"), function () { P.copyText(url, input); }, "", "copy")),
                h("a", { class: "btn small", href: url, target: "_blank", rel: "noopener" }, P.icon("external"), P.t("share.open"))));
        } else {
            content.push(h("p", { class: "notice", text: P.t("share.notPublic") }));
        }
        var groups = wishlist.members || [];
        content.push(h("div", { class: "stack tight" },
            h("h3", { text: P.t("share.groups") }),
            groups.length ? h("div", { class: "row" }, groups.map(function (group) {
                return h("a", { class: "chip", href: "/groups/" + encodeURIComponent(group.id) }, P.icon("users"), group.name);
            })) : h("p", { class: "help", text: P.t("share.noGroups") })));
        var collaborators = (wishlist.collaborators || []).filter(function (c) { return c.enabled !== false; });
        content.push(h("div", { class: "stack tight" },
            h("h3", { text: P.t("listForm.collaborators") }),
            collaborators.length ? h("div", null, collaborators.map(function (c) {
                return h("div", { class: "member-row" }, P.avatar(c.user), h("span", { class: "grow", text: P.displayName(c.user) }));
            })) : h("p", { class: "help", text: P.t("listForm.noCollaborators") })));
        if (role === "owner") {
            content.push(h("a", { class: "btn", href: "/wishlists/" + encodeURIComponent(wishlist.id) + "/edit#sharing-title" }, P.icon("settings"), P.t("share.manage")));
        }
        P.dialog({ title: P.t("share.title"), content: h("div", { class: "stack" }, content) });
    }

    function header(wishlist, publicURL, role) {
        var manage = role === "owner" || role === "collaborator";
        var id = encodeURIComponent(wishlist.id);
        var expiry = P.expiryText(wishlist);
        var actions = h("div", { class: "row" });
        if (manage && !P.isExpired(wishlist)) {
            actions.appendChild(h("a", { class: "btn primary", href: "/wishes/new?wishlist=" + id }, P.icon("plus"), P.t("wish.add")));
        }
        if (manage) {
            actions.appendChild(h("a", { class: "btn", href: "/wishlists/" + id + "/edit" }, P.icon("edit"), P.t("action.edit")));
        }
        actions.appendChild(P.button(P.t("share.title"), function () { shareDialog(wishlist, publicURL, role); }, "", "share"));
        if (role === "owner") {
            actions.appendChild(P.button(P.t("list.delete"), function () {
                P.confirm({ title: P.t("list.deleteTitle"), message: P.t("list.deleteConfirm", { name: wishlist.name }), confirm: P.t("list.delete"), danger: true }).then(function (yes) {
                    if (!yes) {
                        return;
                    }
                    P.api("DELETE", "auth/wishlists/" + id).then(function () {
                        window.location.href = "/wishlists";
                    }, P.error);
                });
            }, "danger-quiet", "trash"));
        }

        var roleText = role === "owner" ? P.t("role.owner") : role === "collaborator" ? P.t("role.collaborator", { name: P.displayName(wishlist.owner) }) : P.t("role.member", { name: P.displayName(wishlist.owner) });
        return h("section", { class: "list-header" },
            h("div", { class: "row spread" },
                h("span", { class: "eyebrow", text: roleText }),
                h("div", { class: "row" },
                    wishlist.public ? h("span", { class: "badge" }, P.icon("globe"), P.t("list.public")) : null,
                    (wishlist.members || []).map(function (group) {
                        return h("a", { class: "badge", href: "/groups/" + encodeURIComponent(group.id) }, P.icon("users"), group.name);
                    }))),
            h("h1", { text: wishlist.name, tabIndex: -1 }),
            wishlist.description ? h("p", { class: "muted pre", text: wishlist.description }) : null,
            h("div", { class: "list-meta" },
                h("span", null, P.avatar(wishlist.owner), P.t("list.by", { name: P.displayName(wishlist.owner) })),
                expiry ? h("span", null, P.icon("calendar"), expiry) : null,
                h("span", null, P.icon(wishlist.claimable ? "gift" : "lock"), wishlist.claimable ? (wishlist.hide_claimers ? P.t("list.claimableHidden") : P.t("list.claimable")) : P.t("list.notClaimable"))),
            actions);
    }

    P.page({
        session: "required",
        title: "nav.lists",
        init: function (main, user) {
            var id = listID();
            P.api("GET", "auth/wishlists/" + encodeURIComponent(id)).then(function (result) {
                var wishlist = result.wishlist;
                var role = P.wishlistRole(wishlist, user.id);
                var manage = role === "owner" || role === "collaborator";
                P.setTitle(wishlist.name);

                var reload;
                var claim = function (wish) {
                    P.confirm({ title: P.t("wish.claimTitle"), message: P.t("wish.claimConfirm", { name: wish.name }), confirm: P.t("wish.claim") }).then(function (yes) {
                        if (yes) {
                            P.api("POST", "auth/wishes/" + encodeURIComponent(wish.id) + "/claim", { wishlist_id: wishlist.id }).then(function () {
                                P.toast(P.t("wish.claimToast"));
                                reload();
                            }, P.error);
                        }
                    });
                };
                var unclaim = function (wish) {
                    P.api("POST", "auth/wishes/" + encodeURIComponent(wish.id) + "/unclaim", { wishlist_id: wishlist.id }).then(function () {
                        P.toast(P.t("wish.unclaimToast"));
                        reload();
                    }, P.error);
                };
                var remove = function (wish) {
                    P.confirm({ title: P.t("wish.deleteTitle"), message: P.t("wish.deleteConfirm", { name: wish.name }), confirm: P.t("action.delete"), danger: true }).then(function (yes) {
                        if (yes) {
                            P.api("DELETE", "auth/wishes/" + encodeURIComponent(wish.id)).then(function () {
                                P.forgetWishImage(wish.id);
                                P.toast(P.t("wish.deleted"));
                                reload();
                            }, P.error);
                        }
                    });
                };

                var board = P.wishBoard({
                    mode: manage ? "manage" : "member",
                    userID: user.id,
                    showClaims: !manage,
                    hideClaimers: !!wishlist.hide_claimers,
                    onClaim: claim,
                    onUnclaim: unclaim,
                    onDelete: remove,
                    empty: manage && !P.isExpired(wishlist) ?
                        P.emptyState(P.t("board.emptyOwner"), h("a", { class: "btn primary", href: "/wishes/new?wishlist=" + encodeURIComponent(wishlist.id) }, P.icon("plus"), P.t("wish.add"))) :
                        P.emptyState(P.t("board.empty"))
                });

                reload = function () {
                    return P.api("GET", "auth/wishes?wishlist=" + encodeURIComponent(wishlist.id)).then(function (wishes) {
                        board.update(wishes.wishes || []);
                    }, P.error);
                };

                P.mount(main,
                    P.backLink("/wishlists", P.t("nav.lists")),
                    header(wishlist, result.public_url, role),
                    P.isExpired(wishlist) ? h("p", { class: "notice warn", text: P.t("list.expiredNotice") }) : null,
                    board.el);
                reload().then(function () {
                    var target = window.location.hash ? document.getElementById(window.location.hash.slice(1)) : null;
                    if (target) {
                        target.scrollIntoView({ block: "center" });
                    }
                });
            }, function (err) {
                P.mount(main, P.backLink("/wishlists", P.t("nav.lists")), P.emptyState(err.message));
            });
        }
    });
})();
