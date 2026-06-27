// pi-side adapter: a peer_send tool (pi -> broker) plus a receive loop that
// injects incoming peer messages into pi's LIVE turn via sendMessage steer.
// Mirrors pi-messenger's proven call, but the transport is the broker (remote-capable)
// instead of the local filesystem.

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent"
import { Type } from "typebox"
import { connect } from "../lib/peers.js"

const ME = process.env.PEER_NAME ?? "pi-agent"

export default function (pi: ExtensionAPI) {
  const client = connect({
    me: ME,
    onMessage: (m) =>
      pi.sendMessage(
        { customType: "peer", content: `[peer ${m.from}] ${m.content}`, display: true, details: m },
        { deliverAs: "steer", triggerTurn: true },
      ),
  })

  // SEND: pi -> broker -> peer
  pi.registerTool({
    name: "peer_send",
    label: "Peer Send",
    description: "Send a message to another AI agent on the peers network (e.g. to: \"claude-agent\").",
    promptSnippet: "Use peer_send to message another agent on the peers network.",
    parameters: Type.Object({
      to: Type.String({ description: "peer agent name, e.g. claude-agent" }),
      content: Type.String({ description: "message text" }),
    }),
    async execute(_id, params: { to: string; content: string }) {
      const out = await client.send(params.to, params.content)
      const status = out.queued ? "queued (offline)" : "delivered live"
      return { content: [{ type: "text", text: `sent to ${params.to} — ${status}` }], details: out }
    },
  })
}
