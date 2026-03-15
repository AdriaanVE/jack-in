#!/usr/bin/env bash
# Session end hook -- signal that the worker process exited.
# Args: <signal_dir> <worker_name>
# Stdin: JSON with reason field

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

INPUT=$(cat)
REASON=$(echo "$INPUT" | grep -o '"reason"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"reason"[[:space:]]*:[[:space:]]*"//;s/".*//')

# Atomic write: tmp file + rename for payload integrity
TMPFILE="${SIGNAL_DIR}/${WORKER_NAME}.exited.tmp"
echo "${REASON:-unknown}" > "$TMPFILE"
mv "$TMPFILE" "${SIGNAL_DIR}/${WORKER_NAME}.exited"
