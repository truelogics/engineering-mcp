---
doc: CLAUDE_CODE
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Using engineering-mcp from Claude Code

## Setup

Installation lives in one place:
[`integration/claude-code/README.md`](../integration/claude-code/README.md).
It covers building the binaries, indexing a workspace, registering the
server, and installing the `/review-branch` command.

This document is the companion: what the tools are for and how to read
what they return.

Verify the server independently of any client before wiring it up:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' \
  | ./engineering-mcp --workspace /path/to/workspace
```

## What changes

Without this server, an assistant editing your code knows general
engineering practice. With it, it can ask what *your organization* has
already decided — and cite it.

The division of labor is strict:

- **Claude Code** decides what to look up, reasons about the answer, and
  writes the code.
- **engineering-mcp** answers questions about engineering knowledge.

The server never proposes a change, never judges one, and never decides
which tool to call.

## A worked example

Asked to add a caching layer to permission checks, a client with these
tools available would typically do this:

**1. Find the rules that govern the files it is about to touch.**

```json
{"name": "find_engineering_rules",
 "arguments": {"changed_paths": ["internal/authz/check.go"],
               "task": "cache permission lookups in process"}}
```

Rules come back selected by what they *govern*, not by keyword — a
change rarely mentions the rule it breaks. This is the difference
between a rulebook that gets read and one that gets skimmed.

**2. Gather the surrounding context.**

```json
{"name": "get_context",
 "arguments": {"task": "cache permission lookups in process",
               "changed_paths": ["internal/authz/check.go"]}}
```

If the organization rejected in-process permission caching after an
incident, the ADR recording that comes back here — and the assistant can
say "ADR-0003 rejected this after the March incident, where a revoked
administrator kept access for eleven minutes," rather than "caching can
serve stale data."

Both are correct. Only one is checkable, and only one tells you the
argument has already been had.

**3. Search for anything more specific.**

```json
{"name": "search_memory",
 "arguments": {"query": "permission cache invalidation", "limit": 5}}
```

Every result is qualified as `repository:path`, because a workspace holds
several repositories and `README.md` is ambiguous across them.

## Reading the answers

**An empty result is an answer.** `find_engineering_rules` returning
nothing means no rule in the rulebook declares it governs those files —
not that the lookup failed. The tools state this explicitly rather than
returning silence, because a model cannot otherwise distinguish "nothing
applies" from "I didn't look."

**Scores are relative to one query.** They order results within a single
call and mean nothing across calls.

**Snippets are short.** The kernel returns search highlights of roughly
40–200 characters, not full documents
(`ai-review/KERNEL_REQUIREMENTS.md` #15). Enough to judge whether a
document is worth opening; not enough to quote a paragraph. When the
excerpt matters, read the file at the path given.

## Troubleshooting

**"no indexed workspace at ..."** — the path has no `.eng/memory.db`.
Run `eng workspace create .` there, attach repositories, and `eng index .`.

**Rules come back empty for every file** — check `eng workspace list`
shows the repository holding your rules. A workspace containing only
your application has no rulebook to consult.

**A rule you expected is missing** — check its `applies_to` front matter.
A rule declaring `applies_to: "**/*.ts"` will not appear for a Go
change, by design. A rule with no `applies_to` is universal and always
appears.
