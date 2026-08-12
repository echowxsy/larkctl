---
name: larkctl-calendar
version: 1.0.0
description: "Manage Feishu calendar events — list, create, RSVP, check availability, book rooms. Trigger when user mentions calendar, meetings, events, scheduling, or room booking."
metadata:
  requires:
    bins: ["larkctl"]
  cliHelp: "larkctl calendar --help"
---

# larkctl Calendar

**Prerequisite:** Read `../larkctl-shared/SKILL.md` first.

## Commands

### View events
```bash
larkctl calendar list                                      # Today's events
larkctl calendar list --days 7                             # Next 7 days
larkctl calendar list --start 2026-04-01 --end 2026-04-07 # Date range
larkctl calendar list --format table                       # Table view
larkctl calendar primary                                   # Primary calendar info
```

### Create events
```bash
larkctl calendar create "Team sync" --start "2026-04-05 14:00" --end "2026-04-05 15:00"
larkctl calendar create "Review" --start "2026-04-05 10:00" --end "2026-04-05 11:00" \
  --attendees "Zhang San,Li Si"
larkctl calendar create "Meeting" --start "..." --end "..." --room "1604"
larkctl calendar create "Meeting" --start "..." --end "..." --room omm_xxx
```

**Flags:** `--start`, `--end` (required, YYYY-MM-DD HH:MM or unix), `--description`, `--attendees` (names or user_ids), `--room` (room name or ID)

Room availability is checked before booking. If occupied, the command fails with a suggestion to search rooms.

### Check availability
```bash
larkctl calendar freebusy --user 12345 --start 2026-04-05 --end 2026-04-06
larkctl calendar freebusy --room omm_xxx --start "2026-04-05 09:00" --end "2026-04-05 18:00"
```

### Search meeting rooms
```bash
larkctl calendar rooms              # All rooms
larkctl calendar rooms "1604"       # Search by keyword
larkctl calendar rooms "C3"         # Search by building/floor
```

### RSVP to invitations
```bash
larkctl calendar rsvp EVENT_ID accept
larkctl calendar rsvp EVENT_ID decline
larkctl calendar rsvp EVENT_ID tentative
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `get_feishu_calendar_primary` | Primary calendar info |
| `list_feishu_calendar_events` | List events (calendar_id, start/end time) |
| `create_feishu_calendar_event` | Create event (summary, start/end, attendees, room) |
| `get_feishu_freebusy` | Check user/room availability |
| `search_feishu_rooms` | Search meeting rooms |
| `rsvp_feishu_calendar_event` | RSVP (calendar_id, event_id, status) |

## Required Scopes

| Scope | Operations |
|-------|-----------|
| `calendar:calendar` | All calendar operations |
| `vc:room:readonly` | Room search and booking |

## Time Formats

Commands accept:
- `YYYY-MM-DD` — start of day
- `YYYY-MM-DD HH:MM` — specific time
- Unix timestamp (seconds)

Attendees accept names (auto-resolved) or user_ids.
