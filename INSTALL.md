---
doc: RUNBOOK
audience: [human]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-12
---

# Installing Engineering OS

This is the long version, with what each step produces and what goes
wrong. If you just want it working, read
[`QUICKSTART.md`](QUICKSTART.md) — three commands — and come back here
when something fails.

Installation is per-machine and happens once. Making a *repository*
Engineering OS aware is a separate, per-repository job —
[`ONBOARDING.md`](ONBOARDING.md).

## What you are installing

Two binaries and one index.

| | What it is | Who runs it |
|---|---|---|
| `eng` | the Engineering Kernel CLI: builds and maintains the index, and installs the rest | you |
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

## The short version

```bash
go install github.com/truelogics/engineering-kernel/cmd/eng@latest
export PATH="$(go env GOPATH)/bin:$PATH"        # in your shell profile

eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

The rest of this document is what that does, and how to do each part by
hand when it does not work.

## 1. Get the binaries

```bash
go install github.com/truelogics/engineering-kernel/cmd/eng@latest
go install github.com/truelogics/engineering-mcp/cmd/engineering-mcp@latest
```

`eng setup` runs the second one for you if `engineering-mcp` is not
already on `$PATH`, so you rarely type it.

Both land in `$(go env GOPATH)/bin` — usually `~/go/bin` — which is not
on `$PATH` by default. Put it there in your shell profile rather than
just this session:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Check both:

```bash
eng version                 # eng version v0.3.0
engineering-mcp --version   # engineering-mcp 0.1.0-alpha
```

Installing somewhere on `$PATH` is not cosmetic. Claude Code launches the
server by absolute path, and if you later rebuild to a *different*
location you end up with two binaries and a session that keeps running
the older one. `eng doctor` checks for exactly this and says so.

### Building from a clone instead

If you are going to change the code, clone and build:

```bash
mkdir -p ~/engineering-os && cd ~/engineering-os
git clone git@github.com:truelogics/engineering-kernel.git
git clone git@github.com:truelogics/engineering-mcp.git

mkdir -p ~/.local/bin
(cd engineering-kernel && go build -o ~/.local/bin/eng ./cmd/eng)
(cd engineering-mcp    && go build -o ~/.local/bin/engineering-mcp ./cmd/engineering-mcp)
export PATH="$HOME/.local/bin:$PATH"
```

The directory names no longer matter, and for a while they did.
`engineering-mcp/go.mod` used to carry a `replace` pointing at a sibling
directory, so the kernel was resolved by path: a clone under any other
name, or anywhere not adjacent, failed with `replacement directory ...
does not exist` — accurate, and meaningless the first time you meet it.
It now depends on a released version, so each repository builds alone.

If you are going to change both repositories at once, put a `go.work` at
the directory containing them so your local kernel edits are visible to
the server without a release:

```bash
cd ~/engineering-os && go work init ./engineering-kernel ./engineering-mcp
```

Do not commit it to either repository — it belongs to your machine, and
committing it would reimpose the layout requirement it exists to remove.

After rebuilding, run `engineering-mcp install` so Claude Code launches
the build you just made rather than the one it was registered with.

## 2. `eng setup`

```bash
eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

Four steps, reported as it goes.

**[1/4] Workspace.** A workspace is an indexing boundary — one
`.eng/memory.db` over one or more repositories. It is not a repository,
and usually should not be one. The useful shape is a directory that
*contains* the repositories you want indexed together.

`setup` creates it, and then detaches the root from its own index when
the root is not itself a git repository. That step used to be a manual
`eng workspace detach .` immediately after `create`, and everybody
skipped it, because nothing on screen said why it was there. Left
attached, every document in every child repository is indexed twice: once
under its own repository and once under the root's. Citations then read
`engineering-os:rules/logging.md` for a file that belongs to
`engineering`, and two repositories become indistinguishable.

If you point `setup` at a directory that *is* a git repository, it stays
attached — that is the single-repository workspace, and detaching there
would leave an index of nothing.

**[2/4] Repositories.** Each `--rules` and `--repo` is cloned if it is a
git URL, then attached and indexed. `attach` indexes as it goes; there is
no separate index step during setup. `eng index` is the *refresh*
command, for later.

One bad path does not stop the others: it is reported, the rest are
attached, and `setup` exits non-zero at the end naming what failed.

**What to attach.** The rulebook and the code you actually work on. Do
not attach benchmark fixtures, example projects, or scratch repositories.
They contain plausible ADRs that were never decided and rules nobody
agreed to, and retrieval cannot tell them from the real thing.

**[3/4] `engineering-mcp`.** Installed with `go install` if it is not
already on `$PATH`. If the install succeeds and the binary still is not
found, `$(go env GOPATH)/bin` is not on your `$PATH` — `setup` says so
and names the directory, because "command not found" immediately after a
successful install is otherwise unreadable.

**[4/4] Claude Code.** `setup` hands over to `engineering-mcp install`,
which is the component that owns this. See the next section.

## 3. What `engineering-mcp install` does

You can run it on its own — after rebuilding, or if you skipped `setup`:

```bash
engineering-mcp install [--workspace <dir>] [--force]
```

**Registers the server**, once, at user scope, for every project on the
machine — the equivalent of:

```bash
claude mcp add engineering --scope user \
  -e ENGINEERING_WORKSPACE="$HOME/engineering-os" \
  -- "$HOME/go/bin/engineering-mcp"
```

Two absolute paths, neither of which anyone has to hand, which is why
this is a command. It removes any existing registration first: `claude
mcp add` refuses a name that already exists, so without that, the command
whose whole job is to repoint a stale installation would fail on every
machine that had one.

Note there is no `--workspace` argument in the registration. The server
resolves one at every start, in order of how specific the instruction
was:

1. an explicit `--workspace`, if you pass one;
2. the nearest indexed workspace at or above the directory Claude Code
   was started in — so a project with its own `.eng/` serves its own
   knowledge;
3. `$ENGINEERING_WORKSPACE`, so the rulebook follows you into projects
   that are not themselves indexed.

It prints which workspace it chose, and why, to stderr.

**Installs the `/review-branch` command** into
`~/.claude/commands/review-branch.md`, from a copy embedded in the
binary — so this works from a `go install` with no clone to copy out of.
An existing file that differs is left alone and reported, on the
assumption that you edited it; `--force` overwrites.

This step reads like a convenience and is not one. On a machine with
several MCP servers installed, Claude Code defers tool schemas: the tool
is absent from the model's prompt and cannot be called at all until
something names it. In a controlled experiment, the same server with the
same descriptions was called 3 times out of 40 tool calls with this
command available, and 0 times out of 42 without it
([`docs/reports/TOOL_DISCOVERY_EXPERIMENT.md`](docs/reports/TOOL_DISCOVERY_EXPERIMENT.md)).

Installing it is necessary and not sufficient — see
[`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md) on invoking it by name.

**If Claude Code is not installed**, `install` skips the registration
rather than failing, and prints the command and environment variable to
give any other MCP client. **If no workspace resolves**, it refuses to
register: a registration pointing at a server that exits on startup fails
every session while `claude mcp list` reports it as configured.

Project scope works too if you would rather commit the configuration:
copy
[`integration/claude-code/mcp.json.example`](integration/claude-code/mcp.json.example)
to `.mcp.json` at your project root. Claude Code asks you to approve it
the first time you open a session there.

## 4. Verify

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
| Engineering Kernel (`eng`) | is the CLI available? |
| Workspace | which workspace answers here, and why? |
| Workspace index | which repositories are in it — including this one? |
| Engineering knowledge | given real files from this repository, does the rulebook have anything to say? |
| Claude Code registration | is the server registered, connected, and pointing at this binary? |
| MCP handshake | does the server actually start and speak the protocol? |
| `/review-branch` command | is the command that makes the tools reachable installed? |

Warnings do not fail the run. They describe a system that works and may be
answering the wrong question — which is yours to judge, not doctor's.

## 5. Use it

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
eng update ~/engineering-os     # incremental, uses git to find what changed
eng index  ~/engineering-os     # full re-index, when in doubt
```

Both cover every attached repository. Nothing detects staleness on its
own: a review against a stale index cites rules that may since have been
superseded, with nothing in the output saying so. Re-indexing is still the
developer's job.

## Adding a repository later

```bash
eng setup ~/engineering-os --repo ~/code/another-application
```

`eng setup` is re-runnable. It attaches what is new, re-indexes what is
not, and repoints Claude Code at the current binary. Or, directly:

```bash
cd ~/engineering-os && eng workspace attach ~/code/another-application
```

## When something is wrong

Run `eng doctor` first. Beyond that:

**`go.mod requires go >= 1.25.0`** — upgrade Go, or unset
`GOTOOLCHAIN=local` and let Go fetch the toolchain.

**`command not found: eng` right after a successful `go install`** —
`$(go env GOPATH)/bin` is not on your `$PATH`.

**`replacement directory ../engineering-kernel does not exist`** — an old
checkout with the removed `replace` directive. Pull, or see step 1.

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

**Claude Code launches a binary you rebuilt somewhere else** — run
`engineering-mcp install` again.

**Claude Code never calls the tools** — check the `/review-branch`
command is installed. This is the single most common cause and it looks
exactly like the server being broken.
