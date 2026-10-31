// The /oauth/callback page: exchange the authorization code for tokens (via
// handleOAuthCallback in session.js), then return to where the user started.
(function () {
    "use strict";

    P.page({
        title: "callback.title",
        init: function (main) {
            P.mount(main, P.h("div", { class: "loading stack" },
                P.h("div", { class: "spinner", role: "status", "aria-label": P.t("callback.title") }),
                P.h("p", { class: "muted", text: P.t("callback.title") })));
            handleOAuthCallback();
        }
    });
})();
