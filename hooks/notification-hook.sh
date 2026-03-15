#!/usr/bin/env bash
# Notification hook -- signals that the worker needs input.
# Args: <signal_dir> <worker_name>
# Stdin: JSON with notification_type, message, etc.

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

# Write needs-input signal with notification type as content
INPUT=$(cat)
NOTIFICATION_TYPE=$(echo "$INPUT" | grep -o '"notification_type"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"notification_type"[[:space:]]*:[[:space:]]*"//;s/".*//')

echo "${NOTIFICATION_TYPE:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
