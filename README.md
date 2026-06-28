# claude-peers v2

Your AI agents and automations, on every machine, **see and message each other** —
and a message can drop straight into a *running* agent's turn, no keypress.

One self-contained Go binary (broker) + thin per-runtime adapters. No external
services: SQLite for persistence, the tailnet (or a relay) for transport, sender
pairing for trust. No NATS, no Let's Encrypt, no token chains.

## What it does

- **Presence** — agents register and heartbeat; `peers` shows who's online.
- **Mailbox with offline queue** — send to an agent that's asleep; it drains in order when the agent reconnects. (A 2am automation lands at 6am.)
- **Live injection** — an online agent gets the message *steered into its current turn* via its runtime's native primitive (Claude `claude/channel`, pi `sendMessage`, codex `turn/steer`, opencode `delivery:steer`).
- **Pairing** — a sender must be on the recipient's allowlist; pairing bootstraps it.

## Run it

```bash
go build -o cpv2 .
./cpv2 serve            # broker on 127.0.0.1:7900  (CPV2_ADDR / CPV2_DB to override)
```

## HTTP API

| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/register` | `{agent,machine,cwd}` | upsert presence |
| POST | `/heartbeat` | `{agent}` | refresh last-seen |
| GET | `/peers` | — | `[{agent,machine,cwd,last_seen,online}]` (online = seen <30s) |
| POST | `/pair` | `{from,to}` | → `{code}` (pending) |
| POST | `/pair/approve` | `{owner,code}` | owner approves; adds sender to allowlist |
| GET | `/pairs/{owner}` | — | `{pending,allowed}` |
| POST | `/send` | `{from,to,content,deliverAs?}` | requires pairing; → `{queued}`. `deliverAs`: `steer`\|`followUp`\|`nextTurn` |
| GET | `/stream/{agent}` | — (SSE) | drains queued, then live `data:` frames |

## CLI

```bash
cpv2 setup --agent claude --name keeper          # detect agents + wire the chosen one (no-clobber config merge)
cpv2 run                                          # exec claude with the peers dev-channel loaded
cpv2 statusline                                   # one-line peer status (for a coding-agent statusline)
cpv2 pair    --from daemon --to keeper          # → prints a code
cpv2 approve --owner keeper --code <code>        # run as the target
cpv2 send    --from daemon --to keeper "build is green"
cpv2 peers                                       # table of who's online
```

### Statusline

`cpv2 statusline` prints one compact line so a session always knows its peer state
(wire it via `cpv2 setup`). It never hangs the host (500ms broker cap) and never errors out:

```
● peers: keeper · 4 online                       # registered + online
◌ peers: keeper · offline                        # configured but channel not connected
○ peers: keeper · not registered                 # name not on the broker
○ peers: broker down                             # broker unreachable
● peers: keeper · 4 online · ⚠ also here: jim     # another session is live in THIS cwd
○ peers: ghost · not registered · ⚠ also here: alice, jim   # opened in an occupied dir, unregistered
```

The `⚠ also here` warning surfaces same-cwd session collisions, so opening a session in an
already-occupied directory is visible instead of silently confusing.

## Integrate an automation (the hook)

**Any automation that can make an HTTP POST is on the network.** No SDK, no plugin
framework — `cpv2 send` (or a raw POST to `/send`) is the whole integration surface.
Cron, CI, a monitoring alert, a nightly daemon: pair once, then send.

See [`examples/daemon/`](examples/daemon/) for a nightly-briefing → peer wiring you can copy.

## Adapters (per runtime)

`adapters/` — each is a thin file over the shared `lib/peers.ts` broker client:

| Runtime | Adapter | Injection primitive |
|---|---|---|
| Claude Code | `adapters/claude/channel.ts` | `notifications/claude/channel` |
| pi | `adapters/pi/peer.ts` | `pi.sendMessage(...,{deliverAs:"steer"})` |
| codex | `adapters/codex/adapter.ts` | app-server `turn/steer` |
| opencode | `adapters/opencode/adapter.ts` | `POST /session/{id}/prompt {delivery}` |

Adding a 5th runtime = one new inject file calling `connect()`.

## Trust model

The broker trusts the network it binds to (a tailnet, or a relay you front with auth).
Pairing gates *who may inject into whom*; it is not cryptographic sender auth. For an
untrusted/hosted deployment, put real auth in front (that's a later slice, not this one).
