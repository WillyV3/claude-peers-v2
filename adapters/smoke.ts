#!/usr/bin/env bun
// Headless smoke test for Slice 2a.
// Builds the v2 broker, registers + pairs claude-agent and pi-agent,
// fakes the MCP client handshake with the claude/channel adapter, sends a
// peer message, and asserts notifications/claude/channel is emitted.

import { spawn, spawnSync } from "node:child_process"
import { hostname } from "node:os"

const ADDR = "127.0.0.1:7900"
const BROKER = `http://${ADDR}`
const DB = `/tmp/cpv2_smoke_${Date.now()}.db`
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
const repoRoot = import.meta.dir + "/.."

function fail(msg: string): never { console.error("FAIL:", msg); process.exit(1) }

async function postJSON(path: string, body: any) {
  const res = await fetch(BROKER + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  return { status: res.status, json: await res.json() }
}

async function waitForBroker(ms = 5000) {
  const start = Date.now()
  while (Date.now() - start < ms) {
    try {
      const res = await fetch(`${BROKER}/peers`)
      if (res.status === 200) return
    } catch {}
    await sleep(50)
  }
  fail("broker did not become ready")
}

function parseLines(stream: NodeJS.ReadableStream, out: any[]) {
  let buf = ""
  stream.on("data", (d: Buffer) => {
    buf += d.toString()
    let i: number
    while ((i = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, i).trim()
      buf = buf.slice(i + 1)
      if (!line) continue
      try { out.push(JSON.parse(line)) } catch {}
    }
  })
}

function send(chan: NodeJS.WritableStream, o: any) {
  chan.write(JSON.stringify(o) + "\n")
}

async function waitForMsg(msgs: any[], pred: (m: any) => boolean, ms = 5000) {
  const start = Date.now()
  while (Date.now() - start < ms) {
    const m = msgs.find(pred)
    if (m) return m
    await sleep(50)
  }
  return undefined
}

// 1. Build + start broker
if (spawnSync("go", ["build", "-o", "/tmp/cpv2_smoke", "."], { cwd: repoRoot }).status !== 0)
  fail("broker build failed")

const broker = spawn("/tmp/cpv2_smoke", [], {
  cwd: repoRoot,
  env: { ...process.env, CPV2_ADDR: ADDR, CPV2_DB: DB },
  stdio: ["ignore", "inherit", "inherit"],
})

const adapter = { proc: null as ReturnType<typeof spawn> | null }

try {
  await waitForBroker()

  // 2. Register agents
  for (const agent of ["claude-agent", "pi-agent"]) {
    const { status, json } = await postJSON("/register", {
      agent,
      machine: hostname(),
      cwd: process.cwd(),
    })
    if (status !== 200 || !json.ok) fail(`register ${agent}: ${status} ${JSON.stringify(json)}`)
  }

  // 3. Pair pi-agent -> claude-agent and approve
  const { status: pairStatus, json: pairJson } = await postJSON("/pair", { from: "pi-agent", to: "claude-agent" })
  if (pairStatus !== 200 || !pairJson.code) fail(`pair: ${pairStatus} ${JSON.stringify(pairJson)}`)
  const { status: approveStatus, json: approveJson } = await postJSON("/pair/approve", {
    owner: "claude-agent",
    code: pairJson.code,
  })
  if (approveStatus !== 200 || !approveJson.ok) fail(`approve: ${approveStatus} ${JSON.stringify(approveJson)}`)

  // 4. Spawn claude/channel adapter and capture its stdio
  adapter.proc = spawn("bun", ["adapters/claude/channel.ts"], {
    cwd: repoRoot,
    env: { ...process.env, BROKER, PEER_NAME: "claude-agent" },
    stdio: ["pipe", "pipe", "inherit"],
  })

  const msgs: any[] = []
  parseLines(adapter.proc.stdout!, msgs)
  await sleep(500) // let adapter connect its SSE subscription

  // 5. Fake MCP client handshake
  send(adapter.proc.stdin!, {
    jsonrpc: "2.0",
    id: 1,
    method: "initialize",
    params: {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "smoke", version: "0" },
    },
  })
  const initRes = await waitForMsg(msgs, (m) => m.id === 1 && m.result)
  if (!initRes) fail("no initialize response from adapter")
  if (!initRes.result?.capabilities?.experimental?.["claude/channel"])
    fail("adapter did not advertise claude/channel: " + JSON.stringify(initRes.result?.capabilities))
  send(adapter.proc.stdin!, { jsonrpc: "2.0", method: "notifications/initialized" })
  await sleep(300)

  // 6. Send pi-agent -> claude-agent through broker
  const content = "what is 2+2"
  const { status: sendStatus, json: sendJson } = await postJSON("/send", {
    from: "pi-agent",
    to: "claude-agent",
    content,
    deliverAs: "steer",
  })
  if (sendStatus !== 200) fail(`send: ${sendStatus} ${JSON.stringify(sendJson)}`)

  // 7. Assert notifications/claude/channel
  const note = await waitForMsg(msgs, (m) => m.method === "notifications/claude/channel")
  if (!note) fail("adapter never emitted notifications/claude/channel")
  if (note.params?.content !== content) fail("content mismatch: " + JSON.stringify(note.params))
  if (note.params?.meta?.from !== "pi-agent") fail("meta.from mismatch: " + JSON.stringify(note.params?.meta))

  console.log("PASS: broker + adapters ok, notifications/claude/channel delivered")
  console.log("  payload:", JSON.stringify(note.params))
} finally {
  adapter.proc?.kill()
  broker.kill()
  try { await Bun.file(DB).delete() } catch {}
}

process.exit(0)
