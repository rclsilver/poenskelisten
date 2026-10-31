// Checks the colour tokens of web/css/app.css: every text colour reaches WCAG
// AA (4.5:1) on the backgrounds it is used on, in both themes, and the dark
// theme is the same whether it comes from the system or from the user.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

const css = fs.readFileSync(path.join(root, "web/css/app.css"), "utf8");

function block(selector) {
    const start = css.indexOf(selector + " {");
    assert.notEqual(start, -1, `missing ${selector}`);
    const body = css.slice(css.indexOf("{", start) + 1, css.indexOf("}", start));
    const tokens = {};
    for (const match of body.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)) {
        tokens[match[1]] = match[2].toLowerCase();
    }
    return tokens;
}

function luminance(hex) {
    const channel = (i) => {
        const value = parseInt(hex.slice(i, i + 2), 16) / 255;
        return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
    };
    return 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
}

function contrast(a, b) {
    const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x);
    return (high + 0.05) / (low + 0.05);
}

const light = block(":root");
const darkSystem = block(':root:not([data-theme="light"])');
const dark = block(':root[data-theme="dark"]');

// [text, background] pairs the stylesheet actually uses.
const textPairs = [
    ["text", "bg"], ["text", "surface"], ["text", "surface2"],
    ["muted", "bg"], ["muted", "surface"], ["muted", "surface2"],
    ["onaccent", "accent"], ["bg", "text"],
    ["positive", "positivebg"], ["reserved", "reservedbg"],
    ["danger", "dangerbg"], ["danger", "surface"], ["ondanger", "danger"]
];

for (const [name, tokens] of [["light", light], ["dark", dark]]) {
    test(`${name} theme text reaches AA contrast`, () => {
        for (const [fg, bg] of textPairs) {
            assert.ok(tokens[fg] && tokens[bg], `${name}: missing --${fg} or --${bg}`);
            const ratio = contrast(tokens[fg], tokens[bg]);
            assert.ok(ratio >= 4.5, `${name}: --${fg} on --${bg} is ${ratio.toFixed(2)}:1`);
        }
    });

    test(`${name} theme focus ring and field borders stand out (3:1)`, () => {
        for (const bg of ["bg", "surface"]) {
            assert.ok(contrast(tokens.focus, tokens[bg]) >= 3, `${name}: focus on ${bg}`);
            assert.ok(contrast(tokens.inputline, tokens[bg]) >= 3, `${name}: input border on ${bg}`);
        }
    });
}

test("the system dark theme and the chosen dark theme are identical", () => {
    assert.deepEqual(darkSystem, dark);
});
