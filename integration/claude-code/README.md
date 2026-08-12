---
doc: README
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Claude Code integration files

Two files, and what each is for.

| File | Purpose |
|---|---|
| `mcp.json.example` | project-scope server registration, if you would rather commit the configuration than run `claude mcp add` |
| `review-branch.md` | the `/review-branch` slash command |

## Install

Installation lives in [`../../INSTALL.md`](../../INSTALL.md), and the
Claude Code side in detail is
[`../../docs/CLAUDE_CODE.md`](../../docs/CLAUDE_CODE.md). It used to live
here, which meant a newcomer had to find it three directories down in a
file named for an integration they had not heard of yet.

The short version:

```bash
engineering-mcp install       # or: eng setup, which calls it
```

`review-branch.md` is compiled into the binary (see
[`../integration.go`](../integration.go)), so `install` writes it from
there rather than from this directory — which is what makes it work for
someone who installed with `go install` and has no clone.

By hand, if you would rather:

```bash
mkdir -p ~/.claude/commands
cp review-branch.md ~/.claude/commands/      # every project
# or: cp review-branch.md .claude/commands/  # this project only
```

Editing your copy is supported: `install` leaves a file that differs from
the embedded one alone, and says so, unless you pass `--force`.

## Why `review-branch.md` is not optional

A registered MCP server is a capability a model *may* use. On a machine
with several MCP servers installed it is often not even that: Claude Code
defers tool schemas, so a tool is absent from the model's prompt and
uninvokable until something names it.

This command names the four tools, which is what loads them. It also says
*retrieve before you reason* — retrieval after an opinion has formed
produces citations for a review that was already written.

Measured both ways in
[`../../docs/reports/TOOL_DISCOVERY_EXPERIMENT.md`](../../docs/reports/TOOL_DISCOVERY_EXPERIMENT.md):
3 MCP calls out of 40 tool calls with the command available; 0 out of 42
without it.
