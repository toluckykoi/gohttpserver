# gohttpserver

The most user-friendly HTTP file server: human-friendly UI, file upload, and QR code generation for iOS/Android installers.

## Features

- QR code generation · Breadcrumb navigation · Type-specific file icons · Show/hide hidden files
- File upload (Token/session auth) · Folder upload · Auto-extract zip on upload · Directory archive download
- Online edit · Code preview · Global search · Quick copy download link · MD5/SHA checksum
- README.md preview · Partial refresh on directory change · Single-subdirectory path merge · Theme switching · Small-screen adaption
- HTTP Basic / OpenID / OAuth2-proxy auth · Built-in login (`--login`) · Login rate limiting
- Apple IPA plist generation · Plist proxy · CORS · Nginx deployment · `--prefix` support
- `.ghs.yml` access control (like `.htaccess`) · `--conf` YAML configuration
- Admin panel (Profile / Settings) · frp tunnel
- Self-update (update_core): check / resume download / checksum / replace binary / graceful restart
- Built-in WebDAV (`/dav/`): per-account root limiting, read-only, quota, system file protection, account management

## Installation

### One-click install

```bash
# Linux
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.sh -o install-gohttpserver.sh && sudo bash install-gohttpserver.sh

# Windows (PowerShell / CMD)
iwr -useb https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.ps1 -o install-gohttpserver.ps1; .\install-gohttpserver.ps1
curl -fsSL https://gitee.com/toluckykoi/gohttpserver/raw/main/install-gohttpserver.bat -o install-gohttpserver.bat && install-gohttpserver.bat
```

The script supports TUI interaction, uninstall prompts, and parameter configuration; start with `systemctl start gohttpserver` on Linux.

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/toluckykoi/gohttpserver/releases) or [Gitee Releases](https://gitee.com/toluckykoi/gohttpserver/releases).

## Quick Start

```bash
gohttpserver -r ./ --port 8000 --upload           # upload
gohttpserver -r ./ --port 8000 --edit             # edit
gohttpserver -r ./ --port 8000 --upload --delete --edit   # all features
gohttpserver -r ./ --port 8000 --login            # login (recommended; admin panel / WebDAV / self-update depend on it)
```

Run `gohttpserver --help` for all options.

### Command Line Options

Command line flags take precedence over `--conf` YAML config; repeatable flags can be passed multiple times.

| Flag | Default | Description |
|------|---------|-------------|
| `--conf` | | Config file path, YAML format |
| `-r`, `--root` | `./` | Serving root directory |
| `--prefix` | | URL prefix, e.g. `/foo` |
| `--port` | `8000` | Listen port |
| `-a`, `--addr` | | Listen address, e.g. `127.0.0.1:8000` (overrides `--port`) |
| `--cert` / `--key` | | TLS cert / key paths |
| `--auth-type` | | `http`, `openid` or `oauth2-proxy` |
| `--auth-http` | | HTTP Basic auth `user:pass` (repeatable) |
| `--auth-openid` | `https://login.netease.com/openid` | OpenID auth URL |
| `--theme` | `black` | `black` / `green` |
| `--upload` / `--delete` / `--edit` | off | Enable upload / delete / edit |
| `--xheaders` | off | Trust `X-Forwarded-*` (when behind Nginx) |
| `--debug` | off | Debug mode |
| `-p`, `--plistproxy` | `...herokuapp.com/plist` | Plist proxy when not HTTPS |
| `--title` | `Go HTTP File Server` | Server title |
| `--google-tracker-id` | | Google Analytics ID |
| `--deep-path-max-depth` | `5` | Max depth for merging single-subdirectory paths, `-1` to disable |
| `--no-index` | off | Disable directory indexing |
| `--cors-origins` | | Allowed CORS origins (repeatable; `*` allows all, insecure) |
| `--allow-public-oauth2` | off | Enable oauth2-proxy on non-loopback address (insecure) |
| `--login` | off | Username/password login, auto-enables upload/delete/edit, default `admin`/`admin` |
| `--db` | `./gohttpserver.db` | SQLite path (requires `--login`) |
| `--session-ttl` | `12h` | Session TTL; `0` = browser-session cookie |
| `--log-level` | `info` | `debug`/`info`/`warn`/`error` |
| `--log-format` | `json` | `json`/`text` |

## Monitoring

- `GET /healthz` (or `/-/healthz`): JSON liveness probe `{"status":"ok","version":"v1.2.3"}`
- `GET /metrics` (or `/-/metrics`): Prometheus text format

Both endpoints are unaffected by `--prefix` and always live at `/healthz` and `/metrics`.

## Login & Authentication

### Built-in login (`--login`)

- Default account `admin`/`admin`; change it after first login via **Admin Panel → Profile**
- Sessions are signed cookies; set the `GHS_SESSION_KEY` env var to fix the signing key, otherwise all sessions are invalidated on restart
- Login rate limit: max 5 failures per IP per 15 minutes

### External auth (`--auth-type`)

```bash
# HTTP Basic
gohttpserver --auth-type http --auth-http username1:password1 --auth-http username2:password2
# OpenID
gohttpserver --auth-type openid --auth-openid https://login.example-hostname.com/openid/
# OAuth2 proxy (set up your own reverse proxy; backend reads the headers below)
gohttpserver --auth-type oauth2-proxy
```

| Header | Value |
|--------|-------|
| `X-Auth-Request-Email` | User ID |
| `X-Auth-Request-Fullname` | Display name (URL encoded) |
| `X-Auth-Request-User` | Nickname (usually email prefix) |

## Admin Panel

With `--login`, click the avatar at the top right → **Admin Panel**:

- **Profile**: username, auth type, version; change username (password required) / password
- **Settings**: WebDAV master switch, URL display, account management
- **Tunnel**: frp client settings

## Self-Update (update_core)

With `--login`, click the version number in the footer to open the "Software Update" dialog; a colored badge appears next to the version when a new release is available.

- Update server info is compiled into the binary ([main.go](main.go), `updateServerConfig`); set `BaseURL` to empty to disable
- Reported `package_type` must **exactly match** the server-side config (Windows=`exe`, Linux/macOS=`bin`)
- Private software: set `GHS_UPDATE_TOKEN`; the token never goes into logs/URL, and downloads do not follow redirects
- Update APIs require login; SHA-256 is re-verified before install, old binary kept as `.bak`, auto-rollback on failure
- HTTP and HTTPS sources supported; HTTPS recommended (plain HTTP is vulnerable to MITM and warns at startup)
- Range resume supported (If-Range bound to SHA-256)

Update APIs (login required): `GET /-/api/update/status`, `POST /-/api/update/{check,download,apply,restart}`

## WebDAV (enabled with `--login`)

Mounted at `/dav/` with an independent Basic Auth account system (WebDAV clients do not share browser cookies). Enable it via Admin Panel → Settings → master switch, then create an account:

- **Remark** (required); **Relative root** (default `/`, path traversal blocked); **Advanced options**: read-only mode, system file protection (default on), quota (GB)
- Username is bound to the current logged-in user; password is a random 10-char string
- Account list: expand to view link/username/password (👁 toggle), ⋮ menu for edit, reset password, delete

### Mapping a network drive on Windows

1. The OPTIONS preflight is handled built-in, no config needed
2. Windows does not send Basic Auth over plain HTTP by default. If you get "401 Unauthorized", change the registry key `HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters\BasicAuthLevel` from 1 to 2 and restart WebClient:

```powershell
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\WebClient\Parameters" -Name BasicAuthLevel -Value 2
Restart-Service WebClient
```

Then map `http://IP:8000/dav/` (or `\\IP@8000\DavWWWRoot\dav\`). Use HTTPS in production to skip the registry change.

### Connecting from Linux

Connect to `dav://host:port/dav/` (or `davs://` for HTTPS) in your file manager. The trailing-slash-less `/dav` path is also handled for compatibility.

## Advanced Usage

### `.ghs.yml` access control

Place a `.ghs.yml` in a subdirectory to control permissions for that directory:

```yaml
---
upload: false    # directory-level defaults
delete: false
edit: false
users:                          # per-user overrides
- email: "codeskyblue@codeskyblue.com"
  delete: true
  upload: true
  edit: true
  token: 4567gf8asydhf293r23r
accessTables:                   # file-level visibility
- regex: block.file
  allow: false
- regex: visual.file
  allow: true
```

### IPA Plist proxy

```bash
gohttpserver --plistproxy=https://someproxyhost.com/
```

Auto-disabled when running behind Nginx with HTTPS configured.

### Upload / edit with curl

```bash
# Upload (add -F token=xxx when token auth is used)
curl -F file=@foo.txt localhost:8000/somedir
# Rename
curl -F file=@foo.txt -F filename=hi.txt localhost:8000/somedir
# Upload and extract zip (original zip is deleted afterwards)
curl -F file=@pkg.zip -F unzip=true localhost:8000/somedir
# Folder upload (structure preserved, path is relative)
curl -F file=@sub/b.txt -F path=MyFolder/sub/b.txt localhost:8000/somedir
# Edit (PUT, requires --edit)
curl -X PUT -H "X-Token: 12312jlkjafs" -d "new content" localhost:8000/somedir/foo.txt
```

Note: filenames cannot contain `\/:*<>|`; `path` cannot contain `..` or absolute paths.

### Nginx deployment

Run gohttpserver with `--xheaders`; headers below (note `client_max_body_size 0;` to disable upload size limit):

```nginx
server {
  listen 80;
  server_name your-domain-name.com;
  location / {
    proxy_pass http://127.0.0.1:8200; # change this
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;
    client_max_body_size 0;
  }
}
```

If the root path is taken by another service, run with `--prefix /foo --addr :8200 --xheaders` and use `location /foo` in Nginx likewise (see [issue#105](https://github.com/codeskyblue/gohttpserver/issues/105)).

## Developer Guide

> **Note**: `frontend/dist` is not committed (ignored by `.gitignore`), but `assets.go` embeds it via `//go:embed`.
> A fresh clone will fail `go build` with `pattern frontend/dist: no matching files found` — run `./build.sh` first (or `npm ci && npm run build` inside `frontend/`).

```bash
./build.sh   # builds; artifacts: gohttpserver-<version>-<os>-<arch>[.exe]
./dist/gohttpserver-1.4.0-windows-x64.exe -r ./testdata --addr 0.0.0.0:8000 --login
```

## Support & License

Forked from [codeskyblue/gohttpserver](https://github.com/codeskyblue/gohttpserver) (original project is no longer maintained). Thanks to the original author.

Licensed under [Apache-2.0](LICENSE).