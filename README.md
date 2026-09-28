# larkctl

[![ci](https://github.com/echowxsy/larkctl/actions/workflows/ci.yml/badge.svg)](https://github.com/echowxsy/larkctl/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/echowxsy/larkctl)](https://github.com/echowxsy/larkctl/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/echowxsy/larkctl.svg)](https://pkg.go.dev/github.com/echowxsy/larkctl)
[![license](https://img.shields.io/github/license/echowxsy/larkctl)](LICENSE)

Command-line client for Feishu/Lark. Reads and writes documents, wiki, sheets, bitable, drive, whiteboards, IM messages, calendar, mail, and tasks — with per-user OAuth, so every action runs as the logged-in user (not a bot).

Works in two modes through the same commands:

| Mode | Auth | When to use |
|------|------|-------------|
| **Gateway** | Device-code flow against a [larkctl-gateway](https://github.com/echowxsy/larkctl-gateway) server | Teams, headless machines, MCP clients; app credentials stay on the gateway |
| **Local** | OAuth authorization-code flow directly against Feishu | Solo use with your own Feishu app `app_id`/`app_secret` |

## Features

- Per-user OAuth (user access tokens, auto-refreshed) — not tenant/bot tokens
- Document → Markdown export, Markdown → document import, image download
- Scope groups: request only the permissions you need (`larkctl login docs wiki`)
- Dynamic scope upgrade: commands needing extra scopes trigger re-auth automatically
- The full [lark-cli](https://github.com/larksuite/cli) command set through the gateway (`larkctl cli`)
- JSON / table / CSV output (`--format`), compact JSON for piping (`-c`)
- Self-upgrade (`larkctl upgrade`), machine-readable command schema (`larkctl schema`)
- Session state in plain files (`~/.lark/config.json`), no system keychain

## Install

Download a prebuilt binary for your platform from [GitHub Releases](https://github.com/echowxsy/larkctl/releases) and put it on your `PATH` as `larkctl`. Later, `larkctl upgrade` self-updates from the latest release.

Or install with Go 1.25+:

```bash
go install github.com/echowxsy/larkctl@latest
```

Build from source:

```bash
go build -o larkctl .        # current platform
make build                   # cross-compile darwin-arm64 / linux-amd64 / windows-amd64 into dist/
```

## Quick start

### Gateway mode

Point the CLI at your gateway once; the URL is saved after the first successful login:

```bash
larkctl --gateway-url https://your-gateway.example.com login
larkctl whoami
```

The gateway URL resolves in order: `--gateway-url` flag → `FEISHU_GATEWAY_URL` env → `gateway_url` in `~/.lark/config.json` → `http://127.0.0.1:8787`.

On headless machines add `--open-browser=false` and open the printed authorization link elsewhere.

### Local mode

You need a Feishu Open Platform app with redirect URI `http://127.0.0.1:19876/callback`:

```bash
larkctl init --app-id <app_id> --app-secret <app_secret>
larkctl login
```

When `~/.lark/config.json` contains both `app_id` and `app_secret`, larkctl enters Local mode and bypasses any configured gateway entirely.

### First calls

```bash
larkctl docs info "https://<tenant>.feishu.cn/wiki/<token>" --type wiki
larkctl docs export <doc_url> -f md -o doc.md    # document → Markdown
larkctl im send --to <chat_id_or_open_id> "hello"
larkctl mcp                                       # print the gateway's MCP endpoint for AI clients
```

## Commands

| Command | Description |
|---------|-------------|
| `docs` | Documents: info, blocks, create, export (pdf/docx/md), update from Markdown (diff-based), inline Mermaid diagrams (文本绘图 widget), images/attachments/links/mentions, comments, permissions, search |
| `wiki` | Wiki spaces and nodes |
| `sheets` | Spreadsheets (read) |
| `bitable` | Multi-dimensional tables: tables, fields, records, views |
| `drive` | Drive files: list, upload, download, import/export |
| `board` | Whiteboard nodes: list, create, batch delete |
| `im` | Messages: send, list, search, reply, reactions, files, find users |
| `calendar` | Events, free/busy, meeting rooms, RSVP |
| `mail` | Mailbox: list, read, send, reply, forward, drafts, search |
| `tasks` | Tasks and tasklists |
| `images` | Download all images from a document/wiki/sheet |
| `cli` | Any lark-cli command (OKR, approval, minutes, slides, VC, ...) through the gateway — see below |
| `login` / `logout` / `whoami` | Session management |
| `init` | Configure Local mode credentials |
| `schema` | Dump all commands/flags as JSON |
| `upgrade` | Self-update the binary |

Run `larkctl <command> --help` for subcommands and flags.

## Full lark-cli surface: `larkctl cli`

`larkctl cli` runs [lark-cli](https://github.com/larksuite/cli) — every service, shortcut, and the generic `api` command — authenticated through your gateway session:

```bash
larkctl cli --help
larkctl cli calendar +agenda
larkctl cli okr +cycle-list
larkctl cli api GET /open-apis/im/v1/chats --params '{"page_size":20}'
```

lark-cli holds no Feishu credential of its own. It runs in its auth-sidecar mode: each request is signed and sent to a loopback sidecar inside larkctl, which relays it to the gateway's `/v1/cli/proxy`, where your user access token is attached. Every call therefore passes the gateway's audit log and its access gates (document security level, IM whitelist).

- **Gateway mode only**, and **user identity only** — `--as bot` is refused.
- **Runtime:** lark-cli's published binaries are built without sidecar support, so larkctl uses its own build of the pinned upstream release. It is downloaded to `~/.lark/bin` on first use (about 45 MB) and verified against the release checksums. To use your own build: `go install -tags authsidecar github.com/larksuite/cli@<version>` and set `LARKCTL_LARK_CLI` to the binary.
- **Scopes:** when Feishu reports a scope your session lacks, larkctl opens the browser to grant it and runs the command again. (With piped input it asks you to re-run instead.)
- lark-cli's local cache lives in `~/.lark/lark-cli`, separate from a standalone lark-cli install. `LARKSUITE_CLI_*` variables from your environment are not passed through.

## Scope groups

Login requests only the scope groups you name (plus a small base set). Available groups: `docs`, `wiki`, `sheets`, `bitable`, `board`, `im`, `mail`, `calendar`, `task`, `contact`, or `all`:

```bash
larkctl login                # default groups
larkctl login docs wiki      # documents + wiki only
larkctl login all            # everything
```

Commands that need scopes you haven't granted trigger a browser re-auth automatically.

## Configuration

| Source | Purpose |
|--------|---------|
| `~/.lark/config.json` | Session token, gateway URL, Local-mode credentials |
| `FEISHU_APP_ID` / `FEISHU_APP_SECRET` | Local-mode credentials via env |
| `FEISHU_GATEWAY_URL` | Gateway base URL |
| `LARKCTL_DOWNLOAD_URL` | Override the `upgrade` download base URL |

See [docs/INSTALL.md](docs/INSTALL.md) for a step-by-step install & login walkthrough (Chinese).

## Development

```bash
go test ./...    # unit tests (no network; Feishu API is mocked)
go vet ./...
```

Releases: pushing a `v*` tag runs `.github/workflows/release.yml`, which builds per-platform archives with `scripts/release.sh` and attaches them to a GitHub Release. `--version` reports the tag injected at build time.

## License

[MIT](LICENSE)
