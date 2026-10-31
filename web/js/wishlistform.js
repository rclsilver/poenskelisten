// The wishlist form, on one page, for /wishlists/new (?group= preselects a
// group) and /wishlists/:id/edit. Owners also manage sharing here: the
// groups the list is shared with and its collaborators. Changes to those are
// collected and sent when the form is saved.
(function () {
    "use strict";
    var h = P.h;

    function editID() {
        var match = window.location.pathname.match(/^\/wishlists\/([^\/]+)\/edit$/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    function tomorrow() {
        var date = new Date();
        date.setDate(date.getDate() + 1);
        return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, "0"), String(date.getDate()).padStart(2, "0")].join("-");
    }

    // sequence runs promise-returning steps one after the other.
    function sequence(steps) {
        return steps.reduce(function (chain, step) { return chain.then(step); }, Promise.resolve());
    }

    function render(main, user, wishlist, groups, candidates) {
        var editing = !!wishlist;
        var role = editing ? P.wishlistRole(wishlist, user.id) : "owner";
        var isOwner = role === "owner";
        var preselectedGroup = new URL(window.location.href).searchParams.get("group") || "";

        var name = h("input", { type: "text", name: "name", required: true, value: editing ? wishlist.name : "", attrs: { maxlength: 200, autocomplete: "off" } });
        var description = h("textarea", { name: "description", value: editing ? wishlist.description || "" : "" });

        var expires = P.checkbox(P.t("listForm.expires"), editing ? !!wishlist.expires : false, P.t("listForm.expiresHelp"));
        var date = h("input", { type: "date", name: "date", value: editing && wishlist.expires ? P.dayInputValue(wishlist.date) : "", attrs: { min: editing ? null : tomorrow() } });
        var dateField = P.field(P.t("listForm.date"), date, null, true);
        var claimable = P.checkbox(P.t("listForm.claimable"), editing ? !!wishlist.claimable : true, P.t("listForm.claimableHelp"));
        var hideClaimers = P.checkbox(P.t("listForm.hideClaimers"), editing ? !!wishlist.hide_claimers : false, P.t("listForm.hideClaimersHelp"));
        var isPublic = P.checkbox(P.t("listForm.public"), editing ? !!wishlist.public : false, P.t("listForm.publicHelp"));

        var syncOptions = function () {
            dateField.hidden = !expires.input.checked;
            date.required = expires.input.checked;
            hideClaimers.input.disabled = !claimable.input.checked;
            if (!claimable.input.checked) {
                hideClaimers.input.checked = false;
            }
        };
        expires.input.addEventListener("change", syncOptions);
        claimable.input.addEventListener("change", syncOptions);
        syncOptions();

        // Groups: the user's groups, plus any group the list is already
        // shared with (the owner may have left it since).
        var linked = editing ? (wishlist.members || []).map(function (group) { return group.id; }) : (preselectedGroup ? [preselectedGroup] : []);
        var groupOptions = groups.slice();
        (editing ? wishlist.members || [] : []).forEach(function (group) {
            if (!groupOptions.some(function (g) { return g.id === group.id; })) {
                groupOptions.push(group);
            }
        });
        var groupBoxes = groupOptions.map(function (group) {
            var box = P.checkbox(group.name, linked.indexOf(group.id) !== -1, group.description || null, { value: group.id });
            return box;
        });

        // Collaborators: current ones can be removed, others added.
        var collaborators = editing ? (wishlist.collaborators || []).filter(function (c) { return c.enabled !== false; }).map(function (c) { return c.user; }) : [];
        var pending = collaborators.slice();
        // The API leaves current collaborators out of the candidates; keep them
        // so one removed by mistake can be added back before saving.
        collaborators.forEach(function (person) {
            if (!candidates.some(function (c) { return c.id === person.id; })) {
                candidates.push(person);
            }
        });
        var collabList = h("div");
        var collabSelect = h("select", { "aria-label": P.t("listForm.addCollaborator") });
        var renderCollaborators = function () {
            P.mount(collabList, pending.length ? pending.map(function (person) {
                return h("div", { class: "member-row" },
                    P.avatar(person),
                    h("span", { class: "grow", text: P.displayName(person) }),
                    P.button(P.t("listForm.removeCollaborator", { name: P.displayName(person) }), function () {
                        pending = pending.filter(function (p) { return p.id !== person.id; });
                        renderCollaborators();
                    }, "small icon danger-quiet", "x"));
            }) : h("p", { class: "help", text: P.t("listForm.noCollaborators") }));
            var available = candidates.filter(function (person) {
                return person.id !== user.id && !pending.some(function (p) { return p.id === person.id; });
            });
            P.mount(collabSelect, h("option", { value: "", text: P.t("listForm.chooseCollaborator") }),
                available.map(function (person) { return h("option", { value: person.id, text: P.displayName(person) }); }));
            collabSelect.disabled = available.length === 0;
        };
        renderCollaborators();
        var addCollaborator = P.button(P.t("action.add"), function () {
            var person = candidates.filter(function (p) { return p.id === collabSelect.value; })[0];
            if (person) {
                pending.push(person);
                renderCollaborators();
            }
        }, "", "userPlus");

        var save = h("button", { type: "submit", class: "btn primary", text: editing ? P.t("action.save") : P.t("list.create") });
        var cancelHref = editing ? "/wishlists/" + encodeURIComponent(wishlist.id) : (preselectedGroup ? "/groups/" + encodeURIComponent(preselectedGroup) : "/wishlists");

        var form = h("form", { class: "stack loose", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            var body = {
                name: name.value.trim(),
                description: description.value.trim(),
                expires: expires.input.checked,
                date: expires.input.checked ? P.dayToApi(date.value) : null,
                claimable: claimable.input.checked,
                hide_claimers: claimable.input.checked && hideClaimers.input.checked,
                public: isPublic.input.checked
            };
            if (body.expires && !body.date) {
                P.formError(form, P.t("listForm.dateRequired"));
                return;
            }
            var chosenGroups = groupBoxes.filter(function (box) { return box.input.checked; }).map(function (box) { return box.input.value; });
            save.disabled = true;
            var done = function (id) {
                window.location.href = "/wishlists/" + encodeURIComponent(id);
            };
            var fail = function (err) {
                save.disabled = false;
                P.formError(form, err.message);
            };
            var collaboratorSteps = function (id, before) {
                var added = pending.filter(function (p) { return !before.some(function (b) { return b.id === p.id; }); });
                var removed = before.filter(function (b) { return !pending.some(function (p) { return p.id === b.id; }); });
                var steps = [];
                if (added.length) {
                    steps.push(function () { return P.api("POST", "auth/wishlists/" + encodeURIComponent(id) + "/collaborate", { users: added.map(function (p) { return p.id; }) }); });
                }
                removed.forEach(function (person) {
                    steps.push(function () { return P.api("POST", "auth/wishlists/" + encodeURIComponent(id) + "/un-collaborate", { user_id: person.id }); });
                });
                return steps;
            };

            if (!editing) {
                body.groups = chosenGroups;
                P.api("POST", "auth/wishlists", body).then(function (result) {
                    // The API answers with all of the user's lists: the new one
                    // is their most recent with this name.
                    var created = (result.wishlists || []).filter(function (w) {
                        return w.name === body.name && w.owner && w.owner.id === user.id;
                    }).sort(function (a, b) { return Date.parse(b.created_at) - Date.parse(a.created_at); })[0];
                    if (!created) {
                        window.location.href = "/wishlists";
                        return;
                    }
                    return sequence(collaboratorSteps(created.id, [])).then(function () { done(created.id); });
                }).catch(fail);
                return;
            }

            var steps = [function () { return P.api("POST", "auth/wishlists/" + encodeURIComponent(wishlist.id), body); }];
            if (isOwner) {
                var toAdd = chosenGroups.filter(function (id) { return linked.indexOf(id) === -1; });
                var toRemove = linked.filter(function (id) { return chosenGroups.indexOf(id) === -1; });
                if (toAdd.length) {
                    steps.push(function () { return P.api("POST", "auth/wishlists/" + encodeURIComponent(wishlist.id) + "/join", { groups: toAdd }); });
                }
                toRemove.forEach(function (groupID) {
                    steps.push(function () { return P.api("POST", "auth/wishlists/" + encodeURIComponent(wishlist.id) + "/remove", { group_id: groupID }); });
                });
                steps = steps.concat(collaboratorSteps(wishlist.id, collaborators));
            }
            sequence(steps).then(function () { done(wishlist.id); }, fail);
        } } },
            h("div", { class: "settings-grid" },
                h("section", { class: "panel form-grid", "aria-labelledby": "essentials-title" },
                    h("h2", { id: "essentials-title", text: P.t("listForm.essentials") }),
                    P.field(P.t("listForm.name"), name, null, true),
                    P.field(P.t("listForm.description"), description, P.t("listForm.descriptionHelp"))),
                h("section", { class: "panel form-grid", "aria-labelledby": "options-title" },
                    h("h2", { id: "options-title", text: P.t("listForm.options") }),
                    h("div", null, expires.el, dateField),
                    claimable.el,
                    hideClaimers.el,
                    isPublic.el),
                isOwner ? h("section", { class: "panel form-grid full", "aria-labelledby": "sharing-title" },
                    h("h2", { id: "sharing-title", text: P.t("listForm.sharing") }),
                    h("fieldset", null,
                        h("legend", { text: P.t("listForm.groups") }),
                        h("p", { class: "help", text: P.t("listForm.groupsHelp") }),
                        groupBoxes.length ? h("div", { class: "choice-list" }, groupBoxes.map(function (box) { return box.el; })) :
                            h("p", { class: "help" }, P.t("listForm.noGroups"), " ", h("a", { href: "/groups", text: P.t("listForm.createGroup") }))),
                    h("fieldset", null,
                        h("legend", { text: P.t("listForm.collaborators") }),
                        h("p", { class: "help", text: P.t("listForm.collaboratorsHelp") }),
                        collabList,
                        h("div", { class: "row" }, collabSelect, addCollaborator))) : null),
            h("div", { class: "form-actions" },
                h("a", { class: "btn quiet", href: cancelHref, text: P.t("action.cancel") }),
                save));

        P.mount(main,
            P.backLink(cancelHref, P.t("action.back")),
            P.pageHeader(P.t("lists.eyebrow"), editing ? P.t("listForm.editTitle") : P.t("listForm.createTitle"), P.t("listForm.intro")),
            form);
        P.setTitle(editing ? P.t("listForm.editTitle") : P.t("listForm.createTitle"));
        name.focus();
    }

    P.page({
        session: "required",
        title: "listForm.createTitle",
        init: function (main, user) {
            var id = editID();
            var wishlist = id ? P.api("GET", "auth/wishlists/" + encodeURIComponent(id)).then(function (result) { return result.wishlist; }) : Promise.resolve(null);
            var groups = P.api("GET", "auth/groups").then(function (result) { return result.groups || []; });
            var candidates = P.api("GET", "auth/users" + (id ? "?notACollaboratorOfWishlistID=" + encodeURIComponent(id) : "")).then(function (result) { return result.users || []; }, function () { return []; });
            Promise.all([wishlist, groups, candidates]).then(function (values) {
                var list = values[0];
                if (list && P.wishlistRole(list, user.id) === "group_member") {
                    P.mount(main, P.emptyState(P.t("listForm.notAllowed")));
                    return;
                }
                render(main, user, list, values[1], values[2]);
            }, function (err) {
                P.mount(main, P.emptyState(err.message));
            });
        }
    });
})();
