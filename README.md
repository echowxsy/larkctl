# larkctl

[![ci](https://github.com/echowxsy/larkctl/actions/workflows/ci.yml/badge.svg)](https://github.com/echowxsy/larkctl/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/echowxsy/larkctl)](https://github.com/echowxsy/larkctl/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/echowxsy/larkctl.svg)](https://pkg.go.dev/github.com/echowxsy/larkctl)
[![license](https://img.shields.io/github/license/echowxsy/larkctl)](LICENSE)

Command-line client for Feishu/Lark. Reads and writes documents, wiki, sheets, bitable, drive, whiteboards, IM messages, calendar, mail, and tasks — with per-user OAuth, so every action runs as the logged-in user (not a bot).

Works in two modes through the same commands:

| Mode | Auth | When to use |
|------|------|-------------|
| **Gateway** | Device-code flow against a `larkctl-gateway` server (companion project) | Teams, headless machines, MCP clients; app credentials stay on the gateway |
| **Local** | OAuth authorization-code flow directly against Feishu | Solo use with your own Feishu app `app_id`/`app_secret` |

## Features

- Per-user OAuth (user access tokens, auto-refreshed) — not tenant/bot tokens
- Document → Markdown export, Markdown → document import, image download
- Scope groups: request only the permissions you need (`larkctl login docs wiki`)
- Dynamic scope upgrade: commands needing extra scopes trigger re-auth automatically
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
| `docs` | Documents: info, blocks, create, export (pdf/docx/md), update from Markdown (diff-based), comments, permissions, search |
| `wiki` | Wiki spaces and nodes |
| `sheets` | Spreadsheets (read) |
| `bitable` | Multi-dimensional tables: tables, fields, records |
| `drive` | Drive files: list, upload, download, import/export |
| `board` | Whiteboard nodes |
| `im` | Messages: send, list, search, reply, reactions, files, find users |
| `calendar` | Events, free/busy, meeting rooms, RSVP |
| `mail` | Mailbox: list, read, send, reply, forward, drafts, search |
| `tasks` | Tasks and tasklists |
| `images` | Download all images from a document/wiki/sheet |
| `login` / `logout` / `whoami` | Session management |
| `init` | Configure Local mode credentials |
| `schema` | Dump all commands/flags as JSON |
| `upgrade` | Self-update the binary |

Run `larkctl <command> --help` for subcommands and flags.

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
