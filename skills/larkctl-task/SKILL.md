---
name: larkctl-task
version: 1.0.0
description: "Create, list, update, complete, and comment on Feishu tasks. Trigger when user mentions tasks, todo items, or task management in Feishu/Lark."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl tasks --help"
---

# larkctl Task Management

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Commands

### Create a task
```bash
larkctl tasks create "Fix login bug"
larkctl tasks create "Review PR" --description "Check the auth module"
larkctl tasks create "Deploy" --due "2026-04-10"
larkctl tasks create "Bug fix" --members "Zhang San"
larkctl tasks create "Review" --members "Zhang San,follower:Li Si"
```

**Flags:** `--description`, `--due` (YYYY-MM-DD or unix), `--members` (name or user_id, comma-separated. Prefix `follower:` for follower role)

### List my tasks
```bash
larkctl tasks list                    # All tasks (default 50)
larkctl tasks list --limit 10         # Limit results
larkctl tasks list --format table     # Table view
```

### Update a task
```bash
larkctl tasks update TASK_ID --summary "New title"
larkctl tasks update TASK_ID --description "Updated desc"
larkctl tasks update TASK_ID --due "2026-05-01"
```

### Complete / Reopen
```bash
larkctl tasks complete TASK_ID        # Mark as done
larkctl tasks reopen TASK_ID          # Reopen
```

### Comment on a task
```bash
larkctl tasks comment TASK_ID "This is done"
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `create_feishu_task` | Create task (summary, description, due, members) |
| `list_feishu_tasks` | List user's tasks (pagination) |
| `update_feishu_task` | Update task (task_id, summary, description, due) |
| `complete_feishu_task` | Complete or reopen (task_id, completed bool) |
| `comment_feishu_task` | Add comment (task_id, content) |

## Required Scopes

| Scope | Operations |
|-------|-----------|
| `task:task:write` | create, update, complete, reopen |
| `task:comment:write` | comment |

## Member Resolution

Members can be specified by name or user_id:
- `"Zhang San"` → resolved via user search API
- `"12345"` → used directly as user_id
- `"follower:Zhang San"` → added as follower (not assignee)

Task ID is the `guid` field returned by create/list operations.
