# Remind - Agent Integration Guide

**CLI:** `./bin/remind` (or `go run ./cmd/remind` from project dir)

## Quick Reference

| Action | Command |
|--------|---------|
| List reminders | `remind list` |
| Create reminder | `remind create -m "message" -d "2026-02-22T15:00:00Z"` |
| Delete reminder | `remind delete <id>` |

## Create Reminder

```bash
remind create -m "Task name" -d "2026-02-22T15:00:00Z"
```

**Required:** `-m` (message), `-d` (datetime in RFC3339)

**Optional:**
- `-f P1D` - follow-up interval (ISO 8601: `P1D`=day, `PT1H`=hour, `P1W`=week)
- `-n -1` - max follow-ups (`-1`=infinite, `0`=disabled)

## List Reminders

```bash
remind list                    # all reminders
remind list --status pending   # filter by status
remind list --search "task"   # search messages
```

## Delete Reminder

```bash
remind delete <id>
```
