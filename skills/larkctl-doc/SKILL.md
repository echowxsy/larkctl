---
name: larkctl-doc
version: 1.0.0
description: "Read, create, edit Feishu documents and wiki pages. Trigger when user mentions Feishu docs, wiki, document blocks, comments, or document permissions."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl docs --help"
---

# larkctl Document Operations

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Commands

### Read documents
```bash
larkctl docs info URL_OR_TOKEN                 # Document metadata
larkctl docs blocks URL_OR_TOKEN               # Read all blocks (content)
larkctl docs search "keyword"                  # Search documents
larkctl docs permissions URL_OR_TOKEN          # View permissions
larkctl docs comments URL_OR_TOKEN             # List comments
```

### Create and edit
```bash
larkctl docs create "New Doc" --folder-token FOLDER_TOKEN
larkctl docs create-blocks DOC_ID blocks.json --block-id BLOCK_ID   # JSON body from file, or - for stdin
larkctl docs update DOC_ID doc.md             # Update from markdown file (diff-based, preserves comments)
larkctl docs delete-block DOC_ID BLOCK_ID
```

### Export
```bash
larkctl docs export URL_OR_TOKEN --format docx         # Export as docx
larkctl docs export URL_OR_TOKEN --format pdf           # Export as pdf
larkctl docs export URL_OR_TOKEN --format markdown      # Export as markdown
larkctl docs export URL_OR_TOKEN --output ./doc.docx    # Specify output path
larkctl docs export URL_OR_TOKEN --image-dir ./images   # Markdown only: image path prefix (no download)
```

### Comments
```bash
larkctl docs add-comment FILE_TOKEN "note text"
larkctl docs reply-comment FILE_TOKEN COMMENT_ID "reply text"
larkctl docs resolve-comment FILE_TOKEN COMMENT_ID
```

### Wiki
```bash
larkctl wiki spaces                            # List wiki spaces
larkctl wiki nodes SPACE_ID                    # List nodes in space
larkctl wiki node URL_OR_TOKEN                 # Get wiki node info
larkctl wiki create-node SPACE_ID --title "Page" --type docx --parent NODE_TOKEN
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `get_feishu_document_info` | Document metadata |
| `get_feishu_document_blocks` | Read document content (blocks) |
| `create_feishu_document_blocks` | Add blocks to document |
| `update_feishu_document_blocks` | Update document blocks |
| `delete_feishu_document_block` | Delete a block |
| `search_feishu_docs` | Search documents |
| `create_feishu_document` | Create new document |
| `get_feishu_document_comments` | List comments |
| `create_feishu_document_comment` | Add comment |
| `reply_feishu_document_comment` | Reply to comment |
| `resolve_feishu_document_comment` | Resolve comment |
| `get_feishu_wiki_node` | Get wiki node |
| `list_feishu_wiki_spaces` | List wiki spaces |
| `list_feishu_wiki_nodes` | List nodes in space |
| `create_feishu_wiki_node` | Create wiki node |

## Key Concepts

### URL → Token Resolution
larkctl auto-extracts tokens from Feishu URLs:
- `https://xxx.feishu.cn/docx/ABC123` → token `ABC123`
- `https://xxx.feishu.cn/wiki/WIKI_TOKEN` → resolves via wiki API → document token

### Wiki Node Resolution
Wiki URLs need extra resolution: wiki token → `wiki/v2/spaces/get_node` → `obj_token` (actual document token). larkctl handles this automatically.

### Block Type Limitations
Some block types fail on `batch_create`:
- Bullet list (type 16), Ordered list (type 17), Heading3 (type 5) — **will fail**
- Use text blocks or Heading4 (type 6) instead

### Document Security
Gateway checks document security levels (L1-L4). L3+ documents are blocked unless user/doc is in the bypass list.
