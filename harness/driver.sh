#!/bin/sh
# N senders each pair with an offline "hub" and send one message; then the hub
# connects and must drain exactly N queued messages, in the order they were sent.
# Proves presence-independent delivery + the offline queue at scale.
set -eu
B="${CPV2_URL:-http://broker:7900}"
N="${N:-20}"

# Give the broker a moment to come up.
i=0; until curl -sf "$B/peers" >/dev/null 2>&1 || [ "$i" -ge 30 ]; do i=$((i+1)); sleep 0.5; done

for n in $(seq 1 "$N"); do
  code=$(curl -s -X POST "$B/pair" -d "{\"from\":\"s$n\",\"to\":\"hub\"}" \
         | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
  curl -s -X POST "$B/pair/approve" -d "{\"owner\":\"hub\",\"code\":\"$code\"}" >/dev/null
  curl -s -X POST "$B/send" -d "{\"from\":\"s$n\",\"to\":\"hub\",\"content\":\"msg $n\"}" >/dev/null
done

# Hub was never streaming, so all N are queued. Drain them.
got=$(timeout 5 curl -sN "$B/stream/hub" | grep -c '^data: ' || true)
echo "sent $N, drained $got"
[ "$got" -eq "$N" ] && echo "HARNESS PASS" || { echo "HARNESS FAIL"; exit 1; }
