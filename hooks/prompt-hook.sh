#!/usr/bin/env bash
# Prompt submit hook -- clear needs-input and refresh heartbeat.
# Args: <signal_dir> <worker_name>

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

rm -f "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
