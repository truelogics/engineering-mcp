---
doc: README
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Claude Code integration

Two files turn Claude Code into a reviewer that knows your organization's
engineering decisions:

- `mcp.json.example` — registers the server, so the knowledge is reachable.
- `review-branch.md` — a slash command, so the knowledge is reached
  *without being asked for*.

The second file is the one that matters. A registered MCP server is a
capability a model may use; a command that says "retrieve before you
reason" is a capability it does use, in the right order. Retrieval after
an opinion has formed produces citations for a review that was already
written.

## Install

**1. Build the binaries.**

```bash
go build -o ~/.local/bin/engineering-mcp ./cmd/engineering-mcp   # this repo
go build -o ~/.local/bin/eng ../ai-memory/cmd/eng                # the kernel CLI
```

**2. Index a workspace.**

A workspace is an indexing boundary, not a repository. It should hold the
repository you are working in *and* the repository holding your
engineering rules — a workspace containing only your application has no
rulebook to consult.

```bash
cd /path/to/your/workspace-root
eng workspace create .
eng workspace detach .            # if the root is a parent of your repos
eng workspace attach ./engineering
eng workspace attach ./your-application
eng workspace list
```

`eng workspace create` registers its own directory. When the workspace
root is a *parent* of several repositories, detach it — otherwise every
repository is indexed under one name and `repository:path` citations stop
distinguishing them.

Attach only repositories whose documents are true statements about the
organization. Benchmark fixtures and example projects contain plausible
ADRs that were never decided, and a review cannot tell them from real ones.

**3. Register the server.**

Copy `mcp.json.example` to `.mcp.json` at your project root and fill in
the two absolute paths. Claude Code will ask you to approve it the first
time you start a session there.

**4. Install the command.**

```bash
mkdir -p .claude/commands
cp review-branch.md .claude/commands/
```

## Use

```
/review-branch
```

Claude Code works out the repository, branch and changed files from git,
asks Engineering OS which rules govern those files and what has been
decided around them, reads what comes back, and then reviews. The server
never reviews anything.

Every run ends with a retrieval record: which rules came back, which
actually applied, and which knowledge was retrieved but irrelevant. Read
that section. It is how the rulebook earns its place, or fails to.

## Keeping the index current

The index is a snapshot. After engineering documents change:

```bash
eng index /path/to/workspace-root
```

Nothing detects staleness yet, and a review against a stale index cites
rules that may since have been superseded, with nothing in the output
saying so. Re-indexing is currently the developer's job.

(An earlier draft cited `engineering:rules/no-silent-fallback.md` here.
That rule governs Go and concerns substituting a fake when a dependency is
missing; stretching it to cover stale data borrowed its authority for a
case it does not make. The observation stands on its own.)
