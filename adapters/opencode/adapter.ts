#!/usr/bin/env bun
// opencode adapter: bridges the peers broker to a running opencode server.
// Assumes `OPENCODE_URL` (default http://127.0.0.1:4096).
// Live integration requires a running opencode server — NOT covered by adapters/smoke.ts.

import { connect } from "../lib/peers.js"

const OPENCODE_URL = process.env.OPENCODE_URL ?? "http://127.0.0.1:4096"

// opencode server routes — centralized here so they're trivially fixable if the API shifts.
const SESSIONS_PATH = "/session"                    // GET list / POST create
const promptPath = (sid: string) => `/session/${sid}/prompt`

async function ensureSession(): Promise<string> {
  const listRes = await fetch(OPENCODE_URL + SESSIONS_PATH)
  const sessions = (await listRes.json()) as { id: string }[]
  if (sessions.length > 0) return sessions[0]!.id
  const createRes = await fetch(OPENCODE_URL + SESSIONS_PATH, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({}),
  })
  return ((await createRes.json()) as { id: string }).id
}

const sessionID = await ensureSession()

connect({
  me: "opencode-agent",
  onMessage: (m) =>
    fetch(OPENCODE_URL + promptPath(sessionID), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ prompt: m.content, delivery: m.deliverAs === "steer" ? "steer" : "queue" }),
    }),
})
