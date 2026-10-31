// Registration: name, e-mail, password twice, an invite code and consent.
// On an OIDC-only server accounts come from the identity provider instead.
(function () {
    "use strict";
    var h = P.h;

    function input(type, name, autocomplete, value) {
        return h("input", { type: type, name: name, required: true, value: value || "", attrs: { autocomplete: autocomplete } });
    }

    P.page({
        title: "register.heading",
        init: function (main) {
            set_cookie("poenskelisten", "", 1);

            if (!window.LOCAL_LOGIN_ENABLED) {
                var provider = window.OIDC_PROVIDER_NAME || P.t("login.sso");
                P.authLayout(main, P.t("register.title"), P.t("register.oidcOnly", { provider: provider }), [
                    h("h2", { text: P.t("register.heading") }),
                    h("a", { class: "btn primary wide", href: "/login", text: P.t("register.goToLogin") })
                ], "headphones");
                return;
            }

            var firstName = input("text", "first_name", "given-name");
            var lastName = input("text", "last_name", "family-name");
            var email = input("email", "email", "email");
            var password = input("password", "password", "new-password");
            var repeat = input("password", "password_repeat", "new-password");
            var invite = input("text", "invite_code", "off", new URL(window.location.href).searchParams.get("invite") || "");
            var consent = P.checkbox(P.t("register.consent"), false, null, { required: true, name: "consent" });
            var button = h("button", { type: "submit", class: "btn primary", text: P.t("register.submit") });

            var form = h("form", { class: "form-grid", on: { submit: function (event) {
                event.preventDefault();
                P.formError(form, "");
                if (password.value !== repeat.value) {
                    P.formError(form, P.t("field.passwordMismatch"));
                    return;
                }
                P.busy(button, P.api("POST", "open/users", {
                    first_name: firstName.value.trim(),
                    last_name: lastName.value.trim(),
                    email: email.value.trim(),
                    password: password.value,
                    password_repeat: repeat.value,
                    invite_code: invite.value.trim()
                })).then(function () {
                    P.mount(form,
                        h("p", { class: "notice ok", role: "status", text: P.t("register.done") }),
                        h("a", { class: "btn primary wide", href: "/login", text: P.t("register.goToLogin") }));
                }, function (err) {
                    password.value = "";
                    repeat.value = "";
                    P.formError(form, err.message);
                });
            } } },
                h("div", { class: "field-row" }, P.field(P.t("field.firstName"), firstName, null, true), P.field(P.t("field.lastName"), lastName, null, true)),
                P.field(P.t("field.email"), email, null, true),
                h("div", { class: "field-row" }, P.field(P.t("field.password"), password, null, true), P.field(P.t("field.repeatPassword"), repeat, null, true)),
                P.field(P.t("register.invite"), invite, P.t("register.inviteHelp"), true),
                consent.el,
                button);

            P.authLayout(main, P.t("register.title"), P.t("register.intro"), [
                h("h2", { text: P.t("register.heading") }),
                form,
                h("div", { class: "auth-links" }, h("a", { href: "/login", text: P.t("register.haveAccount") }))
            ], "headphones");
            firstName.focus();
        }
    });
})();
