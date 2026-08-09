---
doc: RUNBOOK
audience: [human]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Installing Engineering OS

This is the long version, with what each step produces and what goes
wrong. If you want the ten-line version, read
[`QUICKSTART.md`](QUICKSTART.md) instead and come back here when
something fails.

Installation is per-machine and happens once. Making a *repository*
Engineering OS aware is a separate, per-repository job —
[`ONBOARDING.md`](ONBOARDING.md).

## What you are installing

Two binaries and one index.

| | What it is | Who runs it |
|---|---|---|
| `eng` | the AI Memory CLI: builds and maintains the index | you |
| `engineering-mcp` | serves that index to an MCP client over stdio | Claude Code |
| `.eng/memory.db` | the index itself, one per workspace | neither; it is data |

`engineering-mcp` never indexes anything. It reads what `eng` has already
built. If the index is stale or empty, that is a job for `eng`.

## Prerequisites

**Go 1.25.0 or newer.** There are no prebuilt binaries; you build from
source. Go will download the 1.25 toolchain automatically on an older
installation, so the practical requirement is Go 1.21+ *and* network
access on first build. With `GOTOOLCHAIN=local` set, or offline, an older
Go fails with `go.mod requires go >= 1.25.0`, and the fix is to upgrade Go
rather than to change anything here.

**git.** Both for cloning and at runtime: the review workflow derives the
branch, base and changed files from git, and the indexer uses
`git check-ignore` to skip files your repository already ignores.

**Claude Code**, if you want the Claude Code integration. The server
speaks plain MCP and does not otherwise care.

The first build downloads dependencies and can take a few minutes.
Subsequent builds are seconds.

## 1. Clone, as siblings

```bash
mkdir -p ~/engineering-os && cd ~/engineering-os

git clone git@github.com:truelogics/ai-memory.git
git clone git@github.com:truelogics/engineering-mcp.git
git clone git@github.com:truelogics/engineering.git
```

**The directory names matter.** `engineering-mcp/go.mod` carries

```
replace github.com/truelogics/ai-memory => ../ai-memory
```

so the kernel is resolved by path, not by version. Clone it under any
other name, or anywhere that is not a sibling, and the build fails with:

```
github.com/truelogics/ai-memory@v0.1.0-alpha: replacement directory ../ai-memory does not exist
```

which is accurate and, the first time you meet it, unhelpful. There is
nothing to configure — move the directory.

The third repository is your **rulebook**: whichever repository holds your
organization's engineering rules, ADRs and standards. For this
organization that is `engineering`. For yours it may be a `docs` repo, a
handbook, or a directory inside your main application. It does not need to
be a separate repository; it needs to exist.

## 2. Build both binaries

```bash
mkdir -p ~/.local/bin
(cd ~/engineering-os/ai-memory       && go build -o ~/.local/bin/eng ./cmd/eng)
(cd ~/engineering-os/engineering-mcp && go build -o ~/.local/bin/engineering-mcp ./cmd/engineering-mcp)
```

Then put that directory on your `PATH`, in your shell profile rather than
just this session:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Check both:

```bash
eng version                 # eng version 0.1.0-dev
engineering-mcp --version   # engineering-mcp 0.1.0-alpha
```

Installing somewhere on `PATH` is not cosmetic. Claude Code launches the
server by absolute path, and if you later rebuild to a *different*
location you end up with two binaries and a session that keeps running the
older one. `eng doctor` checks for exactly this and says so.

## 3. Create a workspace

A **workspace** is an indexing boundary — one `.eng/memory.db` over one or
more repositories. It is not a repository, and usually should not be one.

The useful shape is a directory that *contains* the repositories you want
indexed together:

```bash
cd ~/engineering-os
eng workspace create .
eng workspace detach .
eng workspace attach ./engineering
eng workspace attach /path/to/your-application
eng workspace list
```

Two steps deserve explanation.

**Why `detach .` immediately after `create .`** — `create` registers its
own directory as a repository, which is right when the workspace *is* a
single repository and wrong when it is a parent of several. Left attached,
every document in every child repository is indexed twice: once under its
own repository and once under the root's. Citations then read
`engineering-os:rules/logging.md` for a file that belongs to
`engineering`, and two repositories become indistinguishable. Detaching is
how you say "this directory is a container".

**Why `attach` and not `index`** — `attach` indexes the repository as it
attaches it. There is no separate index step during setup. `eng index .`
is the *refresh* command, for later.

`eng init` is an alias for `eng workspace create`. Same command, two
names; older documents use the other one.

### What to attach

Attach the rulebook and the code you actually work on.

Do not attach benchmark fixtures, example projects, or scratch
repositories. They contain plausible ADRs that were never decided and
rules nobody agreed to, and retrieval cannot tell them from the real
thing.

## 4. Register the server with Claude Code

Once, at user scope, for every project on the machine:

```bash
claude mcp add engineering --scope user \
  -e ENGINEERING_WORKSPACE="$HOME/engineering-os" \
  -- "$HOME/.local/bin/engineering-mcp"
```

Note there is no `--workspace` argument. The server resolves one at every
start, in order of how specific the instruction was:

1. an explicit `--workspace`, if you pass one;
2. the nearest indexed workspace at or above the directory Claude Code was
   started in — so a project with its own `.eng/` serves its own knowledge;
3. `$ENGINEERING_WORKSPACE`, so the rulebook follows you into projects
   that are not themselves indexed.

It prints which workspace it chose, and why, to stderr.

Project scope works too if you would rather commit the configuration: copy
[`integration/claude-code/mcp.json.example`](integration/claude-code/mcp.json.example)
to `.mcp.json` at your project root. Claude Code asks you to approve it the
first time you open a session there.

## 5. Install the `/review-branch` command

```bash
mkdir -p ~/.claude/commands
cp ~/engineering-os/engineering-mcp/integration/claude-code/review-branch.md ~/.claude/commands/
```

`~/.claude/commands/` does not exist on a fresh machine, hence the
`mkdir`.

This step reads like a convenience and is not one. On a machine with
several MCP servers installed, Claude Code defers tool schemas: the tool
is absent from the model's prompt and cannot be called at all until
something names it. In a controlled experiment, the same server with the
same descriptions was called 3 times out of 40 tool calls with this
command available, and 0 times out of 42 without it
([`docs/reports/TOOL_DISCOVERY_EXPERIMENT.md`](docs/reports/TOOL_DISCOVERY_EXPERIMENT.md)).

Installing it is necessary and not sufficient — see
[`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md) on invoking it by name.

## 6. Verify

```bash
cd /path/to/your-application
eng doctor
```

`eng` is the entry point to Engineering OS (RFC-0008). `eng doctor`
delegates to `engineering-mcp doctor`, which is where the MCP and Claude
Code checks live — you do not need to know that, and this is the last
time this document mentions it.

Eight checks, ordered the way the system is layered, so the first failure
is the cause and the ones after it are symptoms:

| Check | Answers |
|---|---|
| Engineering MCP | is the binary installed, and is it the one on `PATH`? |
| AI Memory (`eng`) | is the CLI available? |
| Workspace | which workspace answers here, and why? |
| Workspace index | which repositories are in it — including this one? |
| Engineering knowledge | given real files from this repository, does the rulebook have anything to say? |
| Claude Code registration | is the server registered, connected, and pointing at this binary? |
| MCP handshake | does the server actually start and speak the protocol? |
| `/review-branch` command | is the command that makes the tools reachable installed? |

Warnings do not fail the run. They describe a system that works and may be
answering the wrong question — which is yours to judge, not doctor's.

## 7. Use it

```bash
cd /path/to/your-application
claude
```

> /review-branch

Invoke it by name. A plain-language *"Review my current branch."* can be
claimed by any other review skill installed on the machine, and if it is,
Engineering OS is never reached — see
[`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md).

## Keeping the index current

The index is a snapshot. After engineering documents change:

```bash
eng sync ~/engineering-os      # incremental, uses git to find what changed
eng index ~/engineering-os     # full re-index, when in doubt
```

Both cover every attached repository. Nothing detects staleness on its
own: a review against a stale index cites rules that may since have been
superseded, with nothing in the output saying so. Re-indexing is still the
developer's job.

## When something is wrong

Run `eng doctor` first. Beyond that:

**`replacement directory ../ai-memory does not exist`** — the two clones
are not siblings, or `ai-memory` is under a different name. See step 1.

**`go.mod requires go >= 1.25.0`** — upgrade Go, or unset
`GOTOOLCHAIN=local` and let Go fetch the toolchain.

**`no indexed workspace found at or above ...`** — you are outside every
workspace and `ENGINEERING_WORKSPACE` is unset. Either is fixable; the
error prints both fixes.

**Rules come back empty for every file** — the workspace that answered
probably holds no rulebook. `eng workspace list` shows what is in it. A
workspace containing only your application answers "no engineering rule
governs these files" with complete confidence, and that answer is
indistinguishable from a correct one. `doctor`'s *Engineering knowledge*
check exists for this case.

**A stale `.eng/` wins over the one you meant** — resolution takes the
*nearest* workspace, so a `.eng/` left inside a single repository beats
the workspace above it. Three such directories were found on one machine
during Sprint 11, holding 64 documents and zero rules between them. The
server prints which workspace it chose on stderr; `doctor` prints it too.

**Claude Code never calls the tools** — check the `/review-branch` command
is installed (step 5). This is the single most common cause and it looks
exactly like the server being broken.
