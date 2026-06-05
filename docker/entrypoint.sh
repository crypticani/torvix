#!/bin/sh
set -eu

log_dir="${TORVIX_LOG_DIR:-/app/logs}"
case "$log_dir" in
  /*) ;;
  *) log_dir="/app/$log_dir" ;;
esac

mkdir -p "$log_dir"
chown -R torvix:torvix "$log_dir"

exec su-exec torvix:torvix /app/torvix "$@"
