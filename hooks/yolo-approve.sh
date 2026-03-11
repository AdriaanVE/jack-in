#!/bin/sh
# Claude Code PermissionRequest hook -- auto-approve everything (yolo mode).
cat <<'JSON'
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "allow"
    }
  }
}
JSON
