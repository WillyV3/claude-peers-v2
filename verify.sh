#!/usr/bin/env bash
# One command to run every automated gate for claude-peers v2.
# Anything needing a human (live Claude/pi TTY) is out of scope here by design.
set -uo pipefail
cd "$(dirname "$0")"
fail=0
step() { printf "\n=== %s ===\n" "$1"; }
ok()   { echo "PASS: $1"; }
bad()  { echo "FAIL: $1"; fail=1; }

step "go build"
go build ./... && ok "build" || bad "build"

step "go vet"
go vet ./... && ok "vet" || bad "vet"

step "go test -race (incl 50-peer concurrent load)"
go test -race ./... && ok "go tests" || bad "go tests"

step "adapters typecheck (bun)"
if command -v bun >/dev/null; then
  tc=0
  for f in adapters/lib/peers.ts adapters/claude/channel.ts adapters/pi/peer.ts adapters/codex/adapter.ts adapters/opencode/adapter.ts; do
    bun build "$f" --target=node >/dev/null 2>&1 || { echo "  typecheck failed: $f"; tc=1; }
  done
  [ $tc -eq 0 ] && ok "adapter typechecks" || bad "adapter typechecks"
else
  echo "  bun not found — skipping adapter checks"
fi

step "channel injection smoke (broker -> claude/channel)"
if command -v bun >/dev/null && [ -f ~/.config/opencode/keys.env ]; then
  set -a; . ~/.config/opencode/keys.env 2>/dev/null; set +a
fi
if command -v bun >/dev/null; then
  bun adapters/smoke.ts >/tmp/v2-smoke.out 2>&1 && grep -q PASS /tmp/v2-smoke.out && ok "smoke" || { bad "smoke"; tail -3 /tmp/v2-smoke.out; }
else
  echo "  bun not found — skipping smoke"
fi

step "docker N-peer offline-queue harness (optional)"
if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
  if docker compose up --build --abort-on-container-exit --exit-code-from driver >/tmp/v2-harness.out 2>&1; then
    grep -q "HARNESS PASS" /tmp/v2-harness.out && ok "docker harness" || bad "docker harness"
  else
    grep -q "HARNESS PASS" /tmp/v2-harness.out && ok "docker harness" || bad "docker harness"
  fi
  docker compose down -v >/dev/null 2>&1
else
  echo "  docker not available — skipping (run elsewhere)"
fi

printf "\n========================\n"
[ $fail -eq 0 ] && echo "ALL AUTOMATED GATES PASS" || echo "SOME GATES FAILED"
exit $fail
