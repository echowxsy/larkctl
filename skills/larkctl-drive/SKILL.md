---
name: larkctl-drive
version: 1.0.0
description: "Upload, download, import, export files in Feishu Drive. Trigger when user mentions file upload/download, drive operations, or document import/export."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl drive --help"
---

# larkctl Drive Operations

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Commands

### List and organize
```bash
larkctl drive list                             # Root folder
larkctl drive list FOLDER_TOKEN                # Specific folder
larkctl drive list --format table              # Table view
larkctl drive mkdir PARENT_TOKEN "New Folder"  # Create folder
```

### Upload and download
```bash
larkctl drive upload report.pdf --folder FOLDER_TOKEN
larkctl drive download FILE_TOKEN ./output.pdf
larkctl drive download FILE_TOKEN ./downloads/    # Directory target
```

### Import (local file → cloud document)
```bash
larkctl drive import report.docx --folder FOLDER_TOKEN           # Auto-detect type
larkctl drive import data.xlsx --folder FOLDER_TOKEN --type sheet # Explicit type
larkctl drive import notes.md --folder FOLDER_TOKEN              # Markdown → docx
```

**Supported import formats:**
| Extension | Target Type |
|-----------|------------|
| .docx, .doc, .txt, .md, .markdown, .html | docx |
| .xlsx, .xls, .csv | sheet |

Import is async (upload → create task → poll until done).

### Export (cloud document → local file)
```bash
larkctl docs export URL_OR_TOKEN --format docx  # Export document
larkctl sheets export SPREADSHEET_TOKEN         # Export spreadsheet
```

Export uses the existing `docs export` / `sheets export` commands.

### Download images from documents
```bash
larkctl images URL_OR_TOKEN                    # Download all images to current dir
larkctl images URL_OR_TOKEN --output-dir ./img # Specify output directory
```

Works with documents, wiki pages, and spreadsheets. Extracts and downloads all embedded images.

## MCP Tools

| Tool | Description |
|------|-------------|
| `list_feishu_drive_files` | List files in folder |
| `create_feishu_folder` | Create folder |
| `download_feishu_drive_file` | Get download URL |
| `import_feishu_drive_file` | Import file (3-step flow info) |

## Required Scopes

| Scope | Operations |
|-------|-----------|
| `drive:file:upload` | upload |
| `drive:file:download` | download |
| `docs:document.media:upload` | import (media upload step) |
| `docs:document:import` | import (create task step) |
| `docs:document:export` | export |
