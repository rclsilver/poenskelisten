// Loaded synchronously from <head> so the saved theme applies before the
// first paint.
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
