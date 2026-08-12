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
that. This is the second consumer of [`engineering-kernel`](../engineering-kernel/), and
its purpose is as much to test the kernel as to be useful: a kernel that
two unrelated consumers can build on is a platform, and a kernel only
one can build on is an application with extra steps.

## Why it isn't called a server

It is a transport. MCP today; HTTP or gRPC later, over the same
capabilities and the same adapters. Naming it for the protocol keeps
that honest — nothing in `internal/tools` knows it is being reached over
MCP, and nothing in `internal/mcp` knows what a rule is.

## Getting started

| | |
|---|---|
| [`QUICKSTART.md`](QUICKSTART.md) | ten minutes, from nothing to a repository-aware review |
| [`INSTALL.md`](INSTALL.md) | the same install with what each step produces and what goes wrong |
| [`ONBOARDING.md`](ONBOARDING.md) | making one repository Engineering OS aware |
| [`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md) | the Claude Code side: registering, verifying, troubleshooting |

`engineering-mcp doctor` checks a machine end to end and names the first
thing that is wrong.

## The tools

| Tool | Answers | Validated by |
|---|---|---|
| `search_memory` | "What has been written about this?" | `eng search` |
| `get_context` | "What engineering context surrounds this task?" | Engineering Review's `Reviewer` |
| `find_engineering_rules` | "What rules govern these files?" | Engineering Review, Sprint 7 |
| `verify_evidence` | "Does this quote really appear in that document?" | Engineering Review's `Validator`; promoted to the kernel by RFC-0006 |

Four, not five. Every tool here satisfies
[`KERNEL_POLICY.md`](../engineering/KERNEL_POLICY.md) Rule #6 — it
already exists as a capability a real consumer proved useful. Two of the
five tools originally proposed for this repository do not, and are
deliberately absent.

### Once rejected, then promoted: evidence verification

The fourth tool was originally proposed as `collect_evidence` and
rejected. Checking that a quoted excerpt really appears in the document it
cites was real, tested, and lived in Engineering Review's `Validator` and nowhere
else — so exposing it here meant either duplicating an anti-hallucination
check, leaving two implementations to drift apart, or promoting it into
the kernel.

engineering-kernel RFC-0006 promoted it, and `verify_evidence` is now the four-line
adapter that was predicted. It is named for what it does — verifying a
quote a client proposes — rather than `collect_evidence`, which would
imply the kernel searches for supporting text. It does not, and no
consumer has asked it to.

This is the sequence Rule #6 is meant to produce: consumer proves it,
kernel adopts it, transport exposes it. Not transport invents it.

### Rejected: `get_architecture_context`

`ContextPackage` collapses five distinct retrieval groups —
Architecture, RFCs, Roadmap, Documentation, Other — into one
`RelevantFiles` list before a consumer sees them
(`engineering-review/KERNEL_REQUIREMENTS.md` #16). The kernel's retriever makes
the distinction; its public surface discards it.

A tool named `get_architecture_context` would therefore return
documentation, templates and roadmaps alongside architecture, while its
name promised otherwise. A model choosing tools by their descriptions
would be misled by ours, which is worse than the capability being
missing. `get_context` returns the same documents under a name that does
not overclaim.

## Requirements

Go 1.25+, git, and an indexed Engineering Kernel workspace. The server never
indexes anything — it reads what `eng` has already built. It depends on a
released `engineering-kernel`, so this repository builds from a clone of its own;
use a `go.work` when changing both at once (see `INSTALL.md`).

## Running it

```bash
go build -o engineering-mcp ./cmd/engineering-mcp

./engineering-mcp                                  # serve; resolves a workspace itself
./engineering-mcp --workspace /path/to/workspace   # serve a named workspace
./engineering-mcp doctor                           # check this machine
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

`doctor` reports to stdout instead, because nothing is speaking a protocol
to it.

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
| [`engineering-kernel/`](../engineering-kernel/) | The kernel this exposes |
| [`engineering-review/`](../engineering-review/) | The first consumer; validated these capabilities |
| [`engineering/`](../engineering/) | Standards, rules, and `CAPABILITIES.md` |
