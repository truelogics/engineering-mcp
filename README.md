---
doc: README
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# engineering-mcp

Exposes the Engineering OS's proven knowledge capabilities to AI clients
over the Model Context Protocol.

It reads engineering knowledge and answers questions about it. It does
not review, plan, edit, or decide anything — the client does all of
that. This is the second consumer of [`ai-memory`](../ai-memory/), and
its purpose is as much to test the kernel as to be useful: a kernel that
two unrelated consumers can build on is a platform, and a kernel only
one can build on is an application with extra steps.

## Why it isn't called a server

It is a transport. MCP today; HTTP or gRPC later, over the same
capabilities and the same adapters. Naming it for the protocol keeps
that honest — nothing in `internal/tools` knows it is being reached over
MCP, and nothing in `internal/mcp` knows what a rule is.

## The tools

| Tool | Answers | Validated by |
|---|---|---|
| `search_memory` | "What has been written about this?" | `eng search` |
| `get_context` | "What engineering context surrounds this task?" | AI Review's `Reviewer` |
| `find_engineering_rules` | "What rules govern these files?" | AI Review, Sprint 7 |

Three, not five. Every tool here satisfies
[`KERNEL_POLICY.md`](../engineering/KERNEL_POLICY.md) Rule #6 — it
already exists as a capability a real consumer proved useful. Two of the
five tools originally proposed for this repository do not, and are
deliberately absent.

### Rejected: `collect_evidence`

Evidence verification — checking that a quoted excerpt really appears in
the document it cites, and scoring how well — is real, works, and is
covered by tests. It lives in AI Review's `Validator`, and nowhere else.
`engineering/CAPABILITIES.md` classifies it **Consumer-only** for exactly
this reason.

Exposing it here would mean either duplicating `Validator` — which this
repository is explicitly forbidden from doing, and which would leave two
implementations of an anti-hallucination check to drift apart — or
promoting it into the kernel, which is a kernel change and not this
sprint's work.

It is the strongest candidate for promotion. Once it lives in
`pkg/memory`, this tool becomes a four-line adapter.

### Rejected: `get_architecture_context`

`ContextPackage` collapses five distinct retrieval groups —
Architecture, RFCs, Roadmap, Documentation, Other — into one
`RelevantFiles` list before a consumer sees them
(`ai-review/KERNEL_REQUIREMENTS.md` #16). The kernel's retriever makes
the distinction; its public surface discards it.

A tool named `get_architecture_context` would therefore return
documentation, templates and roadmaps alongside architecture, while its
name promised otherwise. A model choosing tools by their descriptions
would be misled by ours, which is worse than the capability being
missing. `get_context` returns the same documents under a name that does
not overclaim.

## Requirements

An indexed AI Memory workspace. The server never indexes anything — it
reads what `eng` has already built:

```bash
eng workspace create .
eng workspace attach ../engineering      # the organization's rulebook
eng workspace attach ../your-application
eng index .
```

## Running it

```bash
go build -o engineering-mcp ./cmd/engineering-mcp
./engineering-mcp                              # resolves a workspace itself
./engineering-mcp --workspace /path/to/workspace   # or name one
```

With no `--workspace`, it resolves one at startup: the nearest workspace
at or above the working directory, then `$ENGINEERING_WORKSPACE`. That is
what lets a single registered server answer for every repository instead
of the one it was configured for. It reports which workspace it chose,
and why, on stderr.

It speaks JSON-RPC 2.0 over stdin/stdout. Nothing else is ever written to
stdout — diagnostics go to stderr, because a stray line on stdout
corrupts the protocol.

Finding no workspace at all is a hard failure, not an empty one. A server
answering every question with "nothing found" is indistinguishable from
one whose knowledge base is simply quiet
(`engineering/rules/no-silent-fallback.md`).

To install this into Claude Code — server registration and the
`/review-branch` command — see
[`integration/claude-code/README.md`](integration/claude-code/README.md).
For what the tools are for and how to read what they return, see
[`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md).

## Architecture

```
Claude Code  (reasoning, planning, reviewing, editing)
     │
     ▼
engineering-mcp  (protocol, transport, validation, response shape)
     │
     ▼
pkg/memory  (engineering knowledge)
```

`internal/mcp` is the protocol and knows nothing about engineering.
`internal/tools` is the capability adapters and knows nothing about MCP.
Each tool is argument validation, one `pkg/memory` call, and a
rendering — if a tool ever needs logic of its own, that is a sign the
capability does not really exist in the kernel yet, and Rule #6 says it
should not be exposed.

## Non-goals

No review tool, no planning tool, no issue generation, no code
generation. Those are reasoning, and reasoning belongs to the client.
The Engineering OS supplies knowledge.

## Related repos

| Repo | Role |
|------|------|
| [`ai-memory/`](../ai-memory/) | The kernel this exposes |
| [`ai-review/`](../ai-review/) | The first consumer; validated these capabilities |
| [`engineering/`](../engineering/) | Standards, rules, and `CAPABILITIES.md` |
