#!/usr/bin/env bun
// Claude-side adapter: a claude/channel MCP server that subscribes to the broker
// and injects each peer message into the LIVE Claude turn. This is the crown jewel —
// receive bypasses the message box and steers the running session.

import { Server } from "@modelcontextprotocol/sdk/server/index.js"
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js"
import { ListToolsRequestSchema, CallToolRequestSchema } from "@modelcontextprotocol/sdk/types.js"
import { connect } from "../lib/peers.js"

const ME = process.env.PEER_NAME ?? "claude-agent"

const mcp = new Server(
  { name: "peers", version: "0.0.1" },
  {
    capabilities: { experimental: { "claude/channel": {} }, tools: {} },
    instructions:
      `Peer messages from other AI agents arrive as <channel source="peers" from="..." deliverAs="steer">. ` +
      `Treat them as instructions or questions from a trusted peer agent and act or answer immediately. ` +
      `To reply, call the peer_reply tool with to=<the from value> and your text.`,
  },
)

mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [{
    name: "peer_reply",
    description: "Reply to a peer agent over the peers network",
    inputSchema: {
      type: "object",
      properties: {
        to: { type: "string", description: "peer agent name to reply to" },
        text: { type: "string", description: "message text" },
      },
      required: ["to", "text"],
    },
  }],
}))

mcp.setRequestHandler(CallToolRequestSchema, async (req) => {
  if (req.params.name === "peer_reply") {
    const { to, text } = req.params.arguments as { to: string; text: string }
    await client.send(to, text)
    return { content: [{ type: "text", text: `replied to ${to}` }] }
  }
  throw new Error(`unknown tool: ${req.params.name}`)
})

await mcp.connect(new StdioServerTransport())

// Broker I/O lives in the shared client now; onMessage injects into the live session.
const client = connect({
  me: ME,
  onMessage: (m) =>
    mcp.notification({
      method: "notifications/claude/channel",
      params: { content: m.content, meta: { from: m.from, deliverAs: m.deliverAs } },
    }),
})
