// Loads the browser scripts of web/js into a Node VM with a small fake DOM,
// enough for core.js to build elements. It is not a browser: layout, CSS and
// real events are out of scope; those need a manual check in a browser.
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

export const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");

class FakeNode {
    constructor(nodeType, name) {
        this.nodeType = nodeType;
        this.nodeName = name;
        this.childNodes = [];
        this.parentNode = null;
    }
    get firstChild() { return this.childNodes[0] || null; }
    appendChild(child) {
        if (child.parentNode) {
            child.parentNode.removeChild(child);
        }
        child.parentNode = this;
        this.childNodes.push(child);
        return child;
    }
    insertBefore(child, ref) {
        if (!ref) {
            return this.appendChild(child);
        }
        child.parentNode = this;
        this.childNodes.splice(this.childNodes.indexOf(ref), 0, child);
        return child;
    }
    removeChild(child) {
        this.childNodes = this.childNodes.filter((node) => node !== child);
        child.parentNode = null;
        return child;
    }
    get textContent() {
        return this.nodeType === 3 ? this.data : this.childNodes.map((node) => node.textContent).join("");
    }
    set textContent(value) {
        if (this.nodeType === 3) {
            this.data = String(value);
            return;
        }
        this.childNodes = [];
        if (value !== "") {
            this.appendChild(new FakeText(String(value)));
        }
    }
}

class FakeText extends FakeNode {
    constructor(data) {
        super(3, "#text");
        this.data = data;
    }
}

class FakeElement extends FakeNode {
    constructor(tag, ns) {
        super(1, tag.toUpperCase());
        this.tagName = tag.toUpperCase();
        this.namespaceURI = ns || "http://www.w3.org/1999/xhtml";
        this.attributes = {};
        this.listeners = {};
        this.dataset = {};
        this.style = {};
        this._value = "";
        this.open = false;
    }
    // Form controls: an input starts empty, a select reports its selected
    // option, like in a browser.
    get value() {
        if (this.tagName === "SELECT" && this._value === "") {
            const options = this.querySelectorAll("option");
            const chosen = options.find((option) => option.selected) || options[0];
            return chosen ? String(chosen.value) : "";
        }
        return this._value;
    }
    set value(value) { this._value = value === null || value === undefined ? "" : String(value); }
    focus() { this.ownerDocument && (this.ownerDocument.activeElement = this); }
    blur() {}
    select() {}
    scrollIntoView() {}
    contains(node) { return node === this || this.all().includes(node); }
    showModal() { this.open = true; }
    close(result) {
        this.open = false;
        this.returnValue = result || "";
        this.dispatch("close");
    }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null; }
    removeAttribute(name) { delete this.attributes[name]; }
    hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name); }
    addEventListener(type, handler) { (this.listeners[type] = this.listeners[type] || []).push(handler); }
    dispatch(type, event) { (this.listeners[type] || []).forEach((handler) => handler(event || { type, target: this, preventDefault() {} })); }
    get className() { return this.getAttribute("class") || ""; }
    set className(value) { this.setAttribute("class", value); }
    get children() { return this.childNodes.filter((node) => node.nodeType === 1); }
    all() {
        return this.children.flatMap((child) => [child, ...child.all()]);
    }
    querySelectorAll(selector) {
        return this.all().filter((el) => matches(el, selector));
    }
    querySelector(selector) {
        return this.querySelectorAll(selector)[0] || null;
    }
    // Markup is never parsed: a test reading innerHTML sees escaped text only.
    get innerHTML() {
        return this.childNodes.map(serialize).join("");
    }
    set innerHTML(value) {
        throw new Error("innerHTML assignment is not supported by the fake DOM");
    }
}

function escapeText(text) {
    return String(text).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function serialize(node) {
    if (node.nodeType === 3) {
        return escapeText(node.data);
    }
    const tag = node.tagName.toLowerCase();
    const attrs = Object.entries(node.attributes).map(([name, value]) => ` ${name}="${escapeText(value).replace(/"/g, "&quot;")}"`).join("");
    return `<${tag}${attrs}>${node.childNodes.map(serialize).join("")}</${tag}>`;
}

// matches supports the selectors the tests and core.js use: tag, .class,
// [attr] and [attr="value"], one simple selector at a time.
function matches(el, selector) {
    const attr = selector.match(/^([a-z]*)\[([\w-]+)(?:="([^"]*)")?\]$/i);
    if (attr) {
        return (!attr[1] || el.tagName === attr[1].toUpperCase()) && el.hasAttribute(attr[2]) && (attr[3] === undefined || el.getAttribute(attr[2]) === attr[3]);
    }
    if (selector.includes(",")) {
        return selector.split(",").some((part) => matches(el, part.trim()));
    }
    const tagClass = selector.match(/^([a-z0-9]*)\.([\w-]+)$/i);
    if (tagClass) {
        return (!tagClass[1] || el.tagName === tagClass[1].toUpperCase()) && el.className.split(/\s+/).includes(tagClass[2]);
    }
    if (selector.startsWith("#")) {
        return el.id === selector.slice(1);
    }
    return el.tagName === selector.toUpperCase();
}

class FakeDocument extends FakeElement {
    constructor() {
        super("#document");
        this.nodeType = 9;
        this.documentElement = new FakeElement("html");
        this.head = new FakeElement("head");
        this.body = new FakeElement("body");
        this.documentElement.appendChild(this.head);
        this.documentElement.appendChild(this.body);
        this.appendChild(this.documentElement);
        this.readyState = "complete";
        this.activeElement = this.body;
        this.cookie = "";
        this.title = "";
    }
    createElement(tag) {
        const el = new FakeElement(tag);
        el.ownerDocument = this;
        return el;
    }
    createElementNS(ns, tag) { return new FakeElement(tag, ns); }
    createTextNode(text) { return new FakeText(text); }
    getElementById(id) { return this.all().find((el) => el.id === id || el.getAttribute("id") === id) || null; }
}

export function memoryStorage() {
    const data = new Map();
    return {
        getItem: (key) => (data.has(key) ? data.get(key) : null),
        setItem: (key, value) => data.set(key, String(value)),
        removeItem: (key) => data.delete(key),
        data
    };
}

export function loadCore(options = {}) {
    return loadScripts(["core.js"], options);
}

export function loadScripts(files, options = {}) {
    const document = new FakeDocument();
    if (options.appName !== undefined) {
        const meta = document.createElement("meta");
        meta.setAttribute("name", "application-name");
        meta.setAttribute("content", options.appName);
        document.head.appendChild(meta);
    }
    const sandbox = {
        document,
        localStorage: options.storage || memoryStorage(),
        navigator: { languages: options.languages || ["en-GB"] },
        location: { origin: "https://wishes.example", href: "https://wishes.example/", pathname: "/", search: "", hash: "", reload() {} },
        history: { replaceState() {} },
        sessionStorage: memoryStorage(),
        URL,
        Intl,
        setTimeout: () => 0,
        // API calls never answer: rendering must not wait on them.
        fetch: () => new Promise(() => {}),
        console
    };
    Object.assign(sandbox, options.globals || {});
    sandbox.window = sandbox;
    if (options.setup) {
        options.setup(document);
    }
    vm.createContext(sandbox);
    for (const file of files) {
        vm.runInContext(fs.readFileSync(path.join(root, "web/js", file), "utf8"), sandbox, { filename: file });
    }
    return { P: sandbox.P, document, sandbox };
}

export function dictionary(lang) {
    return JSON.parse(fs.readFileSync(path.join(root, `web/json/lang-${lang}.json`), "utf8"));
}
