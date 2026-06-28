#!/usr/bin/env bun
// opencode adapter: bridges the peers broker to a running opencode server.
// Each inbound peer message is injected as a prompt into a dedicated opencode
// session — the agent's live turn. Verified against opencode server v1.17 API:
//   POST /api/session              -> {data:{id}}
//   POST /api/session/{id}/prompt  body {prompt:{text}, delivery?:"steer"|"queue"}
// Requires a running opencode server (`opencode serve`) + OPENCODE_API_KEY for the model.

import { connect } from "../lib/peers.js"

const OPENCODE_URL = process.env.OPENCODE_URL ?? "http://127.0.0.1:4096"

async function createSession(): Promise<string> {
  const res = await fetch(`${OPENCODE_URL}/api/session`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  })
  const body = (await res.json()) as { data: { id: string } }
  return body.data.id
}

const sessionID = await createSession()
console.error(`[opencode-adapter] session ${sessionID} on ${OPENCODE_URL}`)

connect({
  me: "opencode-agent",
  onMessage: async (m) => {
    const res = await fetch(`${OPENCODE_URL}/api/session/${sessionID}/prompt`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        prompt: { text: `[peer ${m.from}] ${m.content}` },
        delivery: m.deliverAs === "steer" ? "steer" : "queue",
      }),
    })
    if (!res.ok) console.error(`[opencode-adapter] prompt ${res.status} for session ${sessionID}`)
  },
})
