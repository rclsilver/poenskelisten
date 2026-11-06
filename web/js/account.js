// First and last names can't be changed. OIDC accounts manage e-mail, password
// and MFA at their provider.
(function () {
    "use strict";
    var h = P.h;

    function profilePanel(account) {
        var oidc = account.auth_source === "oidc";
        var localLogin = !!window.LOCAL_LOGIN_ENABLED;
        var provider = window.OIDC_PROVIDER_NAME || P.t("login.sso");
        var picture = h("span", { class: "avatar large", attrs: { "aria-hidden": "true" } }, P.initials(account));
        P.profileImage(account.id, false).then(function (src) {
            if (src) {
                P.mount(picture, h("img", { src: src, alt: "" }));
            }
        });

        var email = h("input", { type: "email", name: "email", required: !oidc, readOnly: oidc, value: account.email || "", attrs: { autocomplete: "email" } });
        var file = h("input", { type: "file", name: "picture", accept: "image/png,image/jpeg,image/gif,image/webp" });
        var imageData = "";
        var imageError = h("p", { class: "field-error", role: "alert", hidden: true });
        file.addEventListener("change", function () {
            imageError.hidden = true;
            imageData = "";
            var chosen = file.files && file.files[0];
            if (!chosen) {
                return;
            }
            P.readImageFile(chosen).then(function (data) {
                imageData = data;
                P.mount(picture, h("img", { src: data, alt: "" }));
            }, function (err) {
                file.value = "";
                imageError.textContent = err.message;
                imageError.hidden = false;
            });
        });

        var password = h("input", { type: "password", name: "password", attrs: { autocomplete: "new-password" } });
        var repeat = h("input", { type: "password", name: "password_repeat", attrs: { autocomplete: "new-password" } });
        var current = h("input", { type: "password", name: "password_original", required: !oidc, attrs: { autocomplete: "current-password" } });
        var save = h("button", { type: "submit", class: "btn primary", text: P.t("action.save") });

        var note = null;
        if (oidc) {
            note = h("p", { class: "notice", text: P.t("account.oidcNote", { provider: provider }) });
        } else if (!localLogin) {
            note = h("p", { class: "notice", text: P.t("account.localLoginOffNote", { provider: provider }) });
        }

        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            if (password.value !== repeat.value) {
                P.formError(form, P.t("field.passwordMismatch"));
                return;
            }
            var changedPassword = !!password.value;
            P.busy(save, P.api("POST", "auth/users/update", {
                email: email.value.trim(),
                password: password.value,
                password_repeat: repeat.value,
                password_original: current.value,
                profile_image: imageData
            })).then(function (result) {
                current.value = "";
                password.value = "";
                repeat.value = "";
                if (result.verified === false) {
                    // A new e-mail must be verified: the sign-in flow takes
                    // the user to /verify.
                    window.location.href = "/";
                    return;
                }
                P.toast(changedPassword ? P.t("account.passwordChanged") : P.t("account.saved"));
                if (changedPassword) {
                    // Other sessions were signed out and this one renewed:
                    // reloading picks up fresh tokens.
                    window.setTimeout(function () { window.location.reload(); }, 1200);
                }
            }, function (err) {
                current.value = "";
                P.formError(form, err.message);
            });
        } } },
            note,
            P.field(P.t("field.email"), email, oidc ? null : P.t("account.emailHelp")),
            h("div", { class: "image-field" }, P.field(P.t("account.picture"), file, P.t("account.pictureHelp")), imageError),
            !oidc && localLogin ? h("fieldset", null,
                h("legend", { text: P.t("account.newPassword") }),
                h("p", { class: "help", text: P.t("account.newPasswordHelp") }),
                h("div", { class: "field-row" }, P.field(P.t("field.newPassword"), password), P.field(P.t("field.repeatPassword"), repeat))) : null,
            !oidc ? P.field(P.t("account.currentPassword"), current, P.t("account.currentPasswordHelp"), true) : null,
            h("div", { class: "form-actions" }, save));

        return h("section", { class: "panel", "aria-labelledby": "profile-title" },
            h("div", { class: "row" },
                picture,
                h("div", { class: "stack tight" },
                    h("h2", { id: "profile-title", text: P.displayName(account) }),
                    h("p", { class: "help", text: P.t("account.joined", { date: P.fmtDate(account.created_at) }) }),
                    account.admin ? h("span", { class: "badge ok", text: P.t("account.admin") }) : null)),
            h("p", { class: "help", text: P.t("account.nameFixed") }),
            h("hr"),
            form);
    }

    function mfaPanel(account) {
        var body = h("div", { class: "stack" });
        var panel = h("section", { class: "panel", "aria-labelledby": "mfa-title" },
            h("h2", { id: "mfa-title", text: P.t("mfa.heading") }),
            h("p", { class: "help", text: P.t("account.mfaIntro") }),
            body);
        var provider = window.OIDC_PROVIDER_NAME || P.t("login.sso");
        if (account.auth_source === "oidc" || !window.LOCAL_LOGIN_ENABLED) {
            P.mount(body, h("p", { class: "notice", text: P.t("account.mfaManaged", { provider: provider }) }));
            return panel;
        }

        var enabled = account.mfa_enabled === true;
        var draw = function () {
            if (enabled) {
                P.mount(body,
                    h("p", null, h("span", { class: "badge ok" }, P.icon("check"), P.t("account.mfaOn"))),
                    P.button(P.t("account.mfaDisable"), drawDisable, "", "lock"));
            } else {
                P.mount(body,
                    h("p", null, h("span", { class: "badge warn", text: P.t("account.mfaOff") })),
                    P.button(P.t("account.mfaEnable"), function () {
                        P.mfaEnrol(body, function () {
                            enabled = true;
                            P.toast(P.t("account.mfaEnabled"));
                            draw();
                        }, draw);
                    }, "primary", "lock"));
            }
        };
        var drawDisable = function () {
            var password = h("input", { type: "password", name: "password", required: true, attrs: { autocomplete: "current-password" } });
            var code = h("input", { type: "text", name: "code", required: true, attrs: { autocomplete: "one-time-code", spellcheck: "false" } });
            var submit = h("button", { type: "submit", class: "btn danger", text: P.t("account.mfaDisable") });
            var form = h("form", { class: "form-grid", on: { submit: function (event) {
                event.preventDefault();
                P.formError(form, "");
                P.busy(submit, P.api("POST", "auth/users/mfa/disable", { password: password.value, code: code.value.trim() })).then(function () {
                    enabled = false;
                    P.toast(P.t("account.mfaDisabled"));
                    draw();
                }, function (err) {
                    password.value = "";
                    code.value = "";
                    P.formError(form, err.message);
                });
            } } },
                h("p", { class: "help", text: P.t("account.mfaDisableHelp") }),
                P.field(P.t("account.currentPassword"), password, null, true),
                P.field(P.t("mfa.code"), code, P.t("mfa.codeHelp"), true),
                h("div", { class: "row" }, submit, P.button(P.t("action.cancel"), draw, "quiet")));
            P.mount(body, form);
            password.focus();
        };
        draw();
        return panel;
    }

    function preferencesPanel() {
        var theme = h("select", { name: "theme" },
            h("option", { value: "system", text: P.t("prefs.themeSystem") }),
            h("option", { value: "light", text: P.t("prefs.themeLight") }),
            h("option", { value: "dark", text: P.t("prefs.themeDark") }));
        var saved = P.prefs.get("theme");
        theme.value = saved === "light" || saved === "dark" ? saved : "system";
        theme.addEventListener("change", function () {
            P.setTheme(theme.value);
            var header = document.getElementById("pref-theme");
            if (header) {
                header.value = theme.value;
            }
        });
        var lang = h("select", { name: "language" },
            h("option", { value: "en", lang: "en", text: "English" }),
            h("option", { value: "fr", lang: "fr", text: "Français" }));
        lang.value = P.lang;
        lang.addEventListener("change", function () { P.setLang(lang.value); });
        return h("section", { class: "panel form-grid", "aria-labelledby": "prefs-title" },
            h("h2", { id: "prefs-title", text: P.t("prefs.title") }),
            h("p", { class: "help", text: P.t("account.prefsHelp") }),
            P.field(P.t("prefs.theme"), theme),
            P.field(P.t("prefs.language"), lang));
    }

    function sessionsPanel() {
        var apps = h("div", null, P.loading());
        var loadApps = function () {
            P.api("GET", "auth/connected-apps").then(function (result) {
                var list = result.apps || [];
                P.mount(apps, list.length ? list.map(function (app) {
                    return h("div", { class: "member-row" },
                        h("div", { class: "grow" },
                            h("strong", { text: app.client_name || app.client_id }),
                            h("ul", { class: "help" }, (app.scopes || []).map(function (scope) { return h("li", { text: scope.description }); }))),
                        P.button(P.t("account.disconnectApp"), function () {
                            P.confirm({ title: P.t("account.disconnectTitle"), message: P.t("account.disconnectConfirm", { name: app.client_name || app.client_id }), confirm: P.t("account.disconnectApp"), danger: true }).then(function (yes) {
                                if (yes) {
                                    P.api("DELETE", "auth/connected-apps/" + encodeURIComponent(app.client_id)).then(function () {
                                        P.toast(P.t("account.appDisconnected"));
                                        loadApps();
                                    }, P.error);
                                }
                            });
                        }, "small danger-quiet"));
                }) : h("p", { class: "help", text: P.t("account.noApps") }));
            }, function (err) {
                P.mount(apps, h("p", { class: "field-error", text: err.message }));
            });
        };
        loadApps();
        return h("section", { class: "panel stack", "aria-labelledby": "sessions-title" },
            h("h2", { id: "sessions-title", text: P.t("account.security") }),
            h("div", { class: "stack tight" },
                h("h3", { text: P.t("account.sessions") }),
                h("p", { class: "help", text: P.t("account.sessionsHelp") }),
                h("div", null, P.button(P.t("account.logoutAll"), function () {
                    P.confirm({ title: P.t("account.logoutAllTitle"), message: P.t("account.logoutAllConfirm"), confirm: P.t("account.logoutAll"), danger: true }).then(function (yes) {
                        if (yes) {
                            P.api("POST", "auth/tokens/logout-all").then(function () {
                                set_cookie("poenskelisten", "", 1);
                                window.location.href = "/login";
                            }, P.error);
                        }
                    });
                }, "danger-quiet", "logOut"))),
            h("hr"),
            h("div", { class: "stack tight" },
                h("h3", { text: P.t("account.apps") }),
                h("p", { class: "help", text: P.t("account.appsHelp") }),
                apps));
    }

    P.page({
        session: "required",
        title: "nav.account",
        init: function (main, user) {
            P.api("GET", "auth/users/" + encodeURIComponent(user.id)).then(function (result) {
                var account = result.user;
                P.mount(main,
                    P.pageHeader(P.t("account.eyebrow"), P.t("account.title"), P.t("account.intro"),
                        P.button(P.t("nav.logout"), function () { logout(); }, "", "logOut")),
                    h("div", { class: "settings-grid" },
                        profilePanel(account),
                        h("div", { class: "stack" }, mfaPanel(account), preferencesPanel()),
                        h("div", { class: "full" }, sessionsPanel())));
            }, function (err) {
                P.mount(main, P.emptyState(err.message));
            });
        }
    });
})();
