// Sign-in page: password login, the MFA step (/login/mfa), single sign-on,
// the forgotten-password request and the password reset reached from the
// e-mailed link (?reset_code=).
(function () {
    "use strict";
    var h = P.h;

    // After a successful login, return to the OAuth flow (the `next` URL set by
    // /oauth/authorize) or the home page. Only same-site paths are followed.
    function redirectAfterLogin() {
        var next = new URL(window.location.href).searchParams.get("next");
        window.location.href = P.safeLocalPath(next || "/");
    }

    // The MFA step lives on its own URL so it's reached by a real navigation
    // rather than a DOM swap - password managers only reliably offer
    // one-time-code autofill across an actual page load. The challenge token
    // can't travel in the URL without leaking into browser history, so it
    // rides in sessionStorage instead.
    function goToMfaStep(mfaToken) {
        try {
            sessionStorage.setItem("mfa_token", mfaToken);
        } catch (e) {
            // Without storage the MFA page falls back to the login form.
        }
        var target = new URL(window.location.origin + "/login/mfa");
        var next = new URL(window.location.href).searchParams.get("next");
        if (next) {
            target.searchParams.set("next", next);
        }
        window.location.href = target.toString();
    }

    function emailInput(value) {
        return h("input", { type: "email", name: "email", value: value || "", required: true, attrs: { autocomplete: "username" } });
    }

    function submitButton(label) {
        return h("button", { type: "submit", class: "btn primary", text: label });
    }

    function oidcButton(label, url) {
        return h("a", { class: "btn wide", href: P.safeLocalPath(url) }, P.icon("lock"), label);
    }

    function showLogin(main, message) {
        if (!window.LOCAL_LOGIN_ENABLED) {
            var provider = window.OIDC_PROVIDER_NAME || P.t("login.sso");
            P.authLayout(main, P.t("login.title"), P.t("login.oidcOnlyIntro", { provider: provider }), [
                h("h2", { text: P.t("login.heading") }),
                message ? h("p", { class: "notice danger", role: "alert", text: message }) : null,
                h("a", { class: "btn primary wide", href: "/api/open/oidc/login" }, P.icon("lock"), P.t("login.withProvider", { provider: provider }))
            ]);
            return;
        }

        var email = emailInput("");
        var password = h("input", { type: "password", name: "password", required: true, attrs: { autocomplete: "current-password" } });
        var button = submitButton(P.t("login.submit"));
        var oidc = h("div", { class: "stack tight", hidden: true });
        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            P.busy(button, P.api("POST", "open/tokens/register", { email: email.value, password: password.value })).then(function (result) {
                password.value = "";
                if (result.mfa_required) {
                    goToMfaStep(result.mfa_token);
                } else {
                    button.disabled = true;
                    redirectAfterLogin();
                }
            }, function (err) {
                password.value = "";
                P.formError(form, err.message);
            });
        } } },
            message ? h("p", { class: "notice danger form-error", role: "alert", text: message }) : null,
            P.field(P.t("field.email"), email),
            P.field(P.t("field.password"), password),
            button);

        P.authLayout(main, P.t("login.title"), P.t("login.intro"), [
            h("h2", { text: P.t("login.heading") }),
            form,
            oidc,
            h("div", { class: "auth-links" },
                h("a", { href: "#", text: P.t("login.forgot"), on: { click: function (event) {
                    event.preventDefault();
                    showForgot(main, email.value);
                } } }),
                h("a", { href: "/register", text: P.t("login.noAccount") }))
        ]);

        // Offer single sign-on next to the password form when configured.
        P.api("GET", "open/oidc/config").then(function (config) {
            if (config.enabled && config.login_url) {
                P.mount(oidc,
                    h("div", { class: "separator", text: P.t("login.or") }),
                    oidcButton(P.t("login.withProvider", { provider: config.provider_name || P.t("login.sso") }), config.login_url));
                oidc.hidden = false;
            }
        }, function () {
            // No SSO offered: the password form is enough.
        });
        email.focus();
    }

    function showForgot(main, value) {
        var email = emailInput(value);
        var button = submitButton(P.t("forgot.submit"));
        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            P.busy(button, P.api("POST", "open/users/reset", { email: email.value })).then(function (result) {
                P.mount(form, h("p", { class: "notice ok", role: "status", text: result.message || P.t("forgot.sent") }));
            }, function (err) {
                P.formError(form, err.message);
            });
        } } }, P.field(P.t("field.email"), email), button);

        P.authLayout(main, P.t("forgot.title"), P.t("forgot.intro"), [
            h("h2", { text: P.t("forgot.heading") }),
            form,
            h("div", { class: "auth-links" }, h("a", { href: "/login", text: P.t("login.back") }))
        ], "book");
        email.focus();
    }

    function showReset(main, resetCode) {
        P.api("GET", "open/users/reset/" + encodeURIComponent(resetCode)).then(function (result) {
            if (result.expired) {
                showLogin(main, P.t("reset.expired"));
                return;
            }
            var password = h("input", { type: "password", name: "password", required: true, attrs: { autocomplete: "new-password" } });
            var repeat = h("input", { type: "password", name: "password_repeat", required: true, attrs: { autocomplete: "new-password" } });
            var button = submitButton(P.t("reset.submit"));
            var form = h("form", { class: "form-grid", on: { submit: function (event) {
                event.preventDefault();
                P.formError(form, "");
                if (password.value !== repeat.value) {
                    P.formError(form, P.t("field.passwordMismatch"));
                    return;
                }
                P.busy(button, P.api("POST", "open/users/password", { reset_code: resetCode, password: password.value, password_repeat: repeat.value })).then(function () {
                    history.replaceState(null, "", "/login");
                    P.toast(P.t("reset.done"));
                    showLogin(main);
                }, function (err) {
                    P.formError(form, err.message);
                });
            } } },
                P.field(P.t("field.newPassword"), password),
                P.field(P.t("field.repeatPassword"), repeat),
                button);
            P.authLayout(main, P.t("reset.title"), P.t("reset.intro"), [h("h2", { text: P.t("reset.heading") }), form], "book");
            password.focus();
        }, function (err) {
            showLogin(main, err.message);
        });
    }

    function showMfa(main, mfaToken) {
        var code = h("input", { type: "text", name: "totp", required: true, autofocus: true, attrs: { autocomplete: "one-time-code", inputmode: "text", spellcheck: "false" } });
        var button = submitButton(P.t("mfa.verify"));
        var form = h("form", { class: "form-grid", on: { submit: function (event) {
            event.preventDefault();
            P.formError(form, "");
            P.busy(button, P.api("POST", "open/tokens/mfa", { mfa_token: mfaToken, code: code.value.trim() })).then(function () {
                try {
                    sessionStorage.removeItem("mfa_token");
                } catch (e) {
                    // Nothing left to clean up.
                }
                button.disabled = true;
                redirectAfterLogin();
            }, function (err) {
                code.value = "";
                P.formError(form, err.message);
            });
        } } }, P.field(P.t("mfa.code"), code, P.t("mfa.codeHelp")), button);

        P.authLayout(main, P.t("mfa.title"), P.t("mfa.intro"), [
            h("h2", { text: P.t("mfa.heading") }),
            form,
            h("div", { class: "auth-links" }, h("a", { href: "/login", text: P.t("login.back"), on: { click: function () {
                try {
                    sessionStorage.removeItem("mfa_token");
                } catch (e) {
                    // Nothing to clean up.
                }
            } } }))
        ], "lamp");
        code.focus();
    }

    P.page({
        title: "login.heading",
        init: function (main) {
            // Any stale access token is dropped: signing in issues a new one.
            set_cookie("poenskelisten", "", 1);
            var params = new URL(window.location.href).searchParams;
            var message = params.get("error") || "";

            var mfaToken = null;
            if (window.location.pathname === "/login/mfa") {
                try {
                    mfaToken = sessionStorage.getItem("mfa_token");
                } catch (e) {
                    mfaToken = null;
                }
                if (!mfaToken || !window.LOCAL_LOGIN_ENABLED) {
                    // Nothing to resume (e.g. a hard refresh lost the token).
                    history.replaceState(null, "", "/login");
                    mfaToken = null;
                }
            }

            if (mfaToken) {
                showMfa(main, mfaToken);
            } else if (params.get("reset_code") && window.LOCAL_LOGIN_ENABLED) {
                showReset(main, params.get("reset_code"));
            } else {
                showLogin(main, message);
            }
        }
    });
})();
