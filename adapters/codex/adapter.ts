#!/usr/bin/env bun
// codex adapter: bridges the peers broker to a codex app-server (JSON-RPC over stdio).
// Spawn `codex app-server` (assumes `codex` on PATH), track thread/turn state from its
// notifications, and steer or start turns from inbound peer messages.
// Live integration requires `codex` installed — NOT covered by adapters/smoke.ts.

import { spawn } from "node:child_process"
import { connect } from "../lib/peers.js"

let threadId: string | null = null
let currentTurnId: string | null = null
let nextId = 1
const pending = new Map<number, () => void>()

const proc = spawn("codex", ["app-server"], { stdio: ["pipe", "pipe", "inherit"] })

// Read newline-delimited JSON-RPC from the app-server: track state from notifications,
// resolve pending requests by id.
let buf = ""
proc.stdout!.setEncoding("utf8")
proc.stdout!.on("data", (chunk: string) => {
  buf += chunk
  let i: number
  while ((i = buf.indexOf("\n")) >= 0) {
    const line = buf.slice(0, i).trim()
    buf = buf.slice(i + 1)
    if (!line) continue
    let msg: { id?: number; method?: string; params?: { thread_id?: string; turn_id?: string } }
    try { msg = JSON.parse(line) } catch { continue }
    if (msg.method === "thread/started") threadId = msg.params?.thread_id ?? threadId
    else if (msg.method === "turn/started") currentTurnId = msg.params?.turn_id ?? null
    else if (msg.method === "turn/completed") currentTurnId = null
    if (typeof msg.id === "number" && pending.has(msg.id)) {
      const resolve = pending.get(msg.id)!
      pending.delete(msg.id)
      resolve()
    }
  }
})

// Write a JSON-RPC request to the app-server's stdin; resolve when the matching response lands.
function request(method: string, params: Record<string, unknown>): Promise<void> {
  const id = nextId++
  return new Promise((resolve) => {
    pending.set(id, resolve)
    proc.stdin!.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n")
  })
}

connect({
  me: "codex-agent",
  onMessage: (m) => {
    if (!threadId) return
    const input = [{ type: "text", text: m.content }]
    if (currentTurnId) {
      return request("turn/steer", { thread_id: threadId, input, expected_turn_id: currentTurnId })
    }
    return request("turn/start", { thread_id: threadId, input })
  },
})
