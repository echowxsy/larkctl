---
name: larkctl-shared
version: 1.0.0
description: "Foundation skill for larkctl — authentication, modes, output, security. Required by all other larkctl-* skills."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl --help"
---

# larkctl Shared Foundation

**Critical prerequisite for all larkctl-* skills.** Read this before using any larkctl command.

## Two Operating Modes

larkctl works in **Gateway mode** (default) or **Local mode**:

| Mode | Auth | Use Case |
|------|------|----------|
| Gateway | Device code flow via `larkctl-gateway` server | Teams, headless machines, MCP integration |
| Local | Direct OAuth authorization code flow | Single-user, local development |

### Gateway Mode (default)
- Connects to a gateway server (compiled default: `http://127.0.0.1:8787`)
- Login: `larkctl login` → opens browser → enters user code → session created
- Session stored in `~/.lark/config.json`
- Gateway URL resolution order: `--gateway-url` flag → `FEISHU_GATEWAY_URL` env var → `gateway_url` in `~/.lark/config.json` → the localhost default. A fresh install does **not** point at production until login saves the URL.

### Local Mode
- Direct Feishu API calls, no gateway needed
- Setup: `larkctl init --app-id <id> --app-secret <secret>` (or `FEISHU_APP_ID` / `FEISHU_APP_SECRET`)
- OAuth callback at `http://127.0.0.1:19876/callback`
- Auto-refreshes tokens 5 minutes before expiry

## Authentication

```bash
# Gateway mode
larkctl login                    # Login with default scopes
larkctl login docs wiki sheets   # Login with specific scope groups
larkctl whoami                   # Check current user
larkctl logout                   # Clear session

# Local mode
larkctl init                     # Configure app credentials
larkctl login                    # OAuth login
```

## Scope Groups

Login accepts scope group names. Groups available:

| Group | Covers |
|-------|--------|
| `docs` | Documents, comments, permissions, search, drive |
| `wiki` | Wiki spaces and nodes |
| `sheets` | Spreadsheets read/write |
| `bitable` | Multi-dimensional tables |
| `im` | Messages and chats |
| `board` | Whiteboards |
| `calendar` | Calendar events |
| `contact` | Contacts and user info |
| `task` | Task management |
| `mail` | Mailbox: read, send, drafts, folders, search |

**Dynamic scope upgrade**: If a command needs scopes not yet authorized, larkctl automatically opens a browser for re-authorization.

## Output Formats

```bash
larkctl <command> --format json    # Default: JSON output
larkctl <command> --format table   # Aligned table (for list commands)
larkctl <command> --format csv     # CSV format
larkctl <command> --compact        # Compact JSON (no indentation)
```

## Security Features

### Document Security Levels
Gateway enforces document security level checks (L1-L4). Documents at L3 (Confidential) or above are blocked unless the user is in the bypass list.

### IM Whitelist
Send and reply operations are controlled by whitelist:
- **Sender whitelist**: Only listed user_ids can send/reply messages
- **Target whitelist**: Send only allowed to listed chat_id/open_id targets
- Empty whitelist = allow all (disabled)

Configure via `config.yaml` or PG tables (`im_allowed_senders`, `im_allowed_targets`).

## Common Patterns

```bash
# URL/token input — larkctl auto-extracts tokens from URLs
larkctl docs info "https://xxx.feishu.cn/docx/ABC123"    # URL works
larkctl docs info ABC123                                    # Token works too

# User resolution — names auto-resolved to user_ids
larkctl tasks create "Bug fix" --members "Zhang San"
larkctl calendar create "Meeting" --attendees "12345,Li Si"

# Wiki → Document resolution
# Wiki URLs use wiki tokens; larkctl auto-resolves to document tokens
larkctl docs blocks "https://xxx.feishu.cn/wiki/WIKI_TOKEN"
```

## Utility Commands

```bash
larkctl schema                      # Show all commands, flags, and descriptions
larkctl schema im                   # Show IM commands only
larkctl schema --format json        # Full schema as JSON
larkctl images URL_OR_TOKEN         # Download images from documents/wiki/sheets
larkctl images URL --output-dir ./  # Specify output directory
larkctl upgrade                     # Self-update to latest version
larkctl mcp                         # Show MCP SSE endpoint on gateway
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `FEISHU_GATEWAY_URL` | Gateway server URL |
| `LARKCTL_SESSION_TOKEN` | Override session token |
| `FEISHU_APP_ID` | App ID (local mode) |
| `FEISHU_APP_SECRET` | App Secret (local mode) |
