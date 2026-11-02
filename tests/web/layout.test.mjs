// Checks the page width rules of web/css/app.css: the content area does not
// shrink on very wide screens, wish cards stay between 320 and 460 px wide
// from tablets up, and the form, sign-in and account pages stay narrow.
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
    for (const selector of [".appnav", ".footer"]) {
        assert.match(rule(selector), /padding: \d+px max\(var\(--gutter\), calc\(\(100% - var\(--page\)\) \/ 2\)\)/, `${selector} is not aligned with the content`);
    }
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
});
