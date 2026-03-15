#!/usr/bin/env bash
# Heartbeat hook -- touch file to prove worker is active.
# Args: <signal_dir> <worker_name>

SIGNAL_DIR="$1"
WORKER_NAME="$2"

if [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  exit 0
fi

touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
