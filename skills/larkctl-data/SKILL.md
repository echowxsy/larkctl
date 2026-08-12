---
name: larkctl-data
version: 1.0.0
description: "Access Feishu spreadsheets, bitable (multi-dimensional tables), and wiki. Trigger when user mentions sheets, spreadsheets, bitable, tables, records, or wiki."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl sheets --help"
---

# larkctl Data (Sheets, Bitable, Wiki)

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Spreadsheets

```bash
larkctl sheets meta SPREADSHEET_TOKEN                              # Sheet metadata
larkctl sheets values SPREADSHEET_TOKEN SHEET_ID "A1:D10"          # Read range (range is required)
larkctl sheets update SPREADSHEET_TOKEN values.json                # Write values (JSON from file, or - for stdin)
larkctl sheets append SPREADSHEET_TOKEN values.json                # Append rows (JSON from file, or - for stdin)
larkctl sheets export SPREADSHEET_TOKEN                            # Export to xlsx
larkctl sheets export SPREADSHEET_TOKEN --output ./data.xlsx       # Specify output path
```

## Bitable (Multi-dimensional Tables)

```bash
larkctl bitable meta APP_TOKEN                                         # App metadata
larkctl bitable tables APP_TOKEN                                       # List tables
larkctl bitable tables APP_TOKEN --format table                        # Table view
larkctl bitable fields APP_TOKEN TABLE_ID                              # List fields
larkctl bitable records APP_TOKEN TABLE_ID                             # List records
larkctl bitable records APP_TOKEN TABLE_ID --filter '...'              # Filter records
larkctl bitable records APP_TOKEN TABLE_ID --view-id VIEW_ID           # Records from specific view
larkctl bitable create-record APP_TOKEN TABLE_ID record.json           # Create (JSON from file, or - for stdin)
larkctl bitable update-record APP_TOKEN TABLE_ID RECORD_ID record.json  # Update
```

**Flags for `bitable records`:**
| Flag | Description |
|------|-------------|
| `--filter` | Filter expression (Feishu bitable filter syntax) |
| `--view-id` | View ID to scope records |

## Wiki

```bash
larkctl wiki spaces                              # List all spaces
larkctl wiki nodes SPACE_ID                      # List root nodes
larkctl wiki nodes SPACE_ID --parent NODE_TOKEN  # List child nodes
larkctl wiki node URL_OR_TOKEN                   # Get node info
larkctl wiki create-node SPACE_ID --title "Page" --type doc          # Create document node
larkctl wiki create-node SPACE_ID --title "Page" --parent NODE_TOKEN # Create under parent
```

**Flags for `wiki create-node`:**
| Flag | Description |
|------|-------------|
| `--title` | Node title |
| `--type` | Node type (doc, sheet, etc.) |
| `--parent` | Parent node token |

## MCP Tools

### Sheets
| Tool | Description |
|------|-------------|
| `get_feishu_sheet_meta` | Sheet metadata |
| `get_feishu_sheet_values` | Read cell values |
| `update_feishu_sheet_values` | Write cell values |
| `append_feishu_sheet_values` | Append rows |

### Bitable
| Tool | Description |
|------|-------------|
| `get_feishu_bitable_meta` | App metadata |
| `list_feishu_bitable_tables` | List tables |
| `list_feishu_bitable_fields` | List fields |
| `list_feishu_bitable_records` | List records (with filter/sort) |
| `create_feishu_bitable_record` | Create record |
| `update_feishu_bitable_record` | Update record |

### Wiki
| Tool | Description |
|------|-------------|
| `get_feishu_wiki_node` | Node info |
| `list_feishu_wiki_spaces` | List spaces |
| `list_feishu_wiki_nodes` | List nodes |
| `create_feishu_wiki_node` | Create node |

## Required Scopes

| Scope | Operations |
|-------|-----------|
| `sheets:spreadsheet` | Sheets read/write |
| `bitable:app` | Bitable read/write |
| `wiki:wiki` | Wiki read/write |
