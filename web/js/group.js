// A group (/groups/:id): the lists shared with it and its members. Members
// share their own lists, create a list already shared with the group, and
// leave. The owner edits the group, adds and removes members, and deletes it.
// A list's owner can take it out of the group.
(function () {
    "use strict";
    var h = P.h;

    function groupID() {
        var match = window.location.pathname.match(/^\/groups\/([^\/]+)\/?$/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    function checkedValues(boxes) {
        return boxes.filter(function (box) { return box.input.checked; }).map(function (box) { return box.input.value; });
    }

    // pickDialog lets the user tick items and sends them in one request.
    function pickDialog(title, help, load, toBox, empty, submit) {
        var holder = h("div", { class: "choice-list" }, P.loading());
        var boxes = [];
        var form = h("div", { class: "form-grid" }, h("p", { class: "help", text: help }), holder);
        load().then(function (items) {
            boxes = items.map(toBox);
            P.mount(holder, boxes.length ? boxes.map(function (box) { return box.el; }) : h("p", { class: "help", text: empty }));
        }, function (err) {
            P.mount(holder, h("p", { class: "field-error", text: err.message }));
        });
        P.dialog({
            title: title,
            content: form,
            actions: [
                { label: P.t("action.cancel"), kind: "quiet" },
                { label: P.t("action.add"), kind: "primary", onClick: function () {
                    var values = checkedValues(boxes);
                    if (values.length === 0) {
                        return undefined;
                    }
                    return submit(values).then(function () { return true; }, function (err) {
                        P.formError(form, err.message);
                        return false;
                    });
                } }
            ]
        });
    }

    function editDialog(group, onDone) {
        var name = h("input", { type: "text", name: "name", required: true, value: group.name, attrs: { maxlength: 200, autocomplete: "off" } });
        var description = h("textarea", { name: "description", value: group.description || "" });
        var form = h("div", { class: "form-grid" }, P.field(P.t("group.name"), name, null, true), P.field(P.t("group.description"), description));
        P.dialog({
            title: P.t("group.edit"),
            content: form,
            actions: [
                { label: P.t("action.cancel"), kind: "quiet" },
                { label: P.t("action.save"), kind: "primary", onClick: function () {
                    if (!name.value.trim()) {
                        P.formError(form, P.t("group.nameRequired"));
                        return false;
                    }
                    return P.api("POST", "auth/groups/" + encodeURIComponent(group.id), { name: name.value.trim(), description: description.value.trim() }).then(function () {
                        onDone();
                        return true;
                    }, function (err) {
                        P.formError(form, err.message);
                        return false;
                    });
                } }
            ]
        });
    }

    P.page({
        session: "required",
        title: "nav.groups",
        init: function (main, user) {
            var id = groupID();
            var path = "auth/groups/" + encodeURIComponent(id);
            var showExpired = P.checkbox(P.t("lists.showExpired"), false);
            var listGrid = h("div", { class: "grid three" }, P.loading());
            var memberList = h("div");
            var group = null;
            var lists = [];

            var drawLists = function () {
                var visible = lists.filter(function (wishlist) { return showExpired.input.checked || !P.isExpired(wishlist); });
                P.mount(listGrid, visible.length ? visible.map(function (wishlist) {
                    var card = P.listCard(wishlist, user.id);
                    if (P.wishlistRole(wishlist, user.id) === "owner") {
                        card.appendChild(h("div", { class: "collection-actions" }, P.button(P.t("group.removeList"), function () {
                            P.confirm({ title: P.t("group.removeListTitle"), message: P.t("group.removeListConfirm", { list: wishlist.name, group: group.name }), confirm: P.t("group.removeList"), danger: true }).then(function (yes) {
                                if (yes) {
                                    P.api("POST", "auth/wishlists/" + encodeURIComponent(wishlist.id) + "/remove", { group_id: group.id }).then(function () {
                                        P.toast(P.t("group.listRemoved"));
                                        loadLists();
                                    }, P.error);
                                }
                            });
                        }, "small danger-quiet", "x")));
                    }
                    return card;
                }) : P.emptyState(P.t("group.noLists")));
            };
            var loadLists = function () {
                return P.api("GET", "auth/wishlists?group=" + encodeURIComponent(id)).then(function (result) {
                    lists = result.wishlists || [];
                    drawLists();
                }, function (err) { P.mount(listGrid, P.emptyState(err.message)); });
            };
            showExpired.input.addEventListener("change", drawLists);

            var drawMembers = function () {
                var isOwner = group.owner && group.owner.id === user.id;
                var members = (group.members || []).slice().sort(function (a, b) {
                    return (b.id === group.owner.id) - (a.id === group.owner.id) || P.displayName(a).localeCompare(P.displayName(b));
                });
                P.mount(memberList, members.map(function (member) {
                    var owner = member.id === group.owner.id;
                    return h("div", { class: "member-row" },
                        P.avatar(member),
                        h("span", { class: "grow" }, P.displayName(member), member.id === user.id ? h("small", { text: P.t("group.you") }) : null),
                        owner ? h("span", { class: "badge", text: P.t("group.owner") }) : null,
                        isOwner && !owner ? P.button(P.t("group.removeMember", { name: P.displayName(member) }), function () {
                            P.confirm({ title: P.t("group.removeMemberTitle"), message: P.t("group.removeMemberConfirm", { name: P.displayName(member) }), confirm: P.t("action.remove"), danger: true }).then(function (yes) {
                                if (yes) {
                                    P.api("POST", path + "/remove", { member_id: member.id }).then(function () {
                                        P.toast(P.t("group.memberRemoved"));
                                        load();
                                    }, P.error);
                                }
                            });
                        }, "small icon danger-quiet", "x") : null);
                }));
            };

            var load = function () {
                return P.api("GET", path).then(function (result) {
                    group = result.group;
                    render();
                }, function (err) {
                    P.mount(main, P.backLink("/groups", P.t("nav.groups")), P.emptyState(err.message));
                });
            };

            var render = function () {
                var isOwner = group.owner && group.owner.id === user.id;
                P.setTitle(group.name);
                var actions = [];
                if (isOwner) {
                    actions.push(P.button(P.t("group.edit"), function () { editDialog(group, load); }, "", "edit"));
                    actions.push(P.button(P.t("group.delete"), function () {
                        P.confirm({ title: P.t("group.deleteTitle"), message: P.t("group.deleteConfirm", { name: group.name }), confirm: P.t("group.delete"), danger: true }).then(function (yes) {
                            if (yes) {
                                P.api("DELETE", path).then(function () { window.location.href = "/groups"; }, P.error);
                            }
                        });
                    }, "danger-quiet", "trash"));
                } else {
                    actions.push(P.button(P.t("group.leave"), function () {
                        P.confirm({ title: P.t("group.leaveTitle"), message: P.t("group.leaveConfirm", { name: group.name }), confirm: P.t("group.leave"), danger: true }).then(function (yes) {
                            if (yes) {
                                P.api("POST", path + "/leave").then(function () { window.location.href = "/groups"; }, P.error);
                            }
                        });
                    }, "danger-quiet", "logOut"));
                }

                var shareOwn = P.button(P.t("group.shareList"), function () {
                    pickDialog(P.t("group.shareList"), P.t("group.shareListHelp"), function () {
                        return P.api("GET", "auth/wishlists?owned=true&notAMemberOfGroupID=" + encodeURIComponent(id)).then(function (result) {
                            return (result.wishlists || []).filter(function (wishlist) { return P.wishlistRole(wishlist, user.id) === "owner"; });
                        });
                    }, function (wishlist) {
                        return P.checkbox(wishlist.name, false, P.expiryText(wishlist) || null, { value: wishlist.id });
                    }, P.t("group.noListToShare"), function (ids) {
                        return P.api("POST", path + "/add", { wishlists: ids }).then(function () {
                            P.toast(P.t("group.listsShared"));
                            loadLists();
                        });
                    });
                }, "", "share");

                var addMembers = isOwner ? P.button(P.t("group.addMembers"), function () {
                    pickDialog(P.t("group.addMembers"), P.t("group.addMembersHelp"), function () {
                        return P.api("GET", "auth/users?notAMemberOfGroupID=" + encodeURIComponent(id)).then(function (result) { return result.users || []; });
                    }, function (person) {
                        return P.checkbox(P.displayName(person), false, null, { value: person.id });
                    }, P.t("group.nobodyToAdd"), function (ids) {
                        return P.api("POST", path + "/join", { members: ids }).then(function () {
                            P.toast(P.t("group.membersAdded"));
                            load();
                        });
                    });
                }, "small", "userPlus") : null;

                P.mount(main,
                    P.backLink("/groups", P.t("nav.groups")),
                    P.pageHeader(P.t("group.eyebrow"), group.name, group.description || "", actions),
                    h("section", { "aria-labelledby": "group-lists-title" },
                        h("div", { class: "section-top" },
                            h("h2", { id: "group-lists-title", text: P.t("group.listsTitle") }),
                            h("div", { class: "row" },
                                showExpired.el,
                                shareOwn,
                                h("a", { class: "btn primary", href: "/wishlists/new?group=" + encodeURIComponent(id) }, P.icon("plus"), P.t("group.newList")))),
                        listGrid),
                    h("section", { class: "panel spaced", "aria-labelledby": "group-members-title" },
                        h("div", { class: "row spread" },
                            h("h2", { id: "group-members-title", text: P.tn("group.memberCount", (group.members || []).length) }),
                            addMembers),
                        memberList));
                drawMembers();
                drawLists();
            };

            load().then(function () {
                if (group) {
                    loadLists();
                }
            });
        }
    });
})();
