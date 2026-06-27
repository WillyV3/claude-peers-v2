# Example: nightly daemon → peer briefing

Wire any scheduled automation to deliver into an agent's stream. This mirrors a real
setup: a daemon writes a briefing overnight, and at wake-up time it lands in the agent's
session — live if the agent is up, queued-and-drained if it was asleep.

## One-time pairing

The daemon is a *sender*; the recipient must allow it. Pair once:

```bash
# as the daemon's owner: request a pairing code
cpv2 pair --from daemon --to keeper        # prints e.g. "code": "kcnbph"

# as the recipient (keeper), approve it
cpv2 approve --owner keeper --code kcnbph
```

After this, `daemon` may send to `keeper` forever. (Pairing is per sender→recipient.)

## The script

[`briefing-to-peer.sh`](briefing-to-peer.sh) reads today's briefing file, builds a
condensed rollup (head + a pointer to the full file — one message, not a flood), and
sends it. Env knobs: `PEER_FROM`, `PEER_TO`, `CPV2_URL`.

```bash
PEER_FROM=daemon PEER_TO=keeper ./briefing-to-peer.sh
```

## Schedule it (systemd timer)

`~/.config/systemd/user/briefing.service`
```ini
[Service]
Type=oneshot
Environment=PEER_FROM=daemon PEER_TO=keeper
ExecStart=%h/path/to/briefing-to-peer.sh
```

`~/.config/systemd/user/briefing.timer`
```ini
[Timer]
OnCalendar=*-*-* 06:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
systemctl --user enable --now briefing.timer
```

## Why this is the whole integration

There is no plugin API to learn. Any program that can POST JSON is a peer. The daemon
doesn't know or care which runtime the recipient is — the broker queues, and the
recipient's adapter injects it natively when it's online.
