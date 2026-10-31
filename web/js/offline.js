// Everything this page uses is precached by the service worker.
(function () {
    "use strict";
    var h = P.h;

    P.page({
        title: "offline.title",
        init: function (main) {
            P.mount(main, h("div", { class: "auth-layout" },
                h("section", { class: "auth-visual" },
                    h("span", { class: "eyebrow", text: P.appName() }),
                    h("h1", { text: P.t("offline.title"), tabIndex: -1 }),
                    h("p", { text: P.t("offline.intro") }),
                    h("div", { class: "row" }, P.button(P.t("offline.retry"), function () { window.location.reload(); }, "primary"))),
                h("section", null, h("img", { src: "/assets/illustrations/book.svg", alt: "", width: 640, height: 520, class: "offline-art" }))));
        }
    });
})();
