#!/usr/bin/env bash
# Send a daemon's briefing to an agent on the peers network.
# The whole integration is one POST. Pair once (see README), then run this from a
# systemd timer. If the recipient is asleep, the broker queues it and delivers on reconnect.
set -euo pipefail

FROM="${PEER_FROM:-daemon}"            # this automation's agent name (must be paired with TO)
TO="${PEER_TO:-keeper}"                # who receives the briefing
BRIEFING="${1:-$HOME/.claude-daemon/briefings/$(date +%F).md}"

[ -f "$BRIEFING" ] || { echo "no briefing at $BRIEFING" >&2; exit 1; }

# Condensed rollup: first 20 lines + a pointer to the full file. One message, not a flood.
summary="$(head -n 20 "$BRIEFING")
→ full: cat $BRIEFING"

# Option A — the CLI (ergonomic):
cpv2 send --from "$FROM" --to "$TO" "$summary"

# Option B — no binary on PATH? raw POST does the same:
# curl -fsS -X POST "${CPV2_URL:-http://127.0.0.1:7900}/send" \
#   -d "$(jq -n --arg f "$FROM" --arg t "$TO" --arg c "$summary" \
#         '{from:$f,to:$t,content:$c,deliverAs:"steer"}')"
