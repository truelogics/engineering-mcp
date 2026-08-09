---
doc: RUNBOOK
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Engineering OS in Claude Code

Everything about the Claude Code side: registering the server, checking it
works, what a normal session looks like, and what to do when it goes
quiet.

Building the binaries and indexing a workspace comes first —
[`INSTALL.md`](../INSTALL.md).

## Registration

Once, at user scope, for every project on the machine:

```bash
claude mcp add engineering --scope user \
  -e ENGINEERING_WORKSPACE="$HOME/engineering-os" \
  -- "$HOME/.local/bin/engineering-mcp"
```

Two arguments are doing real work.

`--scope user` registers it for every project rather than one. That is the
point of the design: the server resolves a workspace at each start from
the directory Claude Code was opened in, so one registration serves every
repository. A server pinned to one absolute path serves the project it was
configured for and quietly serves the wrong knowledge everywhere else.

`-e ENGINEERING_WORKSPACE=...` is the fallback for projects that are not
themselves inside a workspace. Resolution order:

1. `--workspace`, if you pass one (you usually should not);
2. the nearest indexed workspace at or above the working directory;
3. `$ENGINEERING_WORKSPACE`.

Project scope instead, if you would rather commit the configuration: copy
[`mcp.json.example`](../integration/claude-code/mcp.json.example) to
`.mcp.json` at your project root and approve it when Claude Code asks.

## The command that makes the tools reachable

```bash
mkdir -p ~/.claude/commands
cp integration/claude-code/review-branch.md ~/.claude/commands/
```

Install this. It is not a shortcut for something you could do by hand.

Claude Code defers tool schemas on a machine with several MCP servers
installed: the tool's schema is absent from the model's prompt, and the
tool cannot be called at all until something names it and its schema is
fetched. `review-branch.md` names these four tools, which is what loads
them.

Measured, same repository and same commit, varying only whether the
command was available:

| | `ToolSearch` | Engineering MCP calls | tool calls total |
|---|---|---|---|
| command available | 1 | **3** | 40 |
| command unavailable | 0 | **0** | 42 |

Forty-two tool calls, zero of them to a server that was registered,
connected, and explicitly allowed. Full account:
[`reports/TOOL_DISCOVERY_EXPERIMENT.md`](reports/TOOL_DISCOVERY_EXPERIMENT.md).

## Verification

```bash
eng doctor
```

Run it from inside the repository you want to review, not from the
workspace root — several checks are about *this* repository specifically,
and the answer differs by directory.

To check the server alone, without Claude Code in the picture:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | engineering-mcp
```

You should get two JSON lines on stdout and one `serving <workspace>` line
on stderr. Anything else on stdout is a bug: stdout is the JSON-RPC
channel and a stray line corrupts the protocol without corrupting anything
you can see. `doctor`'s handshake check tests exactly this.

## What a session looks like

```
cd your-repository
claude
```

> /review-branch

**Invoke it by name.** The natural-language form is not equivalent. Two
runs on this repository, same commit, minutes apart, same machine:

| Asked as | Skill that ran | `ToolSearch` | Engineering MCP calls | Total tool calls |
|---|---|---|---|---|
| `/review-branch` | `review-branch` | 1 | **9** | 59 |
| "Review my current branch." | `review-pull-requests` | 0 | **0** | 37 |

Both produced a competent review. The second never called Engineering
OS — a different project's review skill matched the sentence first, and
because tool schemas are deferred, losing that match is not a preference
but an exclusion: nothing named the tools, so nothing could call them.

The sprint that specified this workflow assumed the plain sentence would
work. On a machine with one review skill it does. This one had several.

Once the command is running, Claude Code derives the repository, branch,
base and changed files from git, then:

**1. Finds the rules that govern those files.**

```json
{"name": "find_engineering_rules",
 "arguments": {"changed_paths": ["internal/authz/check.go"],
               "task": "cache permission lookups in process"}}
```

Rules come back selected by what they *govern*, not by keyword — a change
rarely mentions the rule it breaks. This is the difference between a
rulebook that gets read and one that gets skimmed.

**2. Gathers the surrounding context.**

```json
{"name": "get_context",
 "arguments": {"task": "cache permission lookups in process",
               "changed_paths": ["internal/authz/check.go"]}}
```

If the organization rejected in-process permission caching after an
incident, the ADR recording that comes back here — and the review can say
"ADR-0003 rejected this after the March incident, where a revoked
administrator kept access for eleven minutes", rather than "caching can
serve stale data".

Both are correct. Only one is checkable, and only one tells you the
argument has already been had.

**3. Searches for anything more specific.**

```json
{"name": "search_memory",
 "arguments": {"query": "permission cache invalidation", "limit": 5}}
```

**4. Verifies each quote before attributing it.**

```json
{"name": "verify_evidence",
 "arguments": {"task": "cache permission lookups in process",
               "document": "engineering:adr/0003-no-permission-cache.md",
               "excerpt": "in-process permission caches are not permitted",
               "changed_paths": ["internal/authz/check.go"]}}
```

Pass the same `changed_paths` you gave the other tools. Rules are selected
by path scope and are simply absent from an unscoped context, so a
verbatim quote from a rule verifies as `NOT VERIFIED` without them. That
was a real failure, diagnosed in
[`reports/SPRINT_11_VALIDATION.md`](reports/SPRINT_11_VALIDATION.md).

The server does none of the reasoning. It never proposes a change, never
judges one, and never decides which tool to call.

## Reading the answers

**An empty result is an answer.** `find_engineering_rules` returning
nothing means no indexed rule declares it governs those files — not that
the lookup failed. The tools say so explicitly, because a model cannot
otherwise tell "nothing applies" from "I didn't look".

**Every answer names the workspace it came from.** Not only the empty
ones. A workspace holding a few stale or wrong rules returns them with
total confidence, and that answer is indistinguishable from a correct one
unless it says where it came from.

**Scores are relative to one query.** They order results within a single
call and mean nothing across calls. A 0.00 score is noise, not a weak
match.

**Snippets are short.** Roughly 40–200 characters of search highlight, not
full documents. Enough to judge whether a document is worth opening; not
enough to quote a paragraph. When the excerpt matters, read the file at
the path given.

**Documents are `repository:path`.** A workspace holds several
repositories and `README.md` is ambiguous across them.

## Common problems

**Claude Code never calls the tools.** By far the most common, and it
looks exactly like the server being broken. Two distinct causes, in
order of likelihood:

1. You asked in plain language and another review skill claimed it. Use
   `/review-branch`.
2. The command is not installed at all. `eng doctor` checks.

**"No MCP server found with name: engineering".** Not registered, or
registered at project scope in a different project. `claude mcp get
engineering` shows scope and status.

**Registered and connected, but your changes have no effect.** Claude Code
is launching a different binary than the one you rebuilt. `claude mcp get
engineering` prints the command it runs; compare it with `which
engineering-mcp`. `doctor` compares them for you.

**"no indexed workspace found at or above ...".** You are outside every
workspace and `ENGINEERING_WORKSPACE` is unset. The error prints both
fixes. This is a hard failure on purpose: a server answering every
question with "nothing found" is indistinguishable from one whose
knowledge base is simply quiet.

**Rules come back empty for every file.** Usually the workspace that
answered holds no rulebook. `eng workspace list` shows what is in it.

**A rule you expected is missing.** Check its `applies_to`. A rule
declaring `applies_to: "**/*.ts"` will not appear for a Go change, by
design. A rule with no `applies_to` is universal and always appears.

**The wrong workspace answered.** Resolution takes the *nearest*
workspace, so a stale `.eng/` inside a single repository beats the one
above it. The server prints its choice on stderr; `doctor` prints it too.

**A citation verifies as NOT VERIFIED although the quote is exact.** Two
causes. Either `changed_paths` was omitted and the document is a rule (see
step 4 above), or the quote is in the document but not in the ~200
character snippet that was retrieved — the kernel matches against the
snippet, not the file. The second is a known kernel limitation, recorded
as [`KERNEL_REQUIREMENTS.md`](../KERNEL_REQUIREMENTS.md) #2 rather than
worked around here.
