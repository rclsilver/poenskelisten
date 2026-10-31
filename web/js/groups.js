// Groups page: the groups the user belongs to, and creating one with its
// members and the user's lists in a single form.
(function () {
    "use strict";
    var h = P.h;

    function groupCard(group, userID) {
        var members = group.members || [];
        var href = "/groups/" + encodeURIComponent(group.id);
        return h("article", { class: "group-card" },
            h("div", { class: "row spread" },
                h("span", { class: "group-symbol", attrs: { "aria-hidden": "true" } }, P.icon("users")),
                h("span", { class: "badge", text: P.tn("group.memberCount", members.length) })),
            h("h2", null, h("a", { href: href, text: group.name })),
            group.description ? h("p", { class: "clamp", text: group.description }) : null,
            h("div", { class: "row spread" },
                h("div", { class: "avatars", attrs: { "aria-hidden": "true" } }, members.slice(0, 6).map(function (member) { return P.avatar(member); })),
                h("span", { class: "muted small", text: group.owner && group.owner.id === userID ? P.t("group.youOwn") : P.t("group.ownedBy", { name: P.displayName(group.owner) }) })),
            h("a", { class: "btn", href: href, text: P.t("group.open") }));
    }

    // createDialog asks for everything a new group needs on one form.
    function createDialog(user) {
        var name = h("input", { type: "text", name: "name", required: true, attrs: { maxlength: 200, autocomplete: "off" } });
        var description = h("textarea", { name: "description" });
        var people = h("div", { class: "choice-list" }, P.loading());
        var lists = h("div", { class: "choice-list" }, P.loading());
        var peopleBoxes = [];
        var listBoxes = [];
        var form = h("form", { class: "form-grid" },
            P.field(P.t("group.name"), name, null, true),
            P.field(P.t("group.description"), description),
            h("fieldset", null, h("legend", { text: P.t("group.members") }), h("p", { class: "help", text: P.t("group.membersHelp") }), people),
            h("fieldset", null, h("legend", { text: P.t("group.lists") }), h("p", { class: "help", text: P.t("group.listsHelp") }), lists));

        P.api("GET", "auth/users").then(function (result) {
            var users = (result.users || []).filter(function (person) { return person.id !== user.id; });
            peopleBoxes = users.map(function (person) { return P.checkbox(P.displayName(person), false, null, { value: person.id }); });
            P.mount(people, peopleBoxes.length ? peopleBoxes.map(function (box) { return box.el; }) : h("p", { class: "help", text: P.t("group.nobodyElse") }));
        }, function (err) { P.mount(people, h("p", { class: "field-error", text: err.message })); });
        P.api("GET", "auth/wishlists?owned=true").then(function (result) {
            var own = (result.wishlists || []).filter(function (wishlist) { return P.wishlistRole(wishlist, user.id) === "owner"; });
            listBoxes = own.map(function (wishlist) { return P.checkbox(wishlist.name, false, P.expiryText(wishlist) || null, { value: wishlist.id }); });
            P.mount(lists, listBoxes.length ? listBoxes.map(function (box) { return box.el; }) : h("p", { class: "help", text: P.t("group.noOwnLists") }));
        }, function (err) { P.mount(lists, h("p", { class: "field-error", text: err.message })); });

        var checked = function (boxes) {
            return boxes.filter(function (box) { return box.input.checked; }).map(function (box) { return box.input.value; });
        };
        P.dialog({
            title: P.t("group.create"),
            content: form,
            actions: [
                { label: P.t("action.cancel"), kind: "quiet" },
                { label: P.t("group.create"), kind: "primary", onClick: function () {
                    P.formError(form, "");
                    if (!name.value.trim()) {
                        P.formError(form, P.t("group.nameRequired"));
                        name.focus();
                        return false;
                    }
                    return P.api("POST", "auth/groups", {
                        name: name.value.trim(),
                        description: description.value.trim(),
                        members: checked(peopleBoxes),
                        wishlists: checked(listBoxes)
                    }).then(function (result) {
                        var created = (result.groups || []).filter(function (group) {
                            return group.name === name.value.trim() && group.owner && group.owner.id === user.id;
                        }).sort(function (a, b) { return Date.parse(b.created_at) - Date.parse(a.created_at); })[0];
                        window.location.href = created ? "/groups/" + encodeURIComponent(created.id) : "/groups";
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
        title: "groups.title",
        init: function (main, user) {
            var grid = h("div", { class: "grid two" }, P.loading());
            P.mount(main,
                P.pageHeader(P.t("groups.eyebrow"), P.t("groups.title"), P.t("groups.intro"),
                    P.button(P.t("group.create"), function () { createDialog(user); }, "primary", "plus")),
                grid);
            P.api("GET", "auth/groups").then(function (result) {
                var groups = result.groups || [];
                P.mount(grid, groups.length ? groups.map(function (group) { return groupCard(group, user.id); }) :
                    P.emptyState(P.t("groups.empty"), P.button(P.t("group.create"), function () { createDialog(user); }, "primary", "plus")));
            }, function (err) {
                P.mount(grid, P.emptyState(err.message));
            });
        }
    });
})();
