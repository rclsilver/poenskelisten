# Pønskelisten

[![CI](https://img.shields.io/github/actions/workflow/status/aunefyren/poenskelisten/go.yml?branch=main&style=for-the-badge&label=CI)](https://github.com/aunefyren/poenskelisten/actions/workflows/go.yml)
[![Backend coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/aunefyren/c6b71770c9d068fecac9a809568ae9e6/raw/poenskelisten-coverage.json&style=for-the-badge)](https://github.com/aunefyren/poenskelisten/actions/workflows/go.yml)
[![Github Stars](https://img.shields.io/github/stars/aunefyren/poenskelisten?style=for-the-badge)](https://github.com/aunefyren/poenskelisten)
[![Github Forks](https://img.shields.io/github/forks/aunefyren/poenskelisten?style=for-the-badge)](https://github.com/aunefyren/poenskelisten)
[![Docker Pulls](https://img.shields.io/docker/pulls/aunefyren/poenskelisten?style=for-the-badge)](https://hub.docker.com/r/aunefyren/poenskelisten)
[![Newest Release](https://img.shields.io/github/v/release/aunefyren/poenskelisten?style=for-the-badge)](https://github.com/aunefyren/poenskelisten/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/aunefyren/poenskelisten?style=for-the-badge)](https://go.dev/dl/)

<br>

[![Donate](https://img.shields.io/badge/PayPal-Buy%20me%20coffee-blue?style=for-the-badge)](https://www.paypal.com/donate/?hosted_button_id=YRKMNM4S8VNBS)

Like the project? Have too much money? Buy me a coffee or something! ☕️

---

## What is Pønskelisten? 🎁

A self-hosted web app for creating, sharing and collaborating on wishlists - *without ruining the surprise*.  
Share gift ideas, see which ones are already taken, and avoid the awkward “oh… you also bought that…” moment.

### Main Features
- Create wishlists and add wishes
- Collaborate with friends & family on the shared wishlists
- Create groups to share wishlists with multiple people
- Claim wishes anonymously (others see it's taken - owner does not)
- Organise wishes into categories, and sort or group them on a wishlist
- Share a wishlist publicly with people who don't have an account
- Sign in with a password, optionally with MFA (authenticator app), or through single sign-on (OpenID Connect)
- Let an AI assistant read your wishlists through the built-in MCP server (optional)
- Installable as a web app (PWA)

### Known Limitations
- UI is not yet fully optimized for small screens

<br>

![Wishlists screenshot](https://raw.githubusercontent.com/aunefyren/poenskelisten/main/.github/assets/wishlists.jpg?raw=true)

<br>

---

## 🚀 Installation

Pønskelisten is flexible to host. Choose your path:

### **Step 1: Choose how to run it**

| Method | Difficulty | Notes |
|--------|-------------|------------------|
| **⭐Docker** | Easiest | You need to run your instance in a Docker container |
| Download executable | Easy | Choose the correct executable for your system |
| Build from source | Medium | You need to have Go installed |

### **Step 2: Choose your Database**

Pønskelisten currently supports:

| Database | Status | Notes |
|----------|--------|--------|
| **⭐SQLite** | ✅ Fully supported | DB file is handled by Pønskelisten |
| PostgreSQL | ✅ Fully supported | Requires a running PostgreSQL instance |
| MySQL | ✅ Fully supported | Requires a running MySQL instance |


---

## 🧩 Configuration (Recommended: Environment Variables)

You can configure Pønskelisten in **three different ways**:

| Method | Ideal for | Notes |
|--------|------------|--------|
| **⭐Environment variables** | Docker | Add the environment variables to your Dockerfile or docker-compose.yaml |
| Startup flags | Executables | Adding a flags to the startup command alters something in the configuration file |
| config.json | Access to file system | Pønskelisten generates the file on the first run. Can be altered in a text editor afterward |

---

### 📍 Available Configuration Options

| Config file entry | Startup flag | Environment variable |Type | Description |
|-----|-----|-----|-------|--------------|
| poenskelisten_port | port | port | int | Port to run on (default: `8080`) |
| poenskelisten_external_url | externalurl | externalurl | string | **Required.** The main URL users open in their browser, e.g. `https://wishlist.example.com`. Used for login, e-mail links, public wishlist links, single sign-on and as the OAuth issuer. If unset, only `http://localhost:<port>` can log in. |
| poenskelisten_additional_urls | additionalurls | additionalurls | string | Comma-separated extra URLs users can log in from, e.g. `http://192.168.1.10:8080,http://wishlist.lan`. Each must be just scheme, host and port. Links and single sign-on still use `external_url`. |
| poenskelisten_trusted_proxies | trustedproxies | trustedproxies | string | Comma-separated IP addresses or CIDR ranges of your reverse proxies, e.g. `172.18.0.0/16` or `10.244.0.0/16`. Only requests coming from these may give the visitor's address with `X-Forwarded-For`/`X-Real-IP`; anyone else's headers are ignored. Default: none, so the address of the connection is used. See [Behind a reverse proxy](#behind-a-reverse-proxy) |
| poenskelisten_environment | environment | environment | string | `production` or `test` |
| poenskelisten_test_email | testemail | testemail | string | E-mail destination when in `test` |
| poenskelisten_name | name | name | string | Display name of the app |
| poenskelisten_description | description | description | string | Description of the app |
| poenskelisten_log_level | loglevel | loglevel | string | How detailed the logs are. `info`, `debug` or `trace`
| timezone | timezone | timezone | string | E.g. `Europe/Oslo` |
| `N/A` | generateinvite | generateinvite | bool | Generate an invite code on startup. Do `generateinvite true` |
| `N/A` | resetpassword | resetpassword | string | E-mail of a user to issue a password reset link for on startup. See [Admin Access](#-admin-access) |
| `N/A` | resetmfa | resetmfa | string | E-mail of a user to remove MFA for on startup. See [Admin Access](#-admin-access) |
| db_type | dbtype | dbtype | string | `sqlite`, `postgres` or `mysql` |
| db_ip | dbip | dbip | string | DB host |
| db_port | dbport | dbport | int | DB port |
| db_username | dbusername | dbusername | string | DB username |
| db_password | dbpassword | dbpassword | string | DB password |
| db_name | dbname | dbname | string | Database name |
| db_ssl | dbssl | dbssl | bool | Use SSL for DB |
| db_location | dblocation | dblocation | string | SQLite database file path (default: `files/data.db`) |
| smtp_enabled | disablesmtp | disablesmtp | bool | Disable/enable email functions |
| smtp_host | smtphost | smtphost | string | SMTP host |
| smtp_port | smtpport | smtpport | int | SMTP port |
| smtp_username | smtpusername | smtpusername | string | SMTP user |
| smtp_password | smtppassword | smtppassword | string | SMTP password |
| smtp_from | smtpfrom | smtpfrom | string | Sender email address |
| mfa_enforced | mfaenforced | mfaenforced | bool | Require all local users to enroll in MFA (TOTP) |
| rate_limit_login | ratelimitlogin | ratelimitlogin | string | Requests per visitor address, shared by login, the MFA step, registration, password reset and e-mail verification (and the password checks on the account page), as `<requests>/<duration>`, or `off`. Default `20/15m`. See [Rate limits](#rate-limits) |
| rate_limit_oauth | ratelimitoauth | ratelimitoauth | string | Requests per visitor address, shared by `/oauth/authorize`, `/oauth/consent`, `/oauth/token` and `/oauth/revoke`, or `off`. Default `60/1m` |
| rate_limit_register | ratelimitregister | ratelimitregister | string | App registrations (`/oauth/register`) per visitor address, or `off`. Default `10/1h` |
| rate_limit_account | ratelimitaccount | ratelimitaccount | string | Failed sign-in attempts per account, from any address, before it is locked for the rest of the window: wrong passwords per e-mail, wrong MFA codes per user. Default `10/15m`, or `off` |
| rate_limit_upload | ratelimitupload | ratelimitupload | string | Requests per account to the routes that can carry an image: creating and editing wishes, and updating one's account. Default `30/10m`, or `off` |
| mfa_recovery_codes_enabled | mfarecoverycodes | mfarecoverycodes | bool | Issue single-use recovery codes on MFA enrollment (default off; when off, an admin must remove MFA for locked-out users) |
| oidc_enabled | oidcenabled | oidcenabled | bool | Enable OpenID Connect single sign-on |
| oidc_provider_name | oidcprovidername | oidcprovidername | string | Display name on the SSO login button (e.g. "Authelia") |
| oidc_issuer_url | oidcissuerurl | oidcissuerurl | string | OIDC issuer URL used for discovery (e.g. https://auth.example.com) |
| oidc_client_id | oidcclientid | oidcclientid | string | OIDC client ID |
| oidc_client_secret | oidcclientsecret | oidcclientsecret | string | OIDC client secret |
| oidc_redirect_url | oidcredirecturl | oidcredirecturl | string | OIDC callback URL; defaults to `<external_url>/api/open/oidc/callback` |
| oidc_auto_create_users | oidcautocreateusers | oidcautocreateusers | bool | Auto-provision unknown OIDC users (default off) |
| local_login_disabled | disablelocallogin | disablelocallogin | bool | OIDC-only mode: turn off password login, registration and password reset (default off; ignored unless OIDC is enabled and configured) |
| mcp_enabled | mcpenabled | mcpenabled | bool | Enable the MCP resource server, and with it registration of third-party apps (the OAuth authorization server is always on) |
| oauth_signing_key | `N/A` | `N/A` | string | PEM signing key; auto-generated + persisted on first run (config.json only) |
| oauth_signing_key_id | `N/A` | `N/A` | string | Key ID of `oauth_signing_key`; auto-generated alongside it (config.json only) |
| private_key | `N/A` | `N/A` | string | Secret for the login-session cookie; auto-generated on first run (config.json only) |
| poenskelisten_currency, poenskelisten_currency_left, poenskelisten_currency_pad | `N/A` | `N/A` | string / bool | Currency symbol and placement; set from the admin panel |

### 📁 The `files/` folder and `config.json`

Pønskelisten keeps its state in `files/` (config, log and, with SQLite, the database) and wishes/profile pictures in `images/`. Both must be on persistent storage (the Docker volumes below), and `files/` belongs in your backups.

- `config.json` is created on the first run. Values given as startup flags or environment variables are **written back into it** whenever they differ from what's there, so it's always the full picture of your configuration.
- It holds secrets: the auto-generated `oauth_signing_key` and `private_key`, plus any database, SMTP and OIDC passwords you configure, including ones passed as environment variables. Treat it as sensitive. Pønskelisten creates `config.json` and its log readable by its own user only (`0600`) and keeps `files/` closed to other users (`0700`); on every start it tightens `config.json` and `files/` if an older version left them readable. A backup job that reads the folder as a different, non-root user needs to run as the app's user instead.
- Keep the signing keys. If `config.json` is lost or replaced, new keys are generated: everyone is logged out, and connected apps (MCP clients) have to be authorised again.

### Behind a reverse proxy

Behind a reverse proxy (Traefik, Caddy, nginx, ...), every connection comes from the proxy, which passes the visitor's address in `X-Forwarded-For`. Pønskelisten only believes that header from the proxies listed in `trustedproxies`, since any client can send it. Set it to your proxy's address or network, e.g. `trustedproxies=172.18.0.0/16` for a proxy on a Docker network, or the pod network (e.g. `10.244.0.0/16`) on Kubernetes. Without it, every visitor is seen with the proxy's address, which shows in session lists and makes everyone share the same rate limits; a warning is logged once when that happens. Your proxy must overwrite or append to `X-Forwarded-For`, not pass a client's own value through untouched (Traefik, Caddy and nginx's `$proxy_add_x_forwarded_for` all do).

Pønskelisten's access log, on its standard output, leaves out query strings and the codes some URLs carry: password reset and e-mail verification codes, and public wishlist links. Your proxy's access log, if you keep one, may record whole URLs, so reset links: keep it as private as the app's data.

### Security headers

Every response carries `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` (links to shops never carry a public wishlist link or a reset code in their `Referer`), and `X-Frame-Options: DENY`, so no other site can frame Pønskelisten, for instance to trick a click on the consent page. Embedding it in a dashboard iframe isn't possible as a result.

A strict `Content-Security-Policy` lets pages run only the scripts and styles the app serves itself and load images from it or from `data:` URLs; nothing comes from another site, and `frame-ancestors 'none'` repeats the framing ban. A reverse proxy that injects scripts or styles into pages (analytics, banners) will see them blocked.

### Rate limits

Password and code checks are rate-limited per visitor address (IPv6 addresses are grouped by `/64`), with a sliding window. Requests over a limit get `429 Too Many Requests` and a `Retry-After` header, and the first refusal per address and window is logged as a warning.

- `rate_limit_login` (default `20/15m`): login, the MFA step, registration, password reset and e-mail verification share one budget. Logging in with MFA takes two requests.
- `rate_limit_oauth` (default `60/1m`): the OAuth endpoints used by the web app and connected apps. An open tab refreshes its token every ten minutes.
- `rate_limit_register` (default `10/1h`): registration of new connected apps (MCP clients register once, when you connect them).
- `rate_limit_account` (default `10/15m`): failed attempts per account, whatever address they come from, so guessing can't be spread over many addresses. Wrong passwords count per e-mail typed (an e-mail without an account locks the same way, so the lock tells nothing), together with wrong current passwords on the account page; wrong MFA codes count per user. A locked account is refused with `429` until its oldest failure is older than the window, even with the right password, which also keeps out the real user meanwhile; a successful sign-in clears the count.

- `rate_limit_upload` (default `30/10m`): creating and editing wishes and updating one's account, which can all carry an image. It is counted per account rather than per address, so people sharing a home connection each have their own.

Write a limit as `<requests>/<duration>` (durations like `30s`, `15m`, `1h`), or `off` to turn it off. The counters live in memory and start over on a restart. Behind a reverse proxy, set [`trustedproxies`](#behind-a-reverse-proxy) first, or all your visitors share one budget.

### Images and memory

Uploaded images (JPG or PNG, up to 10 MB) can have at most 4 megapixels, such as 2000x2000; the web app scales larger photos down before sending them. They are stored at most 1000x1000, with a 250x250 thumbnail. Decoding one still takes tens of megabytes for a moment, so at most two are processed at a time.

In a container with a memory limit, set the `GOMEMLIMIT` environment variable a little below it, for example `GOMEMLIMIT=400MiB` for a 512 MiB limit. Go then collects garbage harder as it nears that amount, rather than letting the container be killed for running out of memory.

---

## 🔐 Single sign-on (OpenID Connect)

Pønskelisten can act as an OpenID Connect **relying party**, letting users log in
through an external identity provider (Authelia, Keycloak, Authentik, Google, …).
A successful SSO login mints a normal Pønskelisten session, so everything else
works the same afterwards.

**How accounts are resolved on SSO login:**

1. If the IdP subject (`sub`) is already linked to a user, that user logs in.
2. Otherwise, if the IdP asserts a **verified** email that matches an existing
   local account, the OIDC identity is linked to it. An **unverified** email is
   refused.
3. Otherwise a new account is created **only if** `oidc_auto_create_users` is on
   (default off); the account is created pre-verified with no local password.

**Example with Authelia**: register Pønskelisten as an OIDC client in your
Authelia configuration:

```yaml
identity_providers:
  oidc:
    clients:
      - client_id: poenskelisten
        client_secret: '<hashed-or-plaintext-per-your-authelia-version>'
        redirect_uris:
          - https://wishlist.example.com/api/open/oidc/callback
        scopes: [openid, profile, email]
```

Then configure Pønskelisten (env vars shown; flags/config.json equivalents exist):

```yaml
    environment:
      externalurl: https://wishlist.example.com
      oidcenabled: true
      oidcprovidername: Authelia
      oidcissuerurl: https://auth.example.com
      oidcclientid: poenskelisten
      oidcclientsecret: <the-client-secret>
      # oidcredirecturl defaults to <externalurl>/api/open/oidc/callback
      oidcautocreateusers: false
```

The redirect URL registered with the IdP must match
`<external_url>/api/open/oidc/callback`. Single sign-on therefore only works when
Pønskelisten is opened on `external_url`, not on one of the `additional_urls`.

**OIDC-only mode**: set `disablelocallogin: true` to make SSO the only way in.
Password login (including its MFA step), self-registration and password reset
are then refused by the API, and the login page only shows the SSO button.
Existing local accounts keep working through SSO once linked by a verified
email, and local MFA enforcement no longer applies, since the IdP owns MFA.
The setting is ignored, with a warning in the log, unless OIDC is enabled with
an issuer URL and client ID, so a broken SSO setup can't lock everyone out. If
the IdP is unavailable, set `disablelocallogin: false` and restart to get
password login back.

## 🤖 MCP server (AI assistants)

Pønskelisten can expose an authenticated **MCP (Model Context Protocol)** endpoint
so an AI client (e.g. Claude) can read your wishlists on your behalf. It is a full
OAuth 2.1 setup: the app is its own authorization server, and the MCP endpoint is a
resource server that only accepts audience-scoped tokens.

Enable it with `mcp_enabled: true` (it's off by default). The endpoint lives at
`<external_url>/mcp`; the client discovers everything else via
`<external_url>/.well-known/oauth-protected-resource`, self-registers
(`/oauth/register`), and runs the browser login + consent flow — no manual client
setup. Set `external_url` to the public HTTPS address the client connects to, since the OAuth issuer and the MCP resource identifier are derived from it.

Read tools: `list_wishlists` (yours, ones you collaborate on and ones shared with you through groups, each with its owner and your role), `list_wishes` (same access rules as the web app; whether a wish is claimed is shown to group members only, and never to the list owner or collaborators), `get_public_wishlist` (by `public_hash`; like the public page, it never says whether a wish is claimed), `list_groups` and `list_group_members` (only for groups you belong to; each member is an id and a name, never an e-mail)
(gated by the `mcp:wishlists.read` / `mcp:groups.read` scopes you approve on the
consent screen).

Write tools need the `mcp:wishlists.write` scope, which the consent screen words as "Create, edit, share and delete your wishlists and wishes", and follow the web app's rules exactly: `create_wishlist` (optionally shared with groups you belong to), `update_wishlist` (owner or collaborator; only the given fields change) and `delete_wishlist` (owner only). Deleting only disables the wishlist, like the web app, and never touches image files. Callers without access get the same "not found or not accessible" error as for a wishlist that doesn't exist.

Wishes: `create_wish`, `update_wish` and `delete_wish` work for the owner or a collaborator of the wishlist (not for group members, nor the wish's author alone, as on the web); deleting a wish disables it, keeps its image and does not e-mail whoever claimed it. Images cannot be set through MCP.

Sharing is the owner's alone, as on the web: `get_wishlist_sharing` (read scope) shows a wishlist's groups, collaborators and public link; `attach_wishlist_to_group` shares it with a group you belong to, and `detach_wishlist_from_group` withdraws a share (even from a group you have since left). `add_collaborator` and `remove_collaborator` take a `user_id`; unlike the web, which lets you pick any account, the MCP only adds people you share a group with (as listed by `list_group_members`), and answers "user not found or not reachable" for anyone else. Removing a collaborator always works. The public link is turned off with `update_wishlist(public: false)`, and turning it back on issues a new one.

Claims need the `mcp:wishes.claim` scope ("Claim wishes, and cancel your claims, on your behalf"): `claim_wish` and `unclaim_wish` take a `wish_id` and return `{id, claimed}`, never who else holds a claim. As on the web, the wishlist must be claimable, you must be a group member of it (not its owner or a collaborator, not the wish's author), a wish has a single claimer and only your own claim can be released. The wishlist owner is not e-mailed. Unclaiming twice reports an error the second time.

Connected apps only ever get access to the MCP endpoint, never to
the rest of Pønskelisten's API, and app registration is switched off along with MCP. Registration is open while MCP is on, so it is rate-limited (`rate_limit_register`) and bounded: at most 10 redirect URIs of up to 2000 characters each, https or http, without a fragment or credentials, and a single-line `client_name` of up to 100 characters. Users can disconnect an app under "Connected apps" on their account page, and admins can revoke any registered client from the admin panel.

## 🐳 Docker Setup

### **Minimal docker-compose.yml for SQLite (recommended)**

```yaml
services:
  poenskelisten-app:
    container_name: poenskelisten-app
    image: ghcr.io/aunefyren/poenskelisten:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      PUID: 1000
      PGID: 1000
      externalurl: http://localhost:8080 # the URL you open Pønskelisten on
      dbtype: sqlite
      timezone: Europe/Oslo
      generateinvite: true
    volumes:
      - ./files/:/app/files/:rw
      - ./images/:/app/images/:rw
```
Remove `generateinvite` after first run to stop generating codes on start up.

`PUID`/`PGID` pick the user and group the app runs as (default `1000`). The container starts as root, gives that user ownership of `/app/files` and `/app/images` (including any bind-mounted folder Docker created as root), and then drops to it. Set them to match the owner of your host folders. If you'd rather the container never runs as root, set `user: "1000:1000"` on the service instead. In that case `PUID`/`PGID` are ignored, and the host folders must already be writable by that user.

### **Minimal docker-compose.yml for postgres**

```yaml
services:
  db:
    container_name: poenskelisten-db
    image: postgres:16
    restart: unless-stopped
    environment:
      POSTGRES_DB: poenskelisten
      POSTGRES_USER: myuser
      POSTGRES_PASSWORD: mypassword
    volumes:
      - ./db/:/var/lib/postgresql/data/:rw

  poenskelisten-app:
    container_name: poenskelisten-app
    image: ghcr.io/aunefyren/poenskelisten:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      PUID: 1000
      PGID: 1000
      externalurl: http://localhost:8080 # the URL you open Pønskelisten on
      dbtype: postgres
      dbip: db
      dbport: 5432
      dbname: poenskelisten
      dbusername: myuser
      dbpassword: mypassword
      timezone: Europe/Oslo
      generateinvite: true
    depends_on:
      - db
    volumes:
      - ./files/:/app/files/:rw
      - ./images/:/app/images/:rw
```
Remove `generateinvite` after first run to stop generating codes on start up.

### Optional Add-ons

- Reverse proxy for access outside of home network (Caddy, Traefik, Nginx)

- Adminer or phpMyAdmin (if you like UI DB tools, but not required anymore)

## 🔑 Admin Access

- First registered user becomes admin

- Additional invite codes can be created in the admin panel

- If you lose access: restart with generateinvite=true (in OIDC-only mode, also set `disablelocallogin: false`, since invites are used through self-registration)

### Recovering an account from the server

If a user (including yourself) has lost their password or their MFA device, anyone who can start Pønskelisten can recover the account. Neither option needs SMTP.

- **`resetpassword <e-mail>`** issues a password reset link and writes it to the log (`files/poenskelisten.log` and the console/`docker logs`). Open the link to choose a new password. It works like the e-mailed reset link: valid for 24 hours, single-use, and the normal password rules apply. In OIDC-only mode, also set `disablelocallogin: false`, since password reset is part of local login.
- **`resetmfa <e-mail>`** removes MFA and the recovery codes for the user, the same as the admin panel's "remove MFA". If MFA is enforced, the user sets it up again at their next login.

The e-mail is matched ignoring case. Surrounding whitespace and quotes are trimmed, and anything that isn't a plain address is refused before any account is touched.

```bash
./poenskelisten -resetmfa admin@example.com -resetpassword admin@example.com
```

```yaml
    environment:
      resetmfa: admin@example.com
      resetpassword: admin@example.com
```

**Remove the flag or variable once you're back in.** Both run on every start: `resetpassword` issues a new link each time, and `resetmfa` would remove MFA again after the user re-enrols. The reset link also stays in the log file until it expires, so treat the log as sensitive in the meantime.

## 🔧 Building from Source

Requires Go installed:
```bash
go build
./poenskelisten
```

Add flags to configure:
```bash
./poenskelisten -port 9000 -dbtype postgres -generateinvite true
```

## ❓ FAQ

### What does “Pønskelisten” mean?
A Norwegian wordplay. “Ønskeliste” = wishlist, “pønske” = plot/plan.
So… “The plotting list”.

### Is a demo available?
Not at the moment.

### Is mobile UI coming?
Yes - improvements planned.

## 🙌 Contributing

Contributions, issues and feature requests are welcome.
See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a Pull Request.

## ☕️ Donate

If you enjoy using Pønskelisten and want to support development:<br>
[Buy me a coffee](https://www.paypal.com/donate/?hosted_button_id=YRKMNM4S8VNBS)