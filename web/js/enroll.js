// Forced MFA enrolment (/enroll). Reached when the administrator enforces MFA
// and the signed-in (SSO) user hasn't enrolled yet, before they hold an
// access token; mfa.js talks to the SSO-authed /open MFA endpoints.
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
