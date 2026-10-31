// An edited wish stays on its list; a new one goes where P.wishDestination says.
(function () {
    "use strict";
    var h = P.h;
    var NEW_CATEGORY = "__new__";

    function editID() {
        var match = window.location.pathname.match(/^\/wishes\/([^\/]+)\/edit$/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    // The API refuses these characters in names, notes and links.
    function forbidden(text) {
        return /[<>"`]/.test(text);
    }

    function render(main, user, options) {
        var wish = options.wish;
        var editing = !!wish;
        var eligible = options.eligible;
        var params = new URL(window.location.href).searchParams;
        var origin = editing || params.get("wishlist") ? "list" : "home";
        var destination = editing ? { mode: "fixed", listID: wish.wishlist_id, canChange: false } : P.wishDestination(origin, eligible, params.get("wishlist") || "");
        var listName = function (id) {
            var found = options.lists.filter(function (l) { return l.id === id; })[0];
            return found ? found.name : "";
        };
        var backHref = function () {
            return destination.listID && origin === "list" ? "/wishlists/" + encodeURIComponent(destination.listID) : "/";
        };

        if (destination.mode === "none") {
            P.mount(main,
                P.backLink("/", P.t("nav.home")),
                P.pageHeader(P.t("wishForm.eyebrow"), P.t("wishForm.createTitle")),
                P.emptyState(P.t("wishForm.noList"), h("a", { class: "btn primary", href: "/wishlists/new" }, P.icon("plus"), P.t("list.create"))));
            return;
        }

        var listSelect = h("select", { name: "wishlist", required: true },
            h("option", { value: "", text: P.t("wishForm.chooseList") }),
            eligible.map(function (l) { return h("option", { value: l.id, text: l.name, selected: l.id === destination.listID }); }));
        var destinationBox = h("div");
        var drawDestination = function (picking) {
            if (picking) {
                P.mount(destinationBox, P.field(P.t("wishForm.list"), listSelect, destination.mode === "pick" ? P.t("wishForm.chooseListHelp") : null, true));
                return;
            }
            P.mount(destinationBox, h("div", { class: "notice destination" },
                h("span", null, editing ? P.t("wishForm.inList") : P.t("wishForm.addedTo"), " ", h("strong", { text: listName(destination.listID) })),
                destination.canChange ? P.button(P.t("wishForm.changeList"), function () {
                    drawDestination(true);
                    listSelect.focus();
                }, "small") : null));
        };
        drawDestination(destination.mode === "pick");
        var currentList = function () {
            return destinationBox.contains(listSelect) ? listSelect.value : destination.listID;
        };

        var name = h("input", { type: "text", name: "name", required: true, value: editing ? wish.name : "", attrs: { maxlength: 200, autocomplete: "off" } });
        var price = h("input", { type: "number", name: "price", value: editing && wish.price ? String(wish.price) : "", attrs: { min: 0, step: "0.01", inputmode: "decimal" } });
        var url = h("input", { type: "text", name: "url", value: editing ? wish.url || "" : "", placeholder: "https://", attrs: { inputmode: "url", autocomplete: "off", spellcheck: "false" } });
        var note = h("textarea", { name: "note", value: editing ? wish.note || "" : "" });

        var category = h("select", { name: "category" });
        var newCategory = h("input", { type: "text", name: "category_name", attrs: { maxlength: 100, autocomplete: "off" } });
        var newCategoryField = P.field(P.t("wishForm.newCategory"), newCategory);
        var categories = [];
        var loadCategories = function () {
            var id = currentList();
            var keep = category.value || (editing && wish.category ? wish.category.id : "");
            var fill = function () {
                P.mount(category,
                    h("option", { value: "", text: P.t("wishForm.noCategory") }),
                    categories.map(function (c) { return h("option", { value: c.id, text: c.name }); }),
                    h("option", { value: NEW_CATEGORY, text: P.t("wishForm.addCategory") }));
                category.value = categories.some(function (c) { return c.id === keep; }) || keep === NEW_CATEGORY ? keep : "";
                newCategoryField.hidden = category.value !== NEW_CATEGORY;
                preview();
            };
            if (!id) {
                categories = [];
                fill();
                return;
            }
            P.api("GET", "auth/wishlists/" + encodeURIComponent(id) + "/categories").then(function (result) {
                categories = result.categories || [];
                fill();
            }, function () {
                categories = [];
                fill();
            });
        };

        var hadImage = editing && !!wish.image;
        var currentImage = null;
        var image = { action: hadImage ? "keep" : "none", data: "" };
        var file = h("input", { type: "file", name: "image", accept: "image/png,image/jpeg" });
        var imageError = h("p", { class: "field-error", role: "alert", hidden: true });
        var removeImage = P.button(P.t("wishForm.removeImage"), function () {
            image = { action: hadImage ? "delete" : "none", data: "" };
            file.value = "";
            drawImageButtons();
            preview();
        }, "small", "trash");
        var keepImage = P.button(P.t("wishForm.keepImage"), function () {
            image = { action: "keep", data: "" };
            file.value = "";
            drawImageButtons();
            preview();
        }, "small");
        var drawImageButtons = function () {
            removeImage.hidden = image.action === "none" || image.action === "delete";
            keepImage.hidden = !hadImage || image.action === "keep";
        };
        file.addEventListener("change", function () {
            imageError.hidden = true;
            var chosen = file.files && file.files[0];
            if (!chosen) {
                return;
            }
            P.readImageFile(chosen).then(function (data) {
                image = { action: "replace", data: data };
                drawImageButtons();
                preview();
            }, function (err) {
                file.value = "";
                imageError.textContent = err.message;
                imageError.hidden = false;
            });
        });
        drawImageButtons();
        if (hadImage) {
            P.wishImage(wish.id, true).then(function (src) {
                currentImage = src;
                preview();
            });
        }

        var previewBox = h("div");
        var preview = function () {
            var amount = price.value === "" ? null : Number(price.value);
            var categoryName = category.value === NEW_CATEGORY ? newCategory.value.trim() :
                (categories.filter(function (c) { return c.id === category.value; })[0] || {}).name;
            var draft = {
                name: name.value.trim() || P.t("wishForm.previewName"),
                note: note.value.trim(),
                price: amount,
                category: categoryName ? { name: categoryName } : null,
                wish_claimable: false
            };
            var src = image.action === "replace" ? image.data : image.action === "keep" ? currentImage : null;
            P.mount(previewBox, P.wishCard(draft, { mode: "preview", previewImage: src, currency: options.currency }));
        };
        [name, price, note, newCategory].forEach(function (input) { input.addEventListener("input", preview); });
        category.addEventListener("change", function () {
            newCategoryField.hidden = category.value !== NEW_CATEGORY;
            if (!newCategoryField.hidden) {
                newCategory.focus();
            }
            preview();
        });
        listSelect.addEventListener("change", loadCategories);

        var save = h("button", { type: "submit", class: "btn primary", text: editing ? P.t("action.save") : P.t("wish.add") });
        var form = h("form", { class: "panel form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            var listID = currentList();
            if (!listID) {
                P.formError(form, P.t("wishForm.chooseListFirst"));
                listSelect.focus();
                return;
            }
            var link = "";
            if (url.value.trim()) {
                link = P.safeExternalUrl(url.value);
                if (!link) {
                    P.formError(form, P.t("wishForm.badLink"));
                    url.focus();
                    return;
                }
            }
            if ([name.value, note.value, link].some(forbidden)) {
                P.formError(form, P.t("field.forbiddenChars"));
                return;
            }
            var body = {
                name: name.value.trim(),
                note: note.value.trim(),
                price: price.value === "" ? null : Number(price.value),
                url: link,
                image_data: image.action === "replace" ? image.data : "",
                category_id: null,
                category_name: ""
            };
            if (category.value === NEW_CATEGORY) {
                body.category_name = newCategory.value.trim();
            } else if (category.value) {
                body.category_id = category.value;
            }
            var request;
            if (editing) {
                body.image_delete = image.action === "delete";
                request = P.api("POST", "auth/wishes/" + encodeURIComponent(wish.id), body);
            } else {
                request = P.api("POST", "auth/wishes?wishlist=" + encodeURIComponent(listID), body);
            }
            P.busy(save, request).then(function () {
                if (editing) {
                    P.forgetWishImage(wish.id);
                }
                window.location.href = "/wishlists/" + encodeURIComponent(listID) + (editing ? "#wish-" + encodeURIComponent(wish.id) : "");
            }, function (err) {
                P.formError(form, err.message);
            });
        } } },
            destinationBox,
            P.field(P.t("wishForm.name"), name, P.t("wishForm.nameHelp"), true),
            h("div", { class: "field-row" },
                P.field(options.currency.currency ? P.t("wishForm.priceIn", { currency: options.currency.currency }) : P.t("wishForm.price"), price),
                P.field(P.t("wishForm.link"), url, P.t("wishForm.linkHelp"))),
            P.field(P.t("wishForm.note"), note, P.t("wishForm.noteHelp")),
            h("div", { class: "field-row" }, P.field(P.t("wishForm.category"), category), newCategoryField),
            h("div", { class: "image-field" },
                P.field(P.t("wishForm.image"), file, P.t("wishForm.imageHelp")),
                imageError,
                h("div", { class: "row" }, removeImage, keepImage)),
            h("div", { class: "form-actions" },
                h("a", { class: "btn quiet", href: backHref(), text: P.t("action.cancel") }),
                save));

        P.mount(main,
            P.backLink(backHref(), origin === "list" ? P.t("wishForm.backToList") : P.t("nav.home")),
            P.pageHeader(P.t("wishForm.eyebrow"), editing ? P.t("wishForm.editTitle") : P.t("wishForm.createTitle"), P.t("wishForm.intro")),
            h("div", { class: "editor-grid" },
                form,
                h("aside", { class: "preview-panel", "aria-label": P.t("wishForm.preview") },
                    h("span", { class: "eyebrow", text: P.t("wishForm.preview") }),
                    previewBox,
                    h("p", { class: "help", text: P.t("wishForm.previewHelp") }))));
        P.setTitle(editing ? P.t("wishForm.editTitle") : P.t("wishForm.createTitle"));
        loadCategories();
        preview();
        if (destination.mode === "pick") {
            listSelect.focus();
        } else {
            name.focus();
        }
    }

    P.page({
        session: "required",
        title: "wishForm.createTitle",
        init: function (main, user) {
            var id = editID();
            var lists = P.api("GET", "auth/wishlists").then(function (result) { return result.wishlists || []; });
            var wish = id ? P.api("GET", "auth/wishes/" + encodeURIComponent(id)).then(function (result) { return result.wish; }) : Promise.resolve(null);
            var currency = P.api("GET", "auth/currency").then(function (result) {
                return { currency: result.currency, padding: result.padding, left: result.left };
            }, function () { return {}; });
            Promise.all([lists, wish, currency]).then(function (values) {
                render(main, user, {
                    lists: values[0],
                    eligible: P.eligibleWishlists(values[0], user.id),
                    wish: values[1],
                    currency: values[2]
                });
            }, function (err) {
                P.mount(main, P.backLink("/", P.t("nav.home")), P.emptyState(err.message));
            });
        }
    });
})();
