// Applies the saved theme before the first paint. Loaded synchronously from
// <head>, so it must stay tiny and must not touch the body. With no saved
// choice nothing is set and the stylesheet follows the system theme.
(function () {
    "use strict";
    try {
        var theme = window.localStorage.getItem("poenskelistenTheme");
        if (theme === "light" || theme === "dark") {
            document.documentElement.setAttribute("data-theme", theme);
        }
    } catch (e) {
        // Storage can be blocked (private mode, policies): keep the system theme.
    }
})();
