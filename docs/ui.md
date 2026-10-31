# Web interface

What the browser front end does, who may do what, and how it is built. See `docs/development.md` for the Go conventions and the root `CLAUDE.md` for commands.

## Behaviours and permissions

The API is the authority on every permission below: the interface only hides what a user can't do, it never grants anything. Each line names the endpoint the action calls.

### Wishlists

| Action | Who | Endpoint |
|---|---|---|
| See a list and its wishes | owner, collaborators, members of a group it is shared with | `GET /api/auth/wishlists/:id`, `GET /api/auth/wishes?wishlist=:id` |
| Create a list (name, description, expiry date, claimable, hide claimers, public, groups) | anyone; only groups they belong to | `POST /api/auth/wishlists[?groupContextID=:group]` |
| Edit a list | owner, collaborators | `POST /api/auth/wishlists/:id` |
| Delete a list | owner | `DELETE /api/auth/wishlists/:id` |
| Share with groups (several per list) | owner, groups they belong to | `POST /api/auth/wishlists/:id/join` |
| Stop sharing with a group | owner | `POST /api/auth/wishlists/:id/remove` (`group_id`) |
| Add or remove collaborators | owner | `POST /api/auth/wishlists/:id/collaborate`, `/un-collaborate` |
| Public link | anyone with the link, no account needed | `GET /api/open/wishlists/public/:hash` |

A list's role for the current user is `owner`, `collaborator` or `group_member`. Lists are sorted by the date their wishes were last updated; the home page shows the five most recent active ones (`?top=5&expired=false`). Expired lists (an expiry date in the past) are hidden behind a toggle.

### Wishes

| Action | Who | Endpoint |
|---|---|---|
| Add a wish (name, note, link, price, image, category) | owner and collaborators of an active list | `POST /api/auth/wishes?wishlist=:id` |
| Edit, replace or remove its image, delete | owner and collaborators of its list | `POST /api/auth/wishes/:id`, `DELETE /api/auth/wishes/:id` |
| Claim and unclaim | group members of a claimable list, never the owner or a collaborator, one claimer per wish | `POST /api/auth/wishes/:id/claim`, `/unclaim` |
| See who claimed | group members, unless the list hides claimers (the API then sends "Hidden User") | part of the wish |

The API removes claims from what the owner and collaborators receive, so the surprise is kept by the server, not by the page. Where to put a new wish:

- from the home page with several eligible lists: the user must pick one, nothing is preselected;
- with a single eligible list: it is used without asking;
- from a list: that list, with an optional "Change list" button.

Eligible means active (not expired) and owned or collaborated on.

Wishes can be grouped by category (categories keep the order they were created in, and can be collapsed) or shown flat, and sorted by date or price. The choice is kept in `localStorage` (`poenskelistenWishSort`), along with the language and theme. Nothing else is stored there.

Images are JPEG or PNG up to 10 MB, checked in the browser for a quick answer and again by the server, which re-encodes them (dropping EXIF) and keeps a 250 px thumbnail. Cards show the thumbnail (`GET /api/both/wishes/:id/image?thumbnail=true`), the enlarged view the full image. Product links open only if they are `http(s)`, in a new tab with `rel="noopener noreferrer"`.

### Groups

| Action | Who | Endpoint |
|---|---|---|
| Create (name, description, members, own lists) | anyone | `POST /api/auth/groups` |
| Edit name and description, add or remove members, delete | owner | `POST /api/auth/groups/:id`, `/join`, `/remove`, `DELETE /api/auth/groups/:id` |
| Leave | members other than the owner | `POST /api/auth/groups/:id/leave` |
| Share own lists with it | members | `POST /api/auth/groups/:id/add` |
| Create a list already shared with it | members | `POST /api/auth/wishlists?groupContextID=:id` |

### Account and sign-in

- Sign in with e-mail and password, then a TOTP or recovery code when MFA is on (`/login/mfa` is a real navigation so password managers offer autofill). OIDC sign-in when configured; on an OIDC-only server there is no password form, registration or reset.
- Register with first name, last name, e-mail, password twice, an invite code when required, and consent.
- E-mail verification with a code, and sending a new code.
- Password reset by e-mail link (`/login?reset_code=`).
- Account: e-mail, profile picture, new password (twice) confirmed by the current password. First and last name can't be changed. MFA enrol with QR code and manual key, recovery codes shown once, disable with password and code. Sign out of every device. Revoke connected apps. OIDC accounts manage e-mail, password and MFA at their provider.
- When the administrator enforces MFA, a user without it is sent to `/enroll` before getting a token.

### Administration (admins only)

Server information (read only, including the application name), invite codes (list, generate, delete unused), users (delete, sign out everywhere, remove MFA), OAuth clients (list, revoke), currency, MFA enforcement and recovery codes, news posts (title, text, publication date, optional expiry). The server hides future posts from non-admins and expired posts from everyone.

### Application name

`poenskelisten_name` (flag `-name`, variable `name`) replaces "Pønskelisten" in page titles, the header, the sign-in pages, the web app manifest, e-mails, the TOTP issuer and token issuer. It is a server setting only, shown in the admin server information.

## How the pages are built

- **Templates.** Each page in `web/html` is a few lines: the shared `head`, `appnav` and `main` parts of `web/templates/layout.html`, plus its script. Static text in the templates carries `data-i18n` keys.
- **Scripts.** `web/js/core.js` (DOM building, translations, preferences, API, images, dialogs, shared rules), `session.js` (OAuth + PKCE session), `cards.js` (list and wish cards, the wish board), `mfa.js` (MFA enrolment), then one script per page. They are plain scripts sharing the `P` namespace; no bundler, no dependency.
- **Safety.** Pages never inject markup. `P.h` sets every user value with `textContent`, refuses inline handlers and only lets in-app paths and http(s) URLs into links, and same-origin, `data:image` and `blob:` URLs into images. Product links open with `rel="noopener noreferrer"`. Nothing is loaded from another site. `main_test.go` checks all of this on the source files.
- **Translations.** `web/json/lang-en.json` and `lang-fr.json` hold the strings, with `{placeholders}`; `{app}` is the application name. The language is the saved choice, else the browser's, else English. `main_test.go` checks both files have the same keys and placeholders and that every key the pages use exists. To add a language, add `lang-<code>.json`, list the code in `P.LANGS` and offer it in the display menu (`layout.html`) and on the account page.
- **Themes.** `web/css/app.css` holds the tokens of the light and dark themes. The dark theme follows the system unless the user chose one in the display menu; `theme.js` applies that choice before the first paint. `tests/web/theme.test.mjs` checks the contrast of every text colour.
- **Application name.** The server puts it in the title, the header and an `application-name` meta tag (escaped by `html/template`); scripts read it back from there.
- **Offline.** `service-worker.js` precaches `/offline` and the files it loads, and serves it when a page can't be reached. It never caches API answers.

## Tests

`go test ./...` renders every page through the router (with an application name full of special characters) and checks the dictionaries, the translation keys, the service worker's precache list and the source hygiene rules above. `node --test tests/web/*.test.mjs` runs the front-end unit tests in Node with a small fake DOM: `core.js` (escaping, URL rules, translations, sorting, roles, where a new wish goes, claim states, preferences), `cards.js` (what each viewer is offered) and the theme contrast. Neither replaces a look in a real browser: layout, focus order and screen reader output need a manual check.
