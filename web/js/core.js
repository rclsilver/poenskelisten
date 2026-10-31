// Library shared by the pages. Pages never inject markup: P.h sets user values
// with textContent. Only preferences go to localStorage, never a token. No
// template action here, so tests/web can load the file in Node.
(function (root) {
    "use strict";

    var P = {};
    var doc = root.document;

    var PREF_KEYS = {
        lang: "poenskelistenLang",
        theme: "poenskelistenTheme",
        sort: "poenskelistenWishSort"
    };

    P.prefs = {
        get: function (name) {
            try {
                return root.localStorage.getItem(PREF_KEYS[name]);
            } catch (e) {
                return null;
            }
        },
        set: function (name, value) {
            try {
                if (value === null || value === undefined) {
                    root.localStorage.removeItem(PREF_KEYS[name]);
                } else {
                    root.localStorage.setItem(PREF_KEYS[name], value);
                }
            } catch (e) {
                // Storage blocked: the choice only lasts for this page.
            }
        }
    };

    P.LANGS = ["en", "fr"];
    P.lang = "en";
    var dictionary = {};

    P.detectLang = function (saved, browserLangs) {
        if (P.LANGS.indexOf(saved) !== -1) {
            return saved;
        }
        var langs = browserLangs || [];
        for (var i = 0; i < langs.length; i++) {
            var primary = String(langs[i] || "").toLowerCase().split("-")[0];
            if (P.LANGS.indexOf(primary) !== -1) {
                return primary;
            }
        }
        return "en";
    };

    P.setDictionary = function (lang, dict) {
        P.lang = lang;
        dictionary = dict || {};
    };

    P.locale = function () {
        return P.lang === "fr" ? "fr-FR" : "en-GB";
    };

    function interpolate(text, vars) {
        return text.replace(/\{(\w+)\}/g, function (match, name) {
            if (vars && Object.prototype.hasOwnProperty.call(vars, name)) {
                return String(vars[name]);
            }
            if (name === "app") {
                return P.appName();
            }
            return match;
        });
    }

    // A missing key shows as is; main_test.go makes sure none ships.
    P.t = function (key, vars) {
        var text = Object.prototype.hasOwnProperty.call(dictionary, key) ? dictionary[key] : key;
        return interpolate(String(text), vars);
    };

    P.tn = function (key, n, vars) {
        var form = "other";
        try {
            form = new Intl.PluralRules(P.locale()).select(n) === "one" ? "one" : "other";
        } catch (e) {
            form = n === 1 ? "one" : "other";
        }
        var all = { n: n };
        for (var name in vars || {}) {
            all[name] = vars[name];
        }
        return P.t(key + "." + form, all);
    };

    // data-i18n-attr holds "attribute:key" pairs separated by ";".
    P.applyI18n = function (scope) {
        var base = scope || doc;
        base.querySelectorAll("[data-i18n]").forEach(function (el) {
            el.textContent = P.t(el.getAttribute("data-i18n"));
        });
        base.querySelectorAll("[data-i18n-attr]").forEach(function (el) {
            el.getAttribute("data-i18n-attr").split(";").forEach(function (pair) {
                var parts = pair.split(":");
                if (parts.length === 2) {
                    el.setAttribute(parts[0].trim(), P.t(parts[1].trim()));
                }
            });
        });
    };

    P.loadI18n = function () {
        var lang = P.detectLang(P.prefs.get("lang"), root.navigator ? root.navigator.languages || [root.navigator.language] : []);
        return root.fetch("/json/lang-" + lang + ".json", { credentials: "same-origin" })
            .then(function (response) {
                return response.ok ? response.json() : {};
            })
            .catch(function () {
                return {};
            })
            .then(function (dict) {
                P.setDictionary(lang, dict);
                doc.documentElement.lang = lang;
                P.applyI18n(doc);
            });
    };

    // The name is read back from the meta tag html/template escaped, never
    // embedded in the script.
    P.appName = function () {
        var meta = doc && doc.querySelector('meta[name="application-name"]');
        var name = meta ? meta.getAttribute("content") : "";
        return name && name.trim() ? name : "Pønskelisten";
    };

    P.setTitle = function (title) {
        doc.title = title ? title + " · " + P.appName() : P.appName();
    };

    var SVG_NS = "http://www.w3.org/2000/svg";

    function appendChildren(el, children) {
        for (var i = 0; i < children.length; i++) {
            var child = children[i];
            if (child === null || child === undefined || child === false || child === true) {
                continue;
            }
            if (Array.isArray(child)) {
                appendChildren(el, child);
            } else if (typeof child === "object" && child.nodeType) {
                el.appendChild(child);
            } else {
                el.appendChild(doc.createTextNode(String(child)));
            }
        }
    }

    // safeLink allows in-app paths, fragments and http(s) URLs only, so no
    // javascript: or data: link can be built by mistake.
    P.safeLink = function (value) {
        var text = String(value || "");
        if (/^(\/(?![\/\\])|#|\?)/.test(text)) {
            return text;
        }
        return P.safeExternalUrl(text) || "#";
    };

    P.safeImageSrc = function (value) {
        var text = String(value || "");
        if (/^\/(?![\/\\])/.test(text) || /^blob:/.test(text) || P.isImageDataUrl(text)) {
            return text;
        }
        return "";
    };

    // href and src go through safeLink and safeImageSrc; inline handlers and
    // markup are refused.
    P.h = function (tag, props) {
        var el = doc.createElement(tag);
        var options = props || {};
        for (var key in options) {
            if (!Object.prototype.hasOwnProperty.call(options, key)) {
                continue;
            }
            var value = options[key];
            if (value === undefined || value === null || value === false && key !== "checked" && key !== "disabled" && key !== "required" && key !== "hidden") {
                continue;
            }
            if (/^on/i.test(key) && key !== "on" || key === "innerHTML" || key === "outerHTML") {
                throw new Error("P.h: refusing " + key);
            }
            if (key === "class") {
                el.className = value;
            } else if (key === "text") {
                el.textContent = String(value);
            } else if (key === "on") {
                for (var event in value) {
                    el.addEventListener(event, value[event]);
                }
            } else if (key === "dataset") {
                for (var data in value) {
                    el.dataset[data] = value[data];
                }
            } else if (key === "attrs") {
                for (var attr in value) {
                    if (/^on/i.test(attr)) {
                        throw new Error("P.h: refusing " + attr);
                    }
                    if (value[attr] !== null && value[attr] !== undefined && value[attr] !== false) {
                        el.setAttribute(attr, value[attr] === true ? "" : String(value[attr]));
                    }
                }
            } else if (key === "href") {
                el.setAttribute("href", P.safeLink(value));
            } else if (key === "src") {
                var src = P.safeImageSrc(value);
                if (src) {
                    el.setAttribute("src", src);
                }
            } else if (key === "for" || key === "role" || /^aria-/.test(key)) {
                el.setAttribute(key, String(value));
            } else {
                el[key] = value;
            }
        }
        appendChildren(el, Array.prototype.slice.call(arguments, 2));
        return el;
    };

    P.clear = function (el) {
        while (el.firstChild) {
            el.removeChild(el.firstChild);
        }
        return el;
    };

    P.mount = function (el) {
        P.clear(el);
        appendChildren(el, Array.prototype.slice.call(arguments, 1));
        return el;
    };

    // Feather icons (MIT), as element specs so they need no markup parsing.
    var ICONS = {
        gift: [["polyline", { points: "20 12 20 22 4 22 4 12" }], ["rect", { x: 2, y: 7, width: 20, height: 5 }], ["line", { x1: 12, y1: 22, x2: 12, y2: 7 }], ["path", { d: "M12 7H7.5a2.5 2.5 0 0 1 0-5C11 2 12 7 12 7z" }], ["path", { d: "M12 7h4.5a2.5 2.5 0 0 0 0-5C13 2 12 7 12 7z" }]],
        plus: [["line", { x1: 12, y1: 5, x2: 12, y2: 19 }], ["line", { x1: 5, y1: 12, x2: 19, y2: 12 }]],
        edit: [["path", { d: "M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" }], ["path", { d: "M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" }]],
        trash: [["polyline", { points: "3 6 5 6 21 6" }], ["path", { d: "M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" }]],
        link: [["path", { d: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" }], ["path", { d: "M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" }]],
        external: [["path", { d: "M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" }], ["polyline", { points: "15 3 21 3 21 9" }], ["line", { x1: 10, y1: 14, x2: 21, y2: 3 }]],
        image: [["rect", { x: 3, y: 3, width: 18, height: 18, rx: 2, ry: 2 }], ["circle", { cx: 8.5, cy: 8.5, r: 1.5 }], ["polyline", { points: "21 15 16 10 5 21" }]],
        users: [["path", { d: "M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" }], ["circle", { cx: 9, cy: 7, r: 4 }], ["path", { d: "M23 21v-2a4 4 0 0 0-3-3.87" }], ["path", { d: "M16 3.13a4 4 0 0 1 0 7.75" }]],
        user: [["path", { d: "M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" }], ["circle", { cx: 12, cy: 7, r: 4 }]],
        userPlus: [["path", { d: "M16 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" }], ["circle", { cx: 8.5, cy: 7, r: 4 }], ["line", { x1: 20, y1: 8, x2: 20, y2: 14 }], ["line", { x1: 23, y1: 11, x2: 17, y2: 11 }]],
        lock: [["rect", { x: 3, y: 11, width: 18, height: 11, rx: 2, ry: 2 }], ["path", { d: "M7 11V7a5 5 0 0 1 10 0v4" }]],
        globe: [["circle", { cx: 12, cy: 12, r: 10 }], ["line", { x1: 2, y1: 12, x2: 22, y2: 12 }], ["path", { d: "M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" }]],
        calendar: [["rect", { x: 3, y: 4, width: 18, height: 18, rx: 2, ry: 2 }], ["line", { x1: 16, y1: 2, x2: 16, y2: 6 }], ["line", { x1: 8, y1: 2, x2: 8, y2: 6 }], ["line", { x1: 3, y1: 10, x2: 21, y2: 10 }]],
        check: [["polyline", { points: "20 6 9 17 4 12" }]],
        x: [["line", { x1: 18, y1: 6, x2: 6, y2: 18 }], ["line", { x1: 6, y1: 6, x2: 18, y2: 18 }]],
        chevronDown: [["polyline", { points: "6 9 12 15 18 9" }]],
        arrowLeft: [["line", { x1: 19, y1: 12, x2: 5, y2: 12 }], ["polyline", { points: "12 19 5 12 12 5" }]],
        share: [["circle", { cx: 18, cy: 5, r: 3 }], ["circle", { cx: 6, cy: 12, r: 3 }], ["circle", { cx: 18, cy: 19, r: 3 }], ["line", { x1: 8.59, y1: 13.51, x2: 15.42, y2: 17.49 }], ["line", { x1: 15.41, y1: 6.51, x2: 8.59, y2: 10.49 }]],
        copy: [["rect", { x: 9, y: 9, width: 13, height: 13, rx: 2, ry: 2 }], ["path", { d: "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" }]],
        logOut: [["path", { d: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" }], ["polyline", { points: "16 17 21 12 16 7" }], ["line", { x1: 21, y1: 12, x2: 9, y2: 12 }]],
        settings: [["circle", { cx: 12, cy: 12, r: 3 }], ["path", { d: "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" }]],
        list: [["line", { x1: 8, y1: 6, x2: 21, y2: 6 }], ["line", { x1: 8, y1: 12, x2: 21, y2: 12 }], ["line", { x1: 8, y1: 18, x2: 21, y2: 18 }], ["line", { x1: 3, y1: 6, x2: 3.01, y2: 6 }], ["line", { x1: 3, y1: 12, x2: 3.01, y2: 12 }], ["line", { x1: 3, y1: 18, x2: 3.01, y2: 18 }]]
    };

    P.icon = function (name, className) {
        var svg = doc.createElementNS(SVG_NS, "svg");
        svg.setAttribute("viewBox", "0 0 24 24");
        svg.setAttribute("fill", "none");
        svg.setAttribute("stroke", "currentColor");
        svg.setAttribute("stroke-width", "2");
        svg.setAttribute("stroke-linecap", "round");
        svg.setAttribute("stroke-linejoin", "round");
        svg.setAttribute("aria-hidden", "true");
        svg.setAttribute("focusable", "false");
        svg.setAttribute("class", className || "icon-svg");
        (ICONS[name] || []).forEach(function (spec) {
            var part = doc.createElementNS(SVG_NS, spec[0]);
            for (var attr in spec[1]) {
                part.setAttribute(attr, String(spec[1][attr]));
            }
            svg.appendChild(part);
        });
        return svg;
    };

    P.button = function (label, onClick, kind, iconName, extra) {
        var iconOnly = /(^|\s)icon(\s|$)/.test(kind || "");
        var props = { type: "button", class: "btn " + (kind || ""), on: { click: onClick } };
        if (iconOnly) {
            props["aria-label"] = label;
            props.title = label;
        }
        for (var key in extra || {}) {
            props[key] = extra[key];
        }
        return P.h("button", props, iconName ? P.icon(iconName) : null, iconOnly ? null : label);
    };

    // safeExternalUrl accepts http(s) URLs only. A bare "shop.example/item" is
    // read as https, the same way the server validates wish links.
    P.safeExternalUrl = function (raw) {
        var text = String(raw || "").trim();
        if (!text) {
            return null;
        }
        var url = null;
        try {
            url = new URL(text);
        } catch (e) {
            try {
                url = new URL("https://" + text);
            } catch (e2) {
                return null;
            }
        }
        if ((url.protocol !== "http:" && url.protocol !== "https:") || !url.hostname || url.username || url.password) {
            return null;
        }
        return url.href;
    };

    // safeLocalPath keeps a post-login destination on this site: a path
    // starting with one slash, never "//host" or "/\host", nor a scheme.
    P.safeLocalPath = function (raw) {
        var text = String(raw || "");
        if (/^\/(?![\/\\])/.test(text) && !/[\u0000-\u001f\u007f]/.test(text)) {
            return text;
        }
        return "/";
    };

    // noreferrer also keeps the list's URL out of the shop's logs.
    P.externalLink = function (raw, label, className) {
        var url = P.safeExternalUrl(raw);
        if (!url) {
            return null;
        }
        return P.h("a", {
            href: url,
            target: "_blank",
            rel: "noopener noreferrer",
            class: className || "btn small",
            attrs: { referrerpolicy: "no-referrer" }
        }, P.icon("external"), label);
    };

    P.parseDate = function (value) {
        if (!value) {
            return null;
        }
        var date = value instanceof Date ? value : new Date(value);
        return isNaN(date.getTime()) ? null : date;
    };

    P.fmtDate = function (value, options) {
        var date = P.parseDate(value);
        if (!date) {
            return "";
        }
        try {
            return new Intl.DateTimeFormat(P.locale(), options || { day: "numeric", month: "long", year: "numeric" }).format(date);
        } catch (e) {
            return date.toDateString();
        }
    };

    // Dates the API stores as calendar days (wishlist expiry, news) travel as
    // midnight UTC, so they are read and written in UTC to stay on the same
    // day in every timezone.
    P.fmtDay = function (value) {
        return P.fmtDate(value, { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
    };

    P.dayInputValue = function (value) {
        var date = P.parseDate(value);
        return date ? date.toISOString().slice(0, 10) : "";
    };

    P.dayToApi = function (day) {
        if (!/^\d{4}-\d{2}-\d{2}$/.test(day || "")) {
            return null;
        }
        var date = new Date(day + "T00:00:00.000Z");
        return isNaN(date.getTime()) ? null : date.toISOString();
    };

    P.todayInputValue = function () {
        var now = new Date();
        return [now.getFullYear(), String(now.getMonth() + 1).padStart(2, "0"), String(now.getDate()).padStart(2, "0")].join("-");
    };

    P.fmtPrice = function (price, currency) {
        if (price === null || price === undefined || Number(price) === 0 || isNaN(Number(price))) {
            return "";
        }
        var value = Number(price);
        var number;
        try {
            number = new Intl.NumberFormat(P.locale(), {
                minimumFractionDigits: value % 1 === 0 ? 0 : 2,
                maximumFractionDigits: 2
            }).format(value);
        } catch (e) {
            number = String(value);
        }
        var cur = currency || {};
        var symbol = cur.currency || "";
        if (!symbol) {
            return number;
        }
        var pad = cur.padding ? " " : "";
        return cur.left ? symbol + pad + number : number + pad + symbol;
    };

    P.displayName = function (user) {
        if (!user) {
            return P.t("user.unknown");
        }
        var name = ((user.first_name || "") + " " + (user.last_name || "")).trim();
        return name || P.t("user.unknown");
    };

    P.initials = function (user) {
        var first = user && user.first_name ? user.first_name.trim().charAt(0) : "";
        var last = user && user.last_name ? user.last_name.trim().charAt(0) : "";
        return (first + last).toUpperCase() || "?";
    };

    function ApiError(message, status) {
        this.name = "ApiError";
        this.message = message;
        this.status = status;
    }
    ApiError.prototype = Object.create(Error.prototype);
    P.ApiError = ApiError;

    P.token = "";

    // A 401 means the token expired between two refreshes: retry once with a
    // fresh one.
    P.api = function (method, path, body, options) {
        var opts = options || {};
        var headers = { "Accept": "application/json" };
        if (body !== undefined && body !== null) {
            headers["Content-Type"] = "application/json;charset=UTF-8";
        }
        if (P.token && opts.auth !== false) {
            headers["Authorization"] = "Bearer " + P.token;
        }
        return root.fetch("/api/" + path, {
            method: method,
            headers: headers,
            body: body !== undefined && body !== null ? JSON.stringify(body) : undefined,
            credentials: "same-origin",
            cache: "no-store"
        }).then(function (response) {
            return response.text().then(function (text) {
                var data = {};
                try {
                    data = text ? JSON.parse(text) : {};
                } catch (e) {
                    data = {};
                }
                if (response.status === 401 && P.token && !opts.retried && typeof P.onUnauthorized === "function") {
                    return P.onUnauthorized().then(function () {
                        opts.retried = true;
                        return P.api(method, path, body, opts);
                    });
                }
                if (!response.ok || data.error) {
                    throw new ApiError(data.error || P.t("error.server"), response.status);
                }
                return data;
            });
        }, function () {
            throw new ApiError(P.t("error.network"), 0);
        });
    };

    P.MAX_IMAGE_BYTES = 10000000;
    P.IMAGE_TYPES = ["image/jpeg", "image/png"];

    P.isImageDataUrl = function (value) {
        return typeof value === "string" && /^data:image\/(jpeg|png|svg\+xml);base64,[A-Za-z0-9+\/]+=*$/.test(value);
    };

    // A quick answer only: the server checks the image again and re-encodes it.
    P.checkImageFile = function (file) {
        if (!file) {
            return null;
        }
        if (P.IMAGE_TYPES.indexOf(file.type) === -1) {
            return "image.badType";
        }
        if (file.size > P.MAX_IMAGE_BYTES) {
            return "image.tooBig";
        }
        return null;
    };

    P.readImageFile = function (file) {
        var problem = P.checkImageFile(file);
        if (problem) {
            return Promise.reject(new Error(P.t(problem)));
        }
        return new Promise(function (resolve, reject) {
            var reader = new root.FileReader();
            reader.onload = function () {
                var url = String(reader.result || "");
                if (!/^data:image\/(jpeg|png);base64,/.test(url)) {
                    reject(new Error(P.t("image.badType")));
                    return;
                }
                var probe = new root.Image();
                probe.onload = function () { resolve(url); };
                probe.onerror = function () { reject(new Error(P.t("image.unreadable"))); };
                probe.src = url;
            };
            reader.onerror = function () { reject(new Error(P.t("image.unreadable"))); };
            reader.readAsDataURL(file);
        });
    };

    var imageCache = {};

    P.wishImage = function (wishID, thumbnail) {
        var key = wishID + (thumbnail ? ":t" : ":f");
        if (!imageCache[key]) {
            imageCache[key] = P.api("GET", "both/wishes/" + encodeURIComponent(wishID) + "/image" + (thumbnail ? "?thumbnail=true" : ""))
                .then(function (data) {
                    return P.isImageDataUrl(data.image) ? data.image : null;
                })
                .catch(function () {
                    delete imageCache[key];
                    return null;
                });
        }
        return imageCache[key];
    };

    P.forgetWishImage = function (wishID) {
        delete imageCache[wishID + ":t"];
        delete imageCache[wishID + ":f"];
    };

    // The same people show up many times on a page (members, owners).
    var profileCache = {};
    P.profileImage = function (userID, thumbnail) {
        var key = userID + (thumbnail ? ":t" : ":f");
        if (!profileCache[key]) {
            profileCache[key] = P.api("GET", "auth/users/" + encodeURIComponent(userID) + "/image" + (thumbnail ? "?thumbnail=true" : ""))
                .then(function (data) {
                    return !data.default && P.isImageDataUrl(data.image) ? data.image : null;
                })
                .catch(function () {
                    return null;
                });
        }
        return profileCache[key];
    };

    P.lazyImage = function (holder, loader, alt) {
        var start = function () {
            loader().then(function (src) {
                if (!src) {
                    return;
                }
                P.mount(holder, P.h("img", { src: src, alt: alt || "", loading: "lazy", decoding: "async" }));
            });
        };
        if (root.IntersectionObserver) {
            var observer = new root.IntersectionObserver(function (entries) {
                if (entries.some(function (entry) { return entry.isIntersecting; })) {
                    observer.disconnect();
                    start();
                }
            }, { rootMargin: "200px" });
            observer.observe(holder);
        } else {
            start();
        }
        return holder;
    };

    P.avatar = function (user, size) {
        var holder = P.h("span", { class: "avatar" + (size ? " " + size : ""), title: P.displayName(user), attrs: { "aria-hidden": "true" } }, P.initials(user));
        if (user && user.id) {
            P.profileImage(user.id, size !== "large").then(function (src) {
                if (src) {
                    P.mount(holder, P.h("img", { src: src, alt: "" }));
                }
            });
        }
        return holder;
    };

    function toastHost() {
        var host = doc.getElementById("toasts");
        if (!host) {
            host = P.h("div", { id: "toasts", class: "toasts" });
            doc.body.appendChild(host);
        }
        return host;
    }

    P.toast = function (message, kind) {
        var isError = kind === "error";
        var item = P.h("div", { class: "toast" + (isError ? " error" : ""), role: isError ? "alert" : "status" });
        var close = function () {
            if (item.parentNode) {
                item.parentNode.removeChild(item);
            }
        };
        P.mount(item, P.h("span", { text: message }), P.button(P.t("action.close"), close, "icon quiet", "x"));
        toastHost().appendChild(item);
        root.setTimeout(close, isError ? 9000 : 5000);
        return item;
    };

    P.error = function (err) {
        P.toast(err && err.message ? err.message : P.t("error.server"), "error");
    };

    // An action's onClick may return a promise; resolving to false keeps the
    // dialog open.
    P.dialog = function (options) {
        var previous = doc.activeElement;
        var titleID = "dialog-title-" + Math.random().toString(36).slice(2);
        var dialog = P.h("dialog", { class: options.className || "", "aria-labelledby": titleID });
        var close = function (result) {
            if (dialog.open) {
                dialog.close(result || "");
            }
        };
        dialog.addEventListener("close", function () {
            if (dialog.parentNode) {
                dialog.parentNode.removeChild(dialog);
            }
            if (previous && previous.focus) {
                previous.focus();
            }
            if (options.onClose) {
                options.onClose(dialog.returnValue);
            }
        });
        dialog.addEventListener("click", function (event) {
            if (event.target === dialog && options.dismissable !== false) {
                close();
            }
        });
        var actions = (options.actions || []).map(function (action) {
            var button = P.button(action.label, function () {
                var outcome = action.onClick ? action.onClick() : undefined;
                if (outcome && typeof outcome.then === "function") {
                    button.disabled = true;
                    outcome.then(function (keep) {
                        button.disabled = false;
                        if (keep !== false) {
                            close(action.value);
                        }
                    }, function (err) {
                        button.disabled = false;
                        P.error(err);
                    });
                } else if (outcome !== false) {
                    close(action.value);
                }
            }, action.kind || "");
            return button;
        });
        P.mount(dialog,
            P.h("div", { class: "dialog-head" },
                P.h("h2", { id: titleID, text: options.title }),
                P.button(P.t("action.close"), function () { close(); }, "icon quiet", "x")),
            options.content,
            actions.length ? P.h("div", { class: "dialog-actions" }, actions) : null);
        doc.body.appendChild(dialog);
        dialog.showModal();
        var focusTarget = dialog.querySelector("[autofocus]") || dialog.querySelector("input, select, textarea") || (actions.length ? actions[actions.length - 1] : null);
        if (focusTarget) {
            focusTarget.focus();
        }
        return { el: dialog, close: close };
    };

    P.confirm = function (options) {
        return new Promise(function (resolve) {
            var confirmed = false;
            P.dialog({
                title: options.title,
                content: options.message ? P.h("p", { text: options.message }) : null,
                actions: [
                    { label: P.t("action.cancel"), kind: "quiet" },
                    { label: options.confirm || P.t("action.confirm"), kind: options.danger ? "danger" : "primary", onClick: function () { confirmed = true; } }
                ],
                onClose: function () { resolve(confirmed); }
            });
        });
    };

    P.lightbox = function (loader, alt) {
        var body = P.h("div", { class: "loading" }, P.h("div", { class: "spinner", role: "status", "aria-label": P.t("state.loading") }));
        P.dialog({ title: alt, content: body, className: "lightbox" });
        loader().then(function (src) {
            if (src) {
                P.mount(body, P.h("img", { src: src, alt: alt }));
                body.className = "";
            } else {
                P.mount(body, P.h("p", { class: "muted", text: P.t("image.unavailable") }));
            }
        });
    };

    P.busy = function (button, promise) {
        if (button) {
            button.disabled = true;
        }
        var done = function () {
            if (button) {
                button.disabled = false;
            }
        };
        promise.then(done, done);
        return promise;
    };

    P.loading = function () {
        return P.h("div", { class: "loading" }, P.h("div", { class: "spinner", role: "status", "aria-label": P.t("state.loading") }));
    };

    P.emptyState = function (message, action) {
        return P.h("div", { class: "empty" }, P.h("p", { text: message }), action || null);
    };

    P.copyText = function (text, input) {
        var done = function () { P.toast(P.t("share.copied")); };
        if (root.navigator && root.navigator.clipboard && root.navigator.clipboard.writeText) {
            return root.navigator.clipboard.writeText(text).then(done, function () {
                if (input) {
                    input.select();
                }
            });
        }
        if (input) {
            input.select();
        }
        return Promise.resolve();
    };

    P.wishDateValue = function (wish) {
        var value = Date.parse(wish.updated_at);
        return isNaN(value) ? 0 : value;
    };

    // A missing and a zero price both mean "no price"; those wishes always go
    // last, whatever the direction.
    P.wishPriceValue = function (wish) {
        if (wish.price === null || wish.price === undefined || Number(wish.price) === 0) {
            return null;
        }
        return Number(wish.price);
    };

    P.sortWishes = function (list, sort) {
        var sorted = list.slice();
        var price = function (a, b, direction) {
            var pa = P.wishPriceValue(a);
            var pb = P.wishPriceValue(b);
            if (pa === null && pb === null) return 0;
            if (pa === null) return 1;
            if (pb === null) return -1;
            return (pa - pb) * direction;
        };
        sorted.sort(function (a, b) {
            switch (sort) {
                case "date_desc": return P.wishDateValue(b) - P.wishDateValue(a);
                case "date_asc": return P.wishDateValue(a) - P.wishDateValue(b);
                case "price_asc": return price(a, b, 1);
                case "price_desc": return price(a, b, -1);
                default: return 0;
            }
        });
        return sorted;
    };

    P.SORTS = ["date_desc", "date_asc", "price_asc", "price_desc"];

    // Sort preferences keep the format the previous interface stored:
    // {sort, layout?}, layout only once the user picked one explicitly.
    P.loadSortPrefs = function () {
        var prefs = { sort: "date_asc", layout: "grouped", explicit: false };
        try {
            var saved = JSON.parse(P.prefs.get("sort") || "{}");
            if (P.SORTS.indexOf(saved.sort) !== -1) {
                prefs.sort = saved.sort;
            }
            if (saved.layout === "grouped" || saved.layout === "flat") {
                prefs.layout = saved.layout;
                prefs.explicit = true;
            }
        } catch (e) {
            // Unreadable preferences: keep the defaults.
        }
        return prefs;
    };

    P.saveSortPrefs = function (prefs) {
        var saved = { sort: prefs.sort };
        if (prefs.explicit) {
            saved.layout = prefs.layout;
        }
        P.prefs.set("sort", JSON.stringify(saved));
    };

    P.hasCategories = function (list) {
        return list.some(function (wish) { return !!wish.category; });
    };

    // Without categories there is nothing to group by, whatever was saved.
    P.effectiveLayout = function (list, prefs) {
        if (!P.hasCategories(list)) {
            return "flat";
        }
        return prefs.explicit ? prefs.layout : "grouped";
    };

    P.bucketWishes = function (list) {
        var byID = {};
        var order = [];
        var uncategorized = [];
        list.forEach(function (wish) {
            if (wish.category) {
                var id = wish.category.id;
                if (!byID[id]) {
                    byID[id] = { id: id, name: wish.category.name, sortOrder: wish.category.sort_order || 0, wishes: [] };
                    order.push(id);
                }
                byID[id].wishes.push(wish);
            } else {
                uncategorized.push(wish);
            }
        });
        order.sort(function (a, b) {
            return (byID[a].sortOrder - byID[b].sortOrder) || String(byID[a].name).localeCompare(String(byID[b].name));
        });
        return { categories: order.map(function (id) { return byID[id]; }), uncategorized: uncategorized };
    };

    P.isCollaborator = function (wishlist, userID) {
        return (wishlist.collaborators || []).some(function (c) {
            return c.user && c.user.id === userID && c.enabled !== false;
        });
    };

    P.wishlistRole = function (wishlist, userID) {
        if (wishlist.owner && wishlist.owner.id === userID) {
            return "owner";
        }
        if (P.isCollaborator(wishlist, userID)) {
            return "collaborator";
        }
        return "group_member";
    };

    P.isExpired = function (wishlist, now) {
        var date = P.parseDate(wishlist.date);
        return !!(wishlist.expires && date && date.getTime() < (now || new Date()).getTime());
    };

    P.eligibleWishlists = function (lists, userID, now) {
        return (lists || []).filter(function (wishlist) {
            var role = P.wishlistRole(wishlist, userID);
            return (role === "owner" || role === "collaborator") && !P.isExpired(wishlist, now);
        });
    };

    // wishDestination decides where a new wish goes:
    //   - "none": no eligible list;
    //   - "fixed": one list, chosen for the user (from a list page, or the
    //     only eligible one); canChange tells whether "Change list" applies;
    //   - "pick": several lists from the home page, nothing preselected.
    P.wishDestination = function (origin, eligible, currentID) {
        var ids = (eligible || []).map(function (wishlist) { return wishlist.id; });
        if (ids.length === 0) {
            return { mode: "none", listID: "", canChange: false };
        }
        if (origin === "list" && ids.indexOf(currentID) !== -1) {
            return { mode: "fixed", listID: currentID, canChange: ids.length > 1 };
        }
        if (ids.length === 1) {
            return { mode: "fixed", listID: ids[0], canChange: false };
        }
        return { mode: "pick", listID: "", canChange: false };
    };

    // claimState describes a wish for the current viewer: "free", "mine"
    // (claimed by the viewer) or "other". The API strips claims for the list
    // owner and collaborators, so they always see "free".
    P.claimState = function (wish, userID) {
        var claims = (wish.wishclaim || []).filter(function (claim) { return claim.enabled !== false; });
        if (claims.length === 0) {
            return { state: "free", claimer: null };
        }
        var mine = claims.some(function (claim) { return claim.user && claim.user.id === userID; });
        return { state: mine ? "mine" : "other", claimer: mine ? null : claims[0].user };
    };

    P.canManageWish = function (wish, userID) {
        if (!userID) {
            return false;
        }
        if (wish.wishlist_owner && wish.wishlist_owner.id === userID) {
            return true;
        }
        if (wish.owner_id && wish.owner_id.id === userID) {
            return true;
        }
        return (wish.collaborators || []).some(function (c) { return c.user && c.user.id === userID; });
    };

    P.publicWishlistUrl = function (baseURL, hash) {
        var base = String(baseURL || "").replace(/\/+$/, "") || root.location.origin;
        return base + "/wishlists/public/" + encodeURIComponent(hash);
    };

    P.user = null;

    P.setTheme = function (theme) {
        var value = theme === "light" || theme === "dark" ? theme : null;
        P.prefs.set("theme", value);
        if (value) {
            doc.documentElement.setAttribute("data-theme", value);
        } else {
            doc.documentElement.removeAttribute("data-theme");
        }
    };

    P.setLang = function (lang) {
        if (P.LANGS.indexOf(lang) === -1 || lang === P.lang) {
            return;
        }
        P.prefs.set("lang", lang);
        // Everything on screen was rendered in the old language.
        root.location.reload();
    };

    function wirePrefs() {
        var themeSelect = doc.getElementById("pref-theme");
        var langSelect = doc.getElementById("pref-lang");
        if (themeSelect) {
            var saved = P.prefs.get("theme");
            themeSelect.value = saved === "light" || saved === "dark" ? saved : "system";
            themeSelect.addEventListener("change", function () { P.setTheme(themeSelect.value); });
        }
        if (langSelect) {
            langSelect.value = P.lang;
            langSelect.addEventListener("change", function () { P.setLang(langSelect.value); });
        }
        // Escape gives the focus back to the menu button.
        doc.addEventListener("click", function (event) {
            doc.querySelectorAll("details.menu").forEach(function (menu) {
                if (menu.open && !menu.contains(event.target)) {
                    menu.open = false;
                }
            });
        });
        doc.addEventListener("keydown", function (event) {
            if (event.key !== "Escape") {
                return;
            }
            doc.querySelectorAll("details.menu").forEach(function (menu) {
                if (menu.open) {
                    menu.open = false;
                    var summary = menu.querySelector("summary");
                    if (summary) {
                        summary.focus();
                    }
                }
            });
        });
    }

    function navLink(href, key, page) {
        var current = doc.body.getAttribute("data-nav") === page;
        return P.h("a", { href: href, class: "btn quiet", "aria-current": current ? "page" : null, text: P.t(key) });
    }

    P.renderNav = function (user, options) {
        var links = doc.getElementById("nav-links");
        var account = doc.getElementById("nav-account");
        if (!links || !account) {
            return;
        }
        if (user) {
            P.mount(links,
                navLink("/", "nav.home", "home"),
                navLink("/wishlists", "nav.lists", "wishlists"),
                navLink("/groups", "nav.groups", "groups"),
                user.admin ? navLink("/admin", "nav.admin", "admin") : null);
            links.hidden = false;
            var menu = P.h("details", { class: "menu" });
            P.mount(menu,
                P.h("summary", { class: "btn quiet", "aria-label": P.t("nav.accountMenu") }, P.avatar(user), P.h("span", { class: "nav-name", text: user.first_name || P.displayName(user) })),
                P.h("div", { class: "menu-panel" },
                    P.h("div", { class: "menu-head" }, P.h("strong", { text: P.displayName(user) })),
                    P.h("a", { href: "/account", class: "btn quiet" }, P.icon("user"), P.t("nav.account")),
                    P.button(P.t("nav.logout"), function () { if (typeof root.logout === "function") { root.logout(); } }, "quiet", "logOut")));
            P.mount(account, menu);
        } else {
            links.hidden = true;
            var path = root.location.pathname;
            P.mount(account,
                path !== "/login" ? P.h("a", { href: "/login", class: "btn quiet", text: P.t("nav.login") }) : null,
                options && options.localLogin && path !== "/register" ? P.h("a", { href: "/register", class: "btn", text: P.t("nav.register") }) : null);
        }
    };

    P.page = function (options) {
        var start = function () {
            P.loadI18n().then(function () {
                wirePrefs();
                var main = doc.getElementById("main");
                if (options.title) {
                    P.setTitle(P.t(options.title));
                }
                var session = options.session || "none";
                if (session === "none") {
                    P.renderNav(null, { localLogin: root.LOCAL_LOGIN_ENABLED });
                    options.init(main, null);
                    return;
                }
                root.startSession(session === "required").then(function (user) {
                    P.user = user;
                    P.renderNav(user, { localLogin: root.LOCAL_LOGIN_ENABLED });
                    if (user || session === "optional") {
                        options.init(main, user);
                    }
                }, function (err) {
                    P.mount(main, P.emptyState(err && err.message ? err.message : P.t("error.server")));
                });
            });
        };
        if (doc.readyState === "loading") {
            doc.addEventListener("DOMContentLoaded", start);
        } else {
            start();
        }
    };

    P.pageHeader = function (eyebrow, title, intro, actions) {
        return P.h("div", { class: "page-top" },
            P.h("div", { class: "intro" },
                eyebrow ? P.h("span", { class: "eyebrow", text: eyebrow }) : null,
                P.h("h1", { text: title, tabIndex: -1 }),
                intro ? P.h("p", { text: intro }) : null),
            actions ? P.h("div", { class: "row" }, actions) : null);
    };

    P.authLayout = function (main, title, intro, card, art) {
        P.mount(main, P.h("div", { class: "auth-layout" },
            P.h("section", { class: "auth-visual" },
                P.h("span", { class: "eyebrow", text: P.appName() }),
                P.h("h1", { text: title, tabIndex: -1 }),
                intro ? P.h("p", { text: intro }) : null,
                P.h("img", { src: "/assets/illustrations/" + (art || "cup") + ".svg", alt: "", width: 640, height: 520 })),
            P.h("section", { class: "auth-card" }, card)));
    };

    P.backLink = function (href, label) {
        return P.h("a", { href: href, class: "btn quiet back" }, P.icon("arrowLeft"), label);
    };

    P.field = function (label, control, help, required) {
        var id = control.id || "f-" + Math.random().toString(36).slice(2);
        control.id = id;
        var helpEl = null;
        if (help) {
            helpEl = P.h("p", { class: "help", id: id + "-help", text: help });
            control.setAttribute("aria-describedby", id + "-help");
        }
        return P.h("div", { class: "field" },
            P.h("label", { for: id }, label, required ? P.h("span", { class: "req", "aria-hidden": "true", text: "*" }) : null),
            control,
            helpEl);
    };

    P.checkbox = function (label, checked, help, props) {
        var options = { type: "checkbox", checked: !!checked };
        for (var key in props || {}) {
            options[key] = props[key];
        }
        var input = P.h("input", options);
        return {
            input: input,
            el: P.h("label", { class: "check" }, input, P.h("span", null, P.h("span", { text: label }), help ? P.h("span", { class: "help", text: help }) : null))
        };
    };

    P.formError = function (form, message) {
        var region = form.querySelector(".form-error");
        if (!region) {
            region = P.h("p", { class: "notice danger form-error", role: "alert" });
            form.insertBefore(region, form.firstChild);
        }
        region.textContent = message || "";
        region.hidden = !message;
        if (message) {
            region.scrollIntoView({ block: "nearest" });
        }
    };

    if (typeof module !== "undefined" && module.exports) {
        module.exports = P;
    }
    root.P = P;
})(typeof window !== "undefined" ? window : globalThis);
