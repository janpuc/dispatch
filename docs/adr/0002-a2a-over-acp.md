# ADR-0002: A2A for the agent-facing edge; ACP is not adopted

- Status: accepted
- Date: 2026-08-24 (protocol state verified against both project sites on this date)

## Context

Two candidate open protocols were on the table:

- **ACP** — agentcommunicationprotocol.dev (IBM/BeeAI lineage, REST-based).
- **A2A** — a2a-protocol.org (Google lineage, now Linux Foundation), spec at 1.0.0.

Verified on 2026-08-24: the ACP site itself carries the banner **"ACP is now part of A2A
under the Linux Foundation"** and publishes a migration guide. The ecosystem made this
choice for us — ACP is a legacy lineage inside A2A, not a competing standard.

A2A 1.0 facts that matter to Dispatch:

- Three transports (JSON-RPC 2.0, gRPC, HTTP+JSON/REST) over one canonical data model.
- Discovery via Agent Cards at `/.well-known/agent-card.json` — an IANA-registered
  well-known URI (RFC 8615 registry), which is exactly the "preparing the internet for
  agents" signal noticed in the original brief. Cards can be cryptographically signed.
- Task lifecycle: `submitted / working / input-required / auth-required / completed /
  canceled / failed / rejected` — a ready-made state machine for Dispatch sessions.
- Updates via polling, SSE streaming, and push notifications (webhooks).

## Decision

Adopt A2A at the **edges only**:

1. **North-south:** the gateway is an A2A server. Every `Agent` CR exposes an Agent Card;
   t3 (or any A2A client) sends tasks, streams progress via SSE, and receives push
   notifications — this is the phone story.
2. **Session states mirror A2A task states** 1:1, so exposing a session over A2A is a
   field mapping, not a translation layer. `input-required` becomes the "agent asks, you
   answer from your phone, it resumes" flow.
3. **East-west (later):** agent→agent handoffs (duty agent files work to a project agent)
   use A2A `message/send` rather than anything bespoke.

A2A is **not** used for the control plane. Reconciliation, RBAC, and audit belong to the
Kubernetes API; tunneling them through A2A would rebuild kubectl badly.

Division of labor: **MCP connects an agent to its tools; A2A connects clients and agents
to each other; CRDs run the machine.**

## Consequences

- Implement an A2A server (JSON-RPC + SSE first; gRPC only if a client demands it) and
  card generation/signing in the gateway — bounded, well-specified work.
- Any future A2A-speaking client works without Dispatch-specific code; cheap optionality.
- ACP is never implemented, and its SDKs are not dependencies.

## Alternatives rejected

- **ACP** — merged into A2A; adopting it today means adopting a migration guide.
- **Bespoke HTTP API only** — less work up front, but re-invents task states, streaming,
  and discovery that A2A specifies, and forecloses interop with a fast-growing ecosystem.
- **MCP-only edge** — MCP is kept (a Dispatch MCP server is the fastest t3 integration)
  but it models tools, not long-running tasks with lifecycle and push updates.
