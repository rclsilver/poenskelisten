// Checks the page width rules of web/css/app.css: the content area does not
// shrink on very wide screens, wish cards stay between 320 and 460 px wide
// from tablets up, the form, sign-in and account pages stay narrow, and the
// header menus stay on screen on phones.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { root } from "./harness.mjs";

const css = fs.readFileSync(path.join(root, "web/css/app.css"), "utf8");

function rule(selector) {
    const match = css.match(new RegExp("^" + selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + " \\{([^}]*)\\}", "m"));
    assert.ok(match, `missing ${selector}`);
    return match[1];
}

function px(body, name) {
    const match = body.match(new RegExp(name + ":\\s*(\\d+)px"));
    assert.ok(match, `missing ${name} in px`);
    return Number(match[1]);
}

const page = px(rule("body"), "--page");
const narrowPage = px(rule("body.narrow"), "--page");
const [, gutterMin, gutterVw, gutterMax] = rule("body").match(/--gutter:\s*clamp\((\d+)px,\s*(\d+)vw,\s*(\d+)px\)/).map(Number);
const cardMin = Number(css.match(/\.wishgrid \{ grid-template-columns: repeat\(auto-fill, minmax\((\d+)px, 1fr\)\); \}/)[1]);
const gap = px(rule(".wishgrid"), "gap");

// contentWidth mirrors .container above the 640 px breakpoint.
function contentWidth(viewport, max = page) {
    const gutter = Math.min(gutterMax, Math.max(gutterMin, viewport * gutterVw / 100));
    return Math.min(viewport - 2 * gutter, max);
}

function cards(width) {
    const columns = Math.max(1, Math.floor((width + gap) / (cardMin + gap)));
    return { columns, width: (width - gap * (columns - 1)) / columns };
}

test("the content area has bounded gutters and does not depend on percentages", () => {
    const container = rule(".container");
    assert.match(container, /max-width: calc\(var\(--page\) \+ 2 \* var\(--gutter\)\)/);
    assert.match(container, /padding: 40px var\(--gutter\) 64px/);
    assert.match(rule(".appnav"), /padding: 16px max\(var\(--gutter\), calc\(\(100% - var\(--page\)\) \/ 2\)\)/, "the header is not aligned with the content");
    for (const viewport of [1920, 2560, 3840, 5120]) {
        assert.equal(contentWidth(viewport), page, `${viewport} px`);
    }
});

test("wish cards use the width without getting narrow or too wide", () => {
    assert.ok(!/min-width: 1400px/.test(css), "a fixed column count for wide screens is back");
    for (const viewport of [1024, 1280, 1440, 1920, 2560, 3840, 5120]) {
        const { columns, width } = cards(contentWidth(viewport));
        assert.ok(width >= 320 && width <= 460, `${viewport} px: ${columns} columns of ${Math.round(width)} px`);
    }
    assert.equal(cards(contentWidth(5120)).columns, 4);
});

test("forms, sign-in and account pages stay narrow, card pages use the full width", () => {
    assert.ok(narrowPage <= 1200 && narrowPage < page);
    const body = (name) => fs.readFileSync(path.join(root, "web/html", name + ".html"), "utf8").match(/<body[^>]*>/)[0];
    for (const name of ["wishlistform", "wishform", "admin", "account", "login", "register", "verify", "enroll", "callback", "offline"]) {
        assert.match(body(name), /class="narrow"/, name);
    }
    for (const name of ["frontpage", "wishlists", "wishlist", "public", "groups", "group"]) {
        assert.doesNotMatch(body(name), /narrow/, name);
    }
    const shell = fs.readFileSync(path.join(root, "controllers/oauth_server.go"), "utf8");
    assert.match(shell, /<body class="narrow">/, "the OAuth pages built in Go");
    assert.match(css, /^\.container > \.auth-card \{ max-width: 640px; margin: 0 auto; \}$/m);
});

test("no page asks for a container class the stylesheet no longer has", () => {
    const files = [
        ...fs.readdirSync(path.join(root, "controllers")).filter((name) => name.endsWith(".go")).map((name) => "controllers/" + name),
        ...["html", "js", "templates"].flatMap((dir) => fs.readdirSync(path.join(root, "web", dir)).map((name) => `web/${dir}/${name}`))
    ];
    for (const file of files) {
        assert.doesNotMatch(fs.readFileSync(path.join(root, file), "utf8"), /container narrow/, file);
    }
});

test("on phones the open menus are anchored to the header, not to their button", () => {
    const start = css.indexOf("@media (max-width: 640px) {");
    const phone = css.slice(start, css.indexOf("\n}\n", start));
    assert.match(rule(".appnav"), /position: relative/);
    assert.match(phone, /\.menu \{ position: static; \}/);
    const panel = phone.match(/\.menu-panel \{([^}]*)\}/);
    assert.ok(panel, "no phone rule for .menu-panel");
    assert.match(panel[1], /top: auto/);
    assert.match(panel[1], /right: 16px/);
    assert.match(panel[1], /max-width: calc\(100vw - 32px\)/);
    assert.match(phone, /\.appnav \{ padding: 12px 16px 8px;/);
});
