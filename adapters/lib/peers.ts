// Shared broker client for all peers adapters. The single place broker I/O lives.
// Adding a 5th runtime means writing one small file that calls connect() — nothing else here changes.

import { hostname } from "node:os"

export type PeerMessage = {
  id: number
  from: string
  to: string
  content: string
  deliverAs: string
  createdAt: number
}

export type PeerClient = {
  send(to: string, content: string, deliverAs?: string): Promise<{ queued: boolean }>
}

export function connect(opts: {
  broker?: string
  me: string
  machine?: string
  cwd?: string
  onMessage: (m: PeerMessage) => void | Promise<void>
}): PeerClient {
  const broker = opts.broker ?? process.env.BROKER ?? "http://127.0.0.1:7900"
  const me = opts.me
  const machine = opts.machine ?? hostname()
  const cwd = opts.cwd ?? process.cwd()

  function postJSON(path: string, body: unknown) {
    return fetch(broker + path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  }

  // Register once at start; heartbeat every 15s. Fire-and-forget — broker may not be up yet.
  postJSON("/register", { agent: me, machine, cwd }).catch(() => {})
  setInterval(() => postJSON("/heartbeat", { agent: me }).catch(() => {}), 15000)

  // SSE subscribe + parse; reconnect forever with a 1s delay. Comment frames (": ...") are
  // ignored because they have no "data: " line.
  ;(async () => {
    for (;;) {
      try {
        const res = await fetch(`${broker}/stream/${me}`)
        const reader = res.body!.getReader()
        const dec = new TextDecoder()
        let buf = ""
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buf += dec.decode(value, { stream: true })
          let i: number
          while ((i = buf.indexOf("\n\n")) >= 0) {
            const frame = buf.slice(0, i)
            buf = buf.slice(i + 2)
            const line = frame.split("\n").find((l) => l.startsWith("data: "))
            if (!line) continue
            const m = JSON.parse(line.slice(6)) as PeerMessage
            await opts.onMessage(m)
          }
        }
      } catch {
        // broker down or stream dropped — retry
      }
      await new Promise((r) => setTimeout(r, 1000))
    }
  })()

  return {
    async send(to, content, deliverAs = "steer") {
      const res = await postJSON("/send", { from: me, to, content, deliverAs })
      return (await res.json()) as { queued: boolean }
    },
  }
}
