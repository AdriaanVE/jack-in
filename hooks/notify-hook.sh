#!/usr/bin/env bash
# jackops notify-hook -- emit signals from non-Claude agents.
# Usage: .jackops/notify-hook.sh <event> [<signal_dir>] [<worker_name>]
#
# Events:
#   active      -- worker is actively working (heartbeat)
#   done        -- task complete (same as touch .done)
#   needs-input -- worker is blocked on user input
#   error       -- worker hit an error
#
# Reads JACKOPS_WORKER_NAME and JACKOPS_SIGNAL_DIR from environment,
# or falls back to positional args.

EVENT="$1"
SIGNAL_DIR="${JACKOPS_SIGNAL_DIR:-$2}"
WORKER_NAME="${JACKOPS_WORKER_NAME:-$3}"

if [ -z "$EVENT" ] || [ -z "$SIGNAL_DIR" ] || [ -z "$WORKER_NAME" ]; then
  echo "Usage: notify-hook.sh <event> [signal_dir] [worker_name]" >&2
  exit 1
fi

case "$EVENT" in
  active)
    touch "${SIGNAL_DIR}/${WORKER_NAME}.heartbeat"
    ;;
  done)
    touch "${SIGNAL_DIR}/${WORKER_NAME}.done"
    ;;
  needs-input)
    echo "${4:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.needs-input"
    ;;
  error)
    echo "${4:-unknown}" > "${SIGNAL_DIR}/${WORKER_NAME}.error"
    ;;
  *)
    echo "Unknown event: $EVENT" >&2
    exit 1
    ;;
esac
