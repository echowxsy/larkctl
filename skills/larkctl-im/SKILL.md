---
name: larkctl-im
version: 1.0.0
description: "Send, read, search, and reply to Feishu IM messages via larkctl. Trigger when user mentions sending messages, reading chat history, or searching messages in Feishu/Lark."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl im --help"
---

# larkctl IM (Instant Messaging)

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Commands

### Send a message
```bash
larkctl im send "hello" --to oc_xxx                    # To group chat
larkctl im send "hello" --to ou_xxx                    # To user (DM)
larkctl im send "hello" --to ou_xxx --type open_id     # Explicit ID type
larkctl im send --file message.json --to oc_xxx        # Complex message from file
larkctl im send --text-file msg.txt --to oc_xxx        # Long/multiline text (no shell quoting)
larkctl im send "hello" --to oc_xxx --msg-type post    # Rich text (post format)
```

**Flags:**
| Flag | Required | Description |
|------|----------|-------------|
| `--to` | Yes | Recipient: chat_id (oc_xxx) or user open_id (ou_xxx) |
| `--type` | No | ID type: chat_id, open_id (auto-detected from --to prefix) |
| `--msg-type` | No | Message type: text (default), post, interactive |
| `--file` | No | JSON file for complex message content |
| `--text-file` | No | Plain-text file sent as a text message (avoids shell quoting/escaping) |

**Auto-detection:** `oc_` prefix → `chat_id`, otherwise → `open_id`.

### Find a user (name → open_id)
```bash
larkctl im find 张三                # Returns user_id/open_id/name matches
larkctl im find 张 --limit 20       # More results (max 20)
```

The returned `open_id` feeds `im send --to` / `im send-file --to` directly —
no lark-cli round-trip needed. Requires `contact:user:search` scope
(first use auto-triggers browser reauth).

### Send a file or image
```bash
larkctl im send-file report.pdf --to oc_xxx        # File message (file_type by extension)
larkctl im send-file screen.png --to ou_xxx        # Image message (auto-detected)
larkctl im send-file data.bin --to oc_xxx --as file --name results.bin
```

Uploads via `im/v1/files` (30MB max) or `im/v1/images` (10MB max), then sends
`msg_type=file|image`. Image extensions (jpg/png/gif/webp/bmp/tiff) auto-send
as image; `--as file` forces a file message. Requires `im:resource` scope
(first use auto-triggers browser reauth).

### List messages in a chat
```bash
larkctl im list oc_xxx                          # Latest 20 messages
larkctl im list oc_xxx --limit 50               # More messages
larkctl im list oc_xxx --sort ByCreateTimeAsc   # Oldest first
larkctl im list oc_xxx --format table           # Table view
```

### Search messages
```bash
larkctl im search "deployment"                  # Search across all chats
larkctl im search "bug" --chat oc_xxx           # Search in specific chat
larkctl im search "release" --limit 10
```

### Reply to a message
```bash
larkctl im reply om_xxx "got it"                # Reply to message ID
```

### Emoji reactions (the OK/Yes/No chips on a message)
```bash
larkctl im react om_xxx ok                      # Add reaction (aliases: ok,yes,no,+1,-1,done,lgtm,heart,fire,...)
larkctl im react om_xxx THUMBSUP                # Or any of the ~185 official emoji_type values
larkctl im reactions om_xxx                     # List reactions (who reacted with what, reaction_id)
larkctl im unreact om_xxx <reaction_id>         # Remove a reaction you added
```

Emoji in normal text messages: just include unicode emoji (😄👍) in the text —
no special syntax needed.

## MCP Tools (via larkctl-gateway)

| Tool | Description |
|------|-------------|
| `send_feishu_message` | Send message (receive_id, receive_id_type, msg_type, content) |
| `list_feishu_messages` | List chat messages (container_id, start/end_time, sort, pagination) |
| `search_feishu_messages` | Search messages (query, message_type, chat_id, pagination) |
| `reply_feishu_message` | Reply to message (message_id, msg_type, content) |

## Required Scopes

| Scope | Operations |
|-------|-----------|
| `im:message` | send, send-file, reply, react, unreact |
| `im:resource` | send-file (upload) |
| `contact:user:search` | find |
| `im:message.group_msg:get_as_user` | list (group chats) |
| `im:message.p2p_msg:get_as_user` | list (DMs) |
| `search:message` | search |

## Identity

All operations use **user identity** (user_access_token). Messages are sent as the logged-in user, not as a bot. No bot needs to join the chat.

## IM Whitelist

Gateway enforces sender + target whitelist. If denied, you'll see `im_whitelist_denied` error. Contact the gateway admin to be added.

## Message Content Format

For `text` type, content is: `{"text": "hello"}`

For `post` type, content follows Feishu post format:
```json
{"zh_cn": {"title": "Title", "content": [[{"tag": "text", "text": "content"}]]}}
```

For `interactive` type, content is a Feishu card JSON.
