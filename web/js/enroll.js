// Reached before the user holds an access token: mfa.js uses the SSO-authed
// /open MFA endpoints.
(function () {
    "use strict";
    var h = P.h;

    P.page({
        title: "enroll.heading",
        init: function (main) {
            var steps = h("div");
            P.authLayout(main, P.t("enroll.title"), P.t("enroll.intro"), [h("h2", { text: P.t("enroll.heading") }), steps], "lamp");
            P.mfaEnrol(steps, function () {
                window.location.href = "/";
            });
        }
    });
})();
