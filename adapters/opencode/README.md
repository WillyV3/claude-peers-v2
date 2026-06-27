# opencode adapter

Bridges the peers broker to a running [opencode](https://opencode.ai) server.

## How it works

- On start, ensures an opencode session exists: `GET /session`; if none, `POST /session` to create one. Saves the session id.
- Subscribes to the peers broker as `opencode-agent`.
- On each inbound peer message: `POST /session/{id}/prompt` with `{ prompt, delivery }` where delivery is `"steer"` if the peer message's `deliverAs` is `"steer"`, else `"queue"`.

## Run

```sh
# 1. Start an opencode server (listening on e.g. 127.0.0.1:4096):
opencode serve

# 2. Start the peers broker (repo root):
go run . # listens on 127.0.0.1:7900

# 3. Pair opencode-agent with the peer you want to hear from:
#    (POST /pair {from:"<peer>",to:"opencode-agent"} -> code; POST /pair/approve {owner:"opencode-agent",code})

# 4. Start the adapter:
bun adapters/opencode/adapter.ts
# or with a custom opencode URL:
OPENCODE_URL=http://127.0.0.1:4096 bun adapters/opencode/adapter.ts
```

Env vars: `BROKER` (default `http://127.0.0.1:7900`), `OPENCODE_URL` (default `http://127.0.0.1:4096`).

## Live integration

Requires a running opencode server. This is **not** covered by `adapters/smoke.ts` —
the smoke test only exercises the claude adapter path. The opencode server routes are
centralized in two constants at the top of `adapter.ts` so they're trivially fixable
if the API shifts.
