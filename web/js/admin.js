// The API checks the admin flag on every call; the page only spares others
// a page of errors.
(function () {
    "use strict";
    var h = P.h;

    function panel(id, title, help, body, wide, action) {
        return h("section", { class: "panel" + (wide ? " full" : ""), id: id, "aria-labelledby": id + "-title" },
            h("div", { class: "row spread" }, h("h2", { id: id + "-title", text: title }), action || null),
            help ? h("p", { class: "help", text: help }) : null,
            body);
    }

    function value(kind, raw) {
        if (kind === "bool") {
            return h("span", { class: "badge " + (raw ? "ok" : ""), text: raw ? P.t("admin.on") : P.t("admin.off") });
        }
        var text = raw === null || raw === undefined ? "" : String(raw);
        if (!text.trim()) {
            return h("span", { class: "muted", text: P.t("admin.notSet") });
        }
        return h("span", { class: kind === "mono" ? "mono" : "", text: text });
    }

    function rows(title, list) {
        return h("div", { class: "kv-group" },
            h("h3", { text: title }),
            h("div", { class: "kv" }, list.filter(Boolean).map(function (row) {
                return h("div", { class: "kv-row" }, h("span", { text: row[0] }), value(row[2] || "text", row[1]));
            })));
    }

    function serverInfo(info) {
        var external = (info.poenskelisten_external_url || "").trim();
        var additional = info.poenskelisten_additional_urls || [];
        var sqlite = (info.database_type || "").toLowerCase() === "sqlite";
        return [
            rows(P.t("admin.application"), [
                [P.t("admin.appName"), info.app_name],
                [P.t("admin.version"), info.poenskelisten_version],
                [P.t("admin.environment"), info.poenskelisten_environment],
                [P.t("admin.externalURL"), external || (additional.length ? P.t("admin.externalURLMissingAdditional", { issuer: info.oauth_issuer }) : P.t("admin.externalURLMissing", { issuer: info.oauth_issuer })), external ? "mono" : "text"],
                [P.t("admin.additionalURLs"), additional.join(", "), "mono"],
                [P.t("admin.port"), info.poenskelisten_port],
                [P.t("admin.timezone"), info.timezone],
                [P.t("admin.logLevel"), info.poenskelisten_log_level],
                (info.poenskelisten_environment || "").toLowerCase() === "test" ? [P.t("admin.testEmail"), info.poenskelisten_test_email, "mono"] : null
            ]),
            h("p", { class: "help", text: P.t("admin.appNameHelp") }),
            rows(P.t("admin.database"), sqlite ? [
                [P.t("admin.type"), info.database_type],
                [P.t("admin.file"), info.database_location, "mono"]
            ] : [
                [P.t("admin.type"), info.database_type],
                [P.t("admin.name"), info.database_name],
                [P.t("admin.host"), info.database_host, "mono"],
                [P.t("admin.port"), info.database_port],
                ["SSL", info.database_ssl, "bool"]
            ]),
            rows(P.t("admin.email"), [
                [P.t("admin.status"), info.smtp_enabled, "bool"],
                info.smtp_enabled ? [P.t("admin.host"), info.smtp_host, "mono"] : null,
                info.smtp_enabled ? [P.t("admin.port"), info.smtp_port] : null,
                info.smtp_enabled ? [P.t("admin.from"), info.smtp_from, "mono"] : null
            ]),
            rows(P.t("admin.sso"), [
                [P.t("admin.status"), info.oidc_enabled, "bool"],
                info.oidc_enabled ? [P.t("admin.provider"), info.oidc_provider_name] : null,
                info.oidc_enabled ? [P.t("admin.issuer"), info.oidc_issuer_url, "mono"] : null,
                info.oidc_enabled ? [P.t("admin.clientID"), info.oidc_client_id, "mono"] : null,
                info.oidc_enabled ? [P.t("admin.redirectURL"), info.oidc_redirect_url, "mono"] : null,
                info.oidc_enabled ? [P.t("admin.autoCreate"), info.oidc_auto_create_users, "bool"] : null,
                info.oidc_enabled ? [P.t("admin.passwordLogin"), info.local_login_enabled, "bool"] : null
            ]),
            rows(P.t("admin.security"), [
                [P.t("admin.mfaEnforced"), info.mfa_enforced, "bool"],
                [P.t("admin.recoveryCodes"), info.mfa_recovery_codes_enabled, "bool"]
            ]),
            rows(P.t("admin.mcp"), [
                [P.t("admin.status"), info.mcp_enabled, "bool"],
                info.mcp_enabled ? [P.t("admin.endpoint"), info.mcp_endpoint, "mono"] : null,
                [P.t("admin.appRegistration"), info.mcp_enabled, "bool"]
            ])
        ];
    }

    function newsDialog(post, onDone) {
        var title = h("input", { type: "text", name: "title", required: true, value: post ? post.title : "", attrs: { minlength: 5, maxlength: 200, autocomplete: "off" } });
        var body = h("textarea", { name: "body", required: true, value: post ? post.body : "", attrs: { minlength: 5 } });
        var date = h("input", { type: "date", name: "date", required: true, value: post ? P.dayInputValue(post.date) : P.todayInputValue() });
        var expiry = h("input", { type: "date", name: "expiry_date", value: post && post.expiry_date ? P.dayInputValue(post.expiry_date) : "" });
        var form = h("div", { class: "form-grid" },
            P.field(P.t("news.postTitle"), title, P.t("news.minLength"), true),
            P.field(P.t("news.postBody"), body, P.t("news.minLength"), true),
            h("div", { class: "field-row" },
                P.field(P.t("news.date"), date, P.t("news.dateHelp"), true),
                P.field(P.t("news.expiry"), expiry, P.t("news.expiryHelp"))));
        P.dialog({
            title: post ? P.t("news.edit") : P.t("news.create"),
            content: form,
            actions: [
                { label: P.t("action.cancel"), kind: "quiet" },
                { label: post ? P.t("action.save") : P.t("news.publish"), kind: "primary", onClick: function () {
                    P.formError(form, "");
                    if (title.value.trim().length < 5 || body.value.trim().length < 5) {
                        P.formError(form, P.t("news.minLength"));
                        return false;
                    }
                    if (!P.dayToApi(date.value)) {
                        P.formError(form, P.t("news.dateRequired"));
                        return false;
                    }
                    if (expiry.value && expiry.value < date.value) {
                        P.formError(form, P.t("news.expiryBeforeDate"));
                        return false;
                    }
                    var payload = {
                        title: title.value.trim(),
                        body: body.value.trim(),
                        date: P.dayToApi(date.value),
                        expiry_date: expiry.value ? P.dayToApi(expiry.value) : null
                    };
                    var request = post ? P.api("POST", "admin/news/" + encodeURIComponent(post.id), payload) : P.api("POST", "admin/news", payload);
                    return request.then(function () {
                        P.toast(post ? P.t("news.saved") : P.t("news.published"));
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

    function newsList(container) {
        var load = function () {
            P.api("GET", "auth/news").then(function (result) {
                var now = Date.now();
                var posts = (result.news || []).slice().sort(function (a, b) { return Date.parse(b.date) - Date.parse(a.date); });
                P.mount(container, posts.length ? h("div", { class: "news-grid" }, posts.map(function (post) {
                    var scheduled = Date.parse(post.date) > now;
                    return h("article", { class: "news-post" },
                        h("div", { class: "row spread" },
                            h("span", { class: "eyebrow", text: P.fmtDay(post.date) }),
                            h("span", { class: "badge " + (scheduled ? "warn" : "ok"), text: scheduled ? P.t("news.scheduled") : P.t("news.live") })),
                        h("h3", { text: post.title }),
                        h("p", { text: post.body }),
                        post.expiry_date ? h("p", { class: "help", text: P.t("news.expiresOn", { date: P.fmtDay(post.expiry_date) }) }) : null,
                        h("div", { class: "row news-actions" },
                            P.button(P.t("action.edit"), function () { newsDialog(post, load); }, "small", "edit"),
                            P.button(P.t("action.delete"), function () {
                                P.confirm({ title: P.t("news.deleteTitle"), message: post.title, confirm: P.t("action.delete"), danger: true }).then(function (yes) {
                                    if (yes) {
                                        P.api("DELETE", "admin/news/" + encodeURIComponent(post.id)).then(function () {
                                            P.toast(P.t("news.deleted"));
                                            load();
                                        }, P.error);
                                    }
                                });
                            }, "small danger-quiet", "trash")));
                })) : P.emptyState(P.t("news.none")));
            }, function (err) { P.mount(container, P.emptyState(err.message)); });
        };
        load();
        return load;
    }

    function userDialog(userID, onChange) {
        var body = h("div", null, P.loading());
        var dialog = P.dialog({ title: P.t("admin.user"), content: body });
        P.api("GET", "auth/users/" + encodeURIComponent(userID)).then(function (result) {
            var person = result.user;
            var act = function (confirmOptions, method, path, done) {
                return function () {
                    P.confirm(confirmOptions).then(function (yes) {
                        if (yes) {
                            P.api(method, path).then(function (response) {
                                P.toast(response.message || P.t("admin.done"));
                                dialog.close();
                                if (done) {
                                    done();
                                }
                            }, P.error);
                        }
                    });
                };
            };
            var base = "admin/users/" + encodeURIComponent(person.id);
            P.mount(body, h("div", { class: "stack" },
                h("div", { class: "row" }, P.avatar(person, "large"), h("div", { class: "stack tight" },
                    h("strong", { text: P.displayName(person) }),
                    person.email ? h("span", { class: "mono", text: person.email }) : null)),
                h("div", { class: "kv" },
                    h("div", { class: "kv-row" }, h("span", { text: P.t("admin.joined") }), h("span", { text: P.fmtDate(person.created_at) })),
                    h("div", { class: "kv-row" }, h("span", { text: P.t("account.admin") }), value("bool", person.admin)),
                    h("div", { class: "kv-row" }, h("span", { text: P.t("mfa.heading") }), value("bool", person.mfa_enabled)),
                    h("div", { class: "kv-row" }, h("span", { text: P.t("admin.signIn") }), h("span", { text: person.auth_source === "oidc" ? P.t("admin.signInOIDC") : P.t("admin.signInLocal") }))),
                h("div", { class: "row" },
                    P.button(P.t("admin.revokeSessions"), act({ title: P.t("admin.revokeSessionsTitle"), message: P.t("admin.revokeSessionsConfirm", { name: P.displayName(person) }), confirm: P.t("admin.revokeSessions"), danger: true }, "DELETE", base + "/sessions"), "", "logOut"),
                    person.mfa_enabled ? P.button(P.t("admin.removeMFA"), act({ title: P.t("admin.removeMFATitle"), message: P.t("admin.removeMFAConfirm", { name: P.displayName(person) }), confirm: P.t("admin.removeMFA"), danger: true }, "DELETE", base + "/mfa", onChange), "", "lock") : null,
                    P.button(P.t("admin.deleteUser"), act({ title: P.t("admin.deleteUserTitle"), message: P.t("admin.deleteUserConfirm", { name: P.displayName(person) }), confirm: P.t("admin.deleteUser"), danger: true }, "DELETE", base, onChange), "danger", "trash"))));
        }, function (err) {
            P.mount(body, h("p", { class: "field-error", text: err.message }));
        });
    }

    function usersList(container, me) {
        var load = function () {
            P.api("GET", "auth/users").then(function (result) {
                var users = (result.users || []).slice().sort(function (a, b) { return P.displayName(a).localeCompare(P.displayName(b)); });
                P.mount(container, users.map(function (person) {
                    return h("div", { class: "member-row" },
                        P.avatar(person),
                        h("span", { class: "grow" }, P.displayName(person), person.email ? h("small", { class: "mono", text: person.email }) : null),
                        person.admin ? h("span", { class: "badge ok", text: P.t("account.admin") }) : null,
                        person.id === me.id ? h("span", { class: "badge", text: P.t("group.you") }) : P.button(P.t("admin.manage"), function () { userDialog(person.id, load); }, "small"));
                }));
            }, function (err) { P.mount(container, h("p", { class: "field-error", text: err.message })); });
        };
        load();
        return load;
    }

    function invitesList(container, onUser) {
        var draw = function (invites) {
            P.mount(container, invites.length ? invites.map(function (invite) {
                var link = window.location.origin + "/register?invite=" + encodeURIComponent(invite.invite_code);
                return h("div", { class: "member-row" },
                    h("span", { class: "grow" }, h("code", { class: "mono", text: invite.invite_code }),
                        invite.invite_used ? h("small", { text: P.t("admin.inviteUsedBy", { name: P.displayName(invite.user) }) }) : h("small", { text: P.t("admin.inviteFree") })),
                    invite.invite_used ? P.button(P.t("admin.manage"), function () { onUser(invite.user.id); }, "small") : [
                        P.button(P.t("admin.copyInvite"), function () { P.copyText(link); }, "small icon", "copy"),
                        P.button(P.t("admin.deleteInvite"), function () {
                            P.confirm({ title: P.t("admin.deleteInviteTitle"), message: invite.invite_code, confirm: P.t("action.delete"), danger: true }).then(function (yes) {
                                if (yes) {
                                    P.api("DELETE", "admin/invites/" + encodeURIComponent(invite.id)).then(function (result) { draw(result.invites || []); }, P.error);
                                }
                            });
                        }, "small icon danger-quiet", "trash")
                    ]);
            }) : h("p", { class: "help", text: P.t("admin.noInvites") }));
        };
        var load = function () {
            P.api("GET", "admin/invites").then(function (result) { draw(result.invites || []); }, function (err) {
                P.mount(container, h("p", { class: "field-error", text: err.message }));
            });
        };
        load();
        return {
            generate: function (button) {
                P.busy(button, P.api("POST", "admin/invites")).then(function (result) {
                    P.toast(P.t("admin.inviteCreated"));
                    draw(result.invites || []);
                }, P.error);
            }
        };
    }

    function clientsList(container) {
        var load = function () {
            P.api("GET", "admin/oauth/clients").then(function (result) {
                var clients = result.clients || [];
                P.mount(container, clients.length ? clients.map(function (client) {
                    var name = client.client_name && client.client_name.trim() ? client.client_name : client.client_id;
                    var state = client.is_first_party ? h("span", { class: "badge", text: P.t("admin.builtIn") }) :
                        client.enabled ? P.button(P.t("admin.revoke"), function () {
                            P.confirm({ title: P.t("admin.revokeClientTitle"), message: P.t("admin.revokeClientConfirm", { name: name }), confirm: P.t("admin.revoke"), danger: true }).then(function (yes) {
                                if (yes) {
                                    P.api("DELETE", "admin/oauth/clients/" + encodeURIComponent(client.client_id)).then(function () {
                                        P.toast(P.t("admin.clientRevoked"));
                                        load();
                                    }, P.error);
                                }
                            });
                        }, "small danger-quiet") : h("span", { class: "badge danger", text: P.t("admin.revoked") });
                    return h("div", { class: "member-row" }, h("span", { class: "grow", text: name }), state);
                }) : h("p", { class: "help", text: P.t("admin.noClients") }));
            }, function (err) { P.mount(container, h("p", { class: "field-error", text: err.message })); });
        };
        load();
    }

    function currencyForm() {
        var currency = h("input", { type: "text", name: "currency", required: true, attrs: { maxlength: 10, autocomplete: "off" } });
        var padding = P.checkbox(P.t("admin.currencyPad"), false, P.t("admin.currencyPadHelp"));
        var left = P.checkbox(P.t("admin.currencyLeft"), false, P.t("admin.currencyLeftHelp"));
        var example = h("p", { class: "help" });
        var showExample = function () {
            example.textContent = P.t("admin.currencyExample", { price: P.fmtPrice(129.5, { currency: currency.value.trim(), padding: padding.input.checked, left: left.input.checked }) });
        };
        [currency, padding.input, left.input].forEach(function (input) { input.addEventListener("input", showExample); input.addEventListener("change", showExample); });
        var fill = function (result) {
            currency.value = result.currency || "";
            padding.input.checked = !!result.padding;
            left.input.checked = !!result.left;
            showExample();
        };
        P.api("GET", "auth/currency").then(fill, P.error);
        var save = h("button", { type: "submit", class: "btn primary", text: P.t("action.save") });
        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            P.busy(save, P.api("POST", "admin/currency/update", {
                poenskelisten_currency: currency.value.trim(),
                poenskelisten_currency_pad: padding.input.checked,
                poenskelisten_currency_left: left.input.checked
            })).then(function (result) {
                fill(result);
                P.toast(P.t("admin.saved"));
            }, function (err) { P.formError(form, err.message); });
        } } }, P.field(P.t("admin.currency"), currency), padding.el, left.el, example, h("div", null, save));
        return form;
    }

    function securityForm(info) {
        var enforced = P.checkbox(P.t("admin.mfaEnforced"), false, P.t("admin.mfaEnforcedHelp"));
        var codes = P.checkbox(P.t("admin.recoveryCodes"), false, P.t("admin.recoveryCodesHelp"));
        var save = h("button", { type: "submit", class: "btn primary", text: P.t("action.save") });
        var fill = function (data) {
            enforced.input.checked = data.mfa_enforced === true;
            codes.input.checked = data.mfa_recovery_codes_enabled === true;
        };
        info.then(fill);
        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            P.busy(save, P.api("POST", "admin/server/settings", { mfa_enforced: enforced.input.checked, mfa_recovery_codes_enabled: codes.input.checked })).then(function (result) {
                fill(result);
                P.toast(P.t("admin.saved"));
            }, function (err) { P.formError(form, err.message); });
        } } }, enforced.el, codes.el, h("div", null, save));
        return form;
    }

    P.page({
        session: "required",
        title: "nav.admin",
        init: function (main, user) {
            if (!user.admin) {
                P.mount(main, P.emptyState(P.t("admin.notAdmin"), h("a", { class: "btn", href: "/", text: P.t("nav.home") })));
                return;
            }
            var infoBox = h("div", null, P.loading());
            var newsBox = h("div", null, P.loading());
            var usersBox = h("div", null, P.loading());
            var invitesBox = h("div", null, P.loading());
            var clientsBox = h("div", null, P.loading());

            var info = P.api("POST", "admin/server/info").then(function (result) { return result.server; });
            info.then(function (server) { P.mount(infoBox, serverInfo(server)); }, function (err) { P.mount(infoBox, h("p", { class: "field-error", text: err.message })); });

            var reloadNews = newsList(newsBox);
            var reloadUsers = usersList(usersBox, user);
            var invites = invitesList(invitesBox, function (id) { userDialog(id, reloadUsers); });
            clientsList(clientsBox);

            var generate = P.button(P.t("admin.generateInvite"), function () { invites.generate(generate); }, "small primary", "plus");

            P.mount(main,
                P.pageHeader(P.t("admin.eyebrow"), P.t("admin.title"), P.t("admin.intro")),
                h("div", { class: "settings-grid" },
                    panel("news", P.t("news.title"), P.t("admin.newsHelp"), newsBox, true,
                        P.button(P.t("news.create"), function () { newsDialog(null, reloadNews); }, "small primary", "plus")),
                    panel("invites", P.t("admin.invites"), P.t("admin.invitesHelp"), invitesBox, false, generate),
                    panel("users", P.t("admin.users"), P.t("admin.usersHelp"), usersBox),
                    panel("currency", P.t("admin.currencyTitle"), P.t("admin.currencyHelp"), currencyForm()),
                    panel("security", P.t("admin.security"), P.t("admin.securityHelp"), securityForm(info)),
                    panel("clients", P.t("admin.clients"), P.t("admin.clientsHelp"), clientsBox),
                    panel("server", P.t("admin.server"), P.t("admin.serverHelp"), infoBox)));

            if (window.location.hash) {
                var target = document.getElementById(window.location.hash.slice(1));
                if (target) {
                    target.scrollIntoView();
                }
            }
        }
    });
})();
