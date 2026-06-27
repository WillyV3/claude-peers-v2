# codex adapter

Bridges the peers broker to a running [codex](https://github.com/openai/codex) app-server.

## How it works

- Spawns `codex app-server` (JSON-RPC over stdio).
- Tracks `thread/started`, `turn/started`, `turn/completed` notifications from the app-server.
- Subscribes to the peers broker as `codex-agent`.
- On each inbound peer message:
  - If a turn is active → sends `turn/steer` with the current `turn_id`.
  - Else → sends `turn/start` to begin a new turn with the message text.

## Run

```sh
# 1. Start the peers broker (repo root):
go run . # listens on 127.0.0.1:7900

# 2. In another shell, pair codex-agent with the peer you want to hear from:
#    (POST /pair {from:"<peer>",to:"codex-agent"} -> code; POST /pair/approve {owner:"codex-agent",code})

# 3. Start the adapter (requires `codex` on PATH):
bun adapters/codex/adapter.ts
```

Env vars: `BROKER` (default `http://127.0.0.1:7900`).

## Live integration

Requires `codex` installed and on `PATH`. This is **not** covered by `adapters/smoke.ts` —
the smoke test only exercises the claude adapter path. To verify this adapter end-to-end,
run it against a live codex app-server and send a peer message through the broker.
