#!/bin/sh
set -eu

APP_DIR="/app"

if [ ! -f "$APP_DIR/config.json" ]; then
  echo "Missing config.json at $APP_DIR/config.json" >&2
  exit 1
fi

if [ ! -f "$APP_DIR/provider_secrets.json" ]; then
  echo "Warning: provider_secrets.json not found; OAuth providers will be unavailable." >&2
fi

( cd "$APP_DIR/modules/menu" && ./menu ) &
MENU_PID=$!

( cd "$APP_DIR/modules/cameraview" && ./cameraview ) &
CAM_PID=$!

( cd "$APP_DIR" && ./authentigate ) &
AUTH_PID=$!

trap 'kill "$MENU_PID" "$CAM_PID" "$AUTH_PID" 2>/dev/null || true; wait; exit 0' INT TERM

wait "$AUTH_PID"

kill "$MENU_PID" "$CAM_PID" 2>/dev/null || true
wait
