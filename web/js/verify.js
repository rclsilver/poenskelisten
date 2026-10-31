// Both calls ride on the SSO session cookie: there is no access token until
// the account is verified.
(function () {
    "use strict";
    var h = P.h;

    P.page({
        title: "verify.heading",
        init: function (main) {
            var code = h("input", { type: "text", name: "code", required: true, attrs: { autocomplete: "one-time-code", spellcheck: "false" } });
            var button = h("button", { type: "submit", class: "btn primary", text: P.t("verify.submit") });
            var form = h("form", { class: "form-grid", on: { submit: function (event) {
                event.preventDefault();
                P.formError(form, "");
                P.busy(button, P.api("POST", "open/users/verify/" + encodeURIComponent(code.value.trim()))).then(function () {
                    window.location.href = "/";
                }, function (err) {
                    P.formError(form, err.message);
                });
            } } }, P.field(P.t("verify.code"), code, P.t("verify.codeHelp")), button);

            var resend = P.button(P.t("verify.resend"), function () {
                P.busy(resend, P.api("POST", "open/users/verification")).then(function (result) {
                    P.toast(result.message || P.t("verify.sent"));
                }, P.error);
            }, "quiet");

            P.authLayout(main, P.t("verify.title"), P.t("verify.intro"), [
                h("h2", { text: P.t("verify.heading") }),
                form,
                h("div", { class: "auth-links" }, resend, h("a", { href: "/login", text: P.t("login.back") }))
            ], "lamp");
            code.focus();
        }
    });
})();
