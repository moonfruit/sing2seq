#!/usr/bin/env bash
set -euo pipefail

if [[ "$(basename "$0")" == "sing-box" ]]; then
    : "${SINGBOX:=/opt/homebrew/bin/sing-box}"
else
    : "${SINGBOX:=sing-box}"
fi

: "${SING2SEQ:=}"

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
    if [[ -z $SING2SEQ ]]; then
        SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        SING2SEQ="${SCRIPT_DIR}/sing2seq"
    fi

    exec {fd}> >(tee /dev/stderr | "$SING2SEQ")
    pid=$!
    "$SINGBOX" "$@" 2>&${fd} || rc=$?
    exec {fd}>&-
    wait "$pid" 2>/dev/null || true
    exit "$rc"
fi

exec "$SINGBOX" "$@"
