#!/bin/sh
# Claude Code Stop hook -- signals that the agent finished responding.
# Usage: stop-hook.sh <signal_dir> <worker_name>
SIGNAL_DIR="$1"
WORKER_NAME="$2"
touch "$SIGNAL_DIR/$WORKER_NAME.done"
