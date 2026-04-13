#!/usr/bin/env bash
set -euo pipefail

if [[ "$(basename "$0")" == "sing-box" ]]; then
    : "${SINGBOX:=/opt/homebrew/bin/sing-box}"
else
    : "${SINGBOX:=sing-box}"
fi

is_run=0
for arg in "$@"; do
    case "$arg" in
    -*) continue ;;
    run)
        is_run=1
        break
        ;;
    *) break ;;
    esac
done

if ((is_run)); then
    "$SINGBOX" "$@" 2> >(tee /dev/stderr | sing2seq)
    echo "running"
    exit $?
fi

exec "$SINGBOX" "$@"
