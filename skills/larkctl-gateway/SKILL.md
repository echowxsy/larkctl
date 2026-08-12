---
name: larkctl-gateway
version: 1.0.0
description: "larkctl-gateway MCP server setup, deployment, tools reference, and security configuration. Trigger when user mentions MCP, gateway setup, Feishu MCP tools, or gateway deployment."
metadata:
  requires:
    bins: ["larkctl-gateway"]
  cliHelp: "larkctl mcp"
---

# larkctl-gateway (MCP Server + Auth Gateway)

## Overview

larkctl-gateway is a centralized OAuth gateway + MCP server for Feishu. It provides:
- **Multi-user session management** with PostgreSQL
- **61 MCP tools** for AI agent integration
- **Document security level enforcement** (L1-L4)
- **IM send/reply whitelist**
- **Audit logging** (all operations logged to PG)

## MCP Endpoints

| Endpoint | Protocol | Usage |
|----------|----------|-------|
| `POST /mcp` | Streamable HTTP (MCP 2025-03-26) | Modern MCP clients |
| `GET /mcp/sse` + `POST /mcp/message` | Legacy SSE | Older MCP clients |

Gateway URL: your deployment's public base URL (e.g. `https://larkmcp.example.com`)

## MCP Tools Reference

### Document Tools
| Tool | Parameters |
|------|-----------|
| `feishu_whoami` | — |
| `get_feishu_document_info` | documentId, documentType |
| `get_feishu_document_blocks` | documentId |
| `create_feishu_document_blocks` | documentId, blockId, children |
| `update_feishu_document_blocks` | documentId, requests |
| `delete_feishu_document_block` | documentId, blockId |
| `search_feishu_docs` | query, count, offset, docTypes |
| `create_feishu_document` | title, folderToken |
| `get_feishu_document_comments` | fileToken, fileType |
| `create_feishu_document_comment` | fileToken, content, fileType |
| `reply_feishu_document_comment` | fileToken, commentId, content, fileType |
| `resolve_feishu_document_comment` | fileToken, commentId, fileType, resolved |

### Data Tools
| Tool | Parameters |
|------|-----------|
| `get_feishu_wiki_node` | token |
| `list_feishu_wiki_spaces` | — |
| `list_feishu_wiki_nodes` | spaceId, parentNodeToken |
| `create_feishu_wiki_node` | spaceId, objType, parentNodeToken, title |
| `get_feishu_sheet_meta` | spreadsheetToken |
| `get_feishu_sheet_values` | spreadsheetToken, sheetId, range, valueRenderOption, dateTimeRenderOption |
| `update_feishu_sheet_values` | spreadsheetToken, range, values |
| `append_feishu_sheet_values` | spreadsheetToken, range, values |
| `get_feishu_bitable_meta` | appToken |
| `list_feishu_bitable_tables` | appToken |
| `list_feishu_bitable_fields` | appToken, tableId |
| `list_feishu_bitable_records` | appToken, tableId, viewId, filter, sort, pageToken |
| `create_feishu_bitable_record` | appToken, tableId, fields |
| `update_feishu_bitable_record` | appToken, tableId, recordId, fields |
| `list_feishu_document_permissions` | token, fileType |
| `list_feishu_drive_files` | folderToken, orderBy, direction |
| `create_feishu_folder` | name, folderToken |
| `import_feishu_drive_file` | — |
| `download_feishu_drive_file` | file_token |

### Collaboration Tools
| Tool | Parameters |
|------|-----------|
| `send_feishu_message` | receive_id, receive_id_type, msg_type, content |
| `list_feishu_messages` | container_id, container_id_type, start_time, end_time, sort_type, page_size, page_token |
| `search_feishu_messages` | query, message_type, chat_id, page_size, page_token |
| `reply_feishu_message` | message_id, msg_type, content |
| `mget_feishu_messages` | message_ids |
| `search_feishu_chats` | query, page_size, page_token |
| `create_feishu_task` | summary, description, due, members |
| `list_feishu_tasks` | page_size, page_token |
| `update_feishu_task` | task_id, summary, description, due |
| `complete_feishu_task` | task_id, completed |
| `comment_feishu_task` | task_id, content |
| `assign_feishu_task_members` | task_id, members |
| `remove_feishu_task_members` | task_id, members |
| `set_feishu_task_reminder` | task_id, action, minutes |
| `create_feishu_tasklist` | name |
| `add_feishu_task_to_tasklist` | task_id, tasklist_id |
| `manage_feishu_tasklist_members` | tasklist_id, action, members, role |
| `get_feishu_calendar_primary` | — |
| `list_feishu_calendar_events` | calendarId, startTime, endTime |
| `create_feishu_calendar_event` | summary, startTime, endTime, description, attendees, room, calendarId |
| `get_feishu_freebusy` | timeMin, timeMax, userId, roomId |
| `search_feishu_rooms` | keyword |
| `rsvp_feishu_calendar_event` | calendar_id, event_id, rsvp_status |
| `search_feishu_users` | query |
| `get_feishu_board_nodes` | whiteboardId |

### Mail Tools
| Tool | Parameters |
|------|-----------|
| `list_feishu_mail` | folder, label, unread, limit, pageToken |
| `read_feishu_mail` | messageId, format |
| `read_feishu_mail_thread` | threadId, format, includeSpamTrash |
| `search_feishu_mail` | query, from, to, folder, hasAttachment, unread, limit, pageToken |
| `list_feishu_mail_folders` | type |

## Deployment

Single static binary behind your reverse proxy, PostgreSQL alongside (Docker Compose setup in the gateway repo's `deploy/`):

```bash
# Build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-w -s' -o /tmp/larkctl-gateway-linux ./cmd/

# Deploy (replace <gateway-host> with your server)
scp /tmp/larkctl-gateway-linux <gateway-host>:/tmp/larkctl-gateway-new
ssh <gateway-host> 'mv /tmp/larkctl-gateway-new ~/larkctl-gateway/larkctl-gateway && cd ~/larkctl-gateway && docker compose restart gateway'
```

## Configuration

Config file: `config.yaml` (YAML + env var overrides)

| Setting | Env Var | Description |
|---------|---------|-------------|
| `listenaddr` | `FEISHU_GATEWAY_LISTEN` | Listen address |
| `publicbaseurl` | `FEISHU_GATEWAY_PUBLIC_URL` | Public URL |
| `databaseurl` | `FEISHU_GATEWAY_DATABASE_URL` | PostgreSQL connection |
| `seclevelbypassusers` | `FEISHU_SEC_BYPASS_USERS` | Security bypass user_ids |
| `imallowedsenders` | `FEISHU_IM_ALLOWED_SENDERS` | IM sender whitelist |
| `imallowedtargets` | `FEISHU_IM_ALLOWED_TARGETS` | IM target whitelist |

Required env vars: `FEISHU_APP_ID`, `FEISHU_APP_SECRET`

## Security

### Document Security Levels
Documents at L3 (Confidential) or above are blocked. Override per-user or per-doc via bypass lists or PG tables (`bypass_users`, `bypass_docs`).

### IM Whitelist
Controlled via config or PG tables (`im_allowed_senders`, `im_allowed_targets`). Empty = allow all. Refreshed every 60 seconds.

### Audit Logging
All operations logged to `audit_logs` table. MCP tool calls logged with tool name and parameters.
