// Shared by the account page and /enroll. The /open MFA endpoints accept the
// SSO session as well as the access token.
(function () {
    "use strict";
    var h = P.h;

    function recoveryCodes(codes, onDone) {
        return h("div", { class: "stack" },
            h("p", { class: "notice warn" }, h("strong", { text: P.t("mfa.saveCodesTitle") }), " ", P.t("mfa.saveCodesHelp")),
            h("div", { class: "codes", attrs: { "aria-label": P.t("mfa.codesLabel") }, role: "group" },
                codes.map(function (code) { return h("span", { text: code }); })),
            h("div", { class: "row" },
                P.button(P.t("mfa.copyCodes"), function () { P.copyText(codes.join("\n")); }, "", "copy"),
                P.button(P.t("mfa.codesSaved"), onDone, "primary", "check")));
    }

    P.mfaEnrol = function (container, onDone, onCancel) {
        P.mount(container, P.loading());
        P.api("POST", "open/users/mfa/enroll").then(function (result) {
            var code = h("input", { type: "text", name: "code", required: true, attrs: { autocomplete: "one-time-code", inputmode: "numeric", pattern: "[0-9 ]*", maxlength: 8 } });
            var submit = h("button", { type: "submit", class: "btn primary", text: P.t("mfa.enable") });
            var form = h("form", { class: "form-grid", on: { submit: function (event) {
                event.preventDefault();
                P.formError(form, "");
                P.busy(submit, P.api("POST", "open/users/mfa/activate", { code: code.value.replace(/\s+/g, "") })).then(function (activated) {
                    if (activated.recovery_codes && activated.recovery_codes.length) {
                        P.mount(container, recoveryCodes(activated.recovery_codes, onDone));
                    } else {
                        onDone();
                    }
                }, function (err) {
                    code.value = "";
                    P.formError(form, err.message);
                });
            } } },
                P.field(P.t("mfa.confirmCode"), code, P.t("mfa.confirmHelp")),
                h("div", { class: "row" }, submit, onCancel ? P.button(P.t("action.cancel"), onCancel, "quiet") : null));

            var otpLink = /^otpauth:\/\//.test(result.otpauth_url || "") ? h("a", { attrs: { href: result.otpauth_url }, text: P.t("mfa.openApp") }) : null;
            P.mount(container, h("div", { class: "stack" },
                h("p", { text: P.t("mfa.scan") }),
                P.isImageDataUrl(result.qr_code) ? h("img", { class: "qr", src: result.qr_code, alt: P.t("mfa.qrAlt"), width: 200, height: 200 }) : null,
                h("div", { class: "stack tight" },
                    h("p", { class: "help", text: P.t("mfa.manual") }),
                    h("code", { class: "mono", text: result.secret || "" }),
                    otpLink),
                form));
            code.focus();
        }, function (err) {
            P.mount(container, h("p", { class: "notice danger", role: "alert", text: err.message }));
            if (err.status === 401) {
                window.setTimeout(function () { window.location.href = "/login"; }, 1500);
            }
        });
    };
})();
