#!/usr/bin/env bash
# Start or reuse a per-session `ww` server that serves HTML answers.
#
# Prints to stdout exactly three KEY=VALUE lines:
#   DIR=<dir to put .html files in>
#   URL=<base url, e.g. http://localhost:5074/>
#   STATUS=reused|started
#
# One temp dir and one server are reused across calls. `ww` has a 30m idle
# timeout and shuts itself down; there is no keep-alive loop. If the server has
# died, the next call starts a fresh one (new port). See SKILL.md.
set -euo pipefail

STATE="${TMPDIR:-/tmp}/claude-ww-$(id -u)"
SITE="$STATE/site"
PIDFILE="$STATE/pid"
URLFILE="$STATE/url"
LOGFILE="$STATE/log"
TIMEOUT="30m"

mkdir -p "$SITE"

# Reuse if a server is up and answering on the recorded URL.
if [[ -s "$URLFILE" ]]; then
  url=$(cat "$URLFILE")
  if curl -fsS --max-time 1 -o /dev/null "$url" 2>/dev/null; then
    printf 'DIR=%s\nURL=%s\nSTATUS=reused\n' "$SITE" "$url"
    exit 0
  fi
fi

# Start a fresh server. `ww` serves its current directory, so launch from $SITE.
: > "$LOGFILE"
(
  cd "$SITE"
  nohup ww -timeout "$TIMEOUT" >"$LOGFILE" 2>&1 &
  echo $! >"$PIDFILE"
)

# Wait (up to ~5s) for ww to print its URL.
url=""
for _ in $(seq 1 50); do
  url=$(grep -oE 'http://[^ ]+' "$LOGFILE" 2>/dev/null | head -1 || true)
  [[ -n "$url" ]] && break
  sleep 0.1
done

if [[ -z "$url" ]]; then
  echo "ERROR: ww did not report a URL within 5s" >&2
  cat "$LOGFILE" >&2 || true
  exit 1
fi

printf '%s' "$url" > "$URLFILE"
printf 'DIR=%s\nURL=%s\nSTATUS=started\n' "$SITE" "$url"
