---
doc: REPORT
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Clean-machine simulation

Sprint 15, Milestone 5. Installation was performed from scratch in an
empty directory with a fake `$HOME`, an empty `~/.claude`, and only the
documentation as a guide — no local knowledge allowed, no step taken
because it was remembered rather than written down.

The documentation was written **after** this run and from it. Writing it
first would have produced a description of what I already knew, which is
the failure mode this milestone exists to avoid.

Twelve findings. Four were blocking, four are fixed in code, four became
documentation.

---

## Blocking

### 1. The default branch does not build

A developer clones `main` and cannot get past step one:

```
$ git clone …/engineering-mcp && cd engineering-mcp
$ go build ./cmd/engineering-mcp
stat …/engineering-mcp/cmd/engineering-mcp: directory not found
```

`git ls-tree origin/main` shows five entries and no `cmd/`. This is the
`.gitignore` defect found in Sprint 11 — an unanchored `engineering-mcp`
pattern matching `cmd/engineering-mcp/` at any depth — which was fixed on
a branch and never merged.

The Definition of Done is *"installed and used by another developer
without needing the project author"*. It cannot be met while the default
branch is missing the server's main package. **Nothing else in this sprint
matters until `sprint-11-claude-code` is merged.**

### 2. The build requires an undocumented directory layout

`go.mod` carries `replace github.com/truelogics/ai-memory => ../ai-memory`,
so the kernel is resolved by path. Clone the two repositories under any
other names, or anywhere that is not adjacent, and:

```
github.com/truelogics/ai-memory@v0.1.0-alpha: replacement directory ../ai-memory does not exist
```

Accurate, and meaningless to someone who has just met both repositories.
No document said the directory names were load-bearing. Now `INSTALL.md`
step 1 does, with this error quoted next to it.

### 3. The Go version requirement was unstated

`go.mod` requires 1.25.0. The system Go was 1.24.5, and the build
succeeded anyway because Go silently downloaded the newer toolchain.
Re-running with `GOTOOLCHAIN=local`:

```
go: go.mod requires go >= 1.25.0 (running go 1.24.5; GOTOOLCHAIN=local)
```

So the real prerequisite is "Go 1.25, or an older Go with network access",
and a developer who is offline or has pinned their toolchain hits a wall
that nothing warned them about. Now in `INSTALL.md` prerequisites.

### 4. There were no diagnostics

`eng doctor` existed as a stub that printed *"not yet implemented (see
docs/cli/CLI.md)"* and exited 1 — a pointer to a design document, offered
to someone whose install is broken.

Six questions had no single answer: is the server installed, is the CLI
available, is the workspace valid, is this repository indexed, is Claude
Code connected, is the server reachable. Each was answerable by hand, by
someone who already knew how.

Now `engineering-mcp doctor`, eight checks, ordered so the first failure
is the cause. It lives here rather than in `eng` because four of the six
questions are about MCP and Claude Code, which the kernel knows nothing
about and should not learn.

---

## Fixed in code

### 5. `eng sync` skipped every attached repository — and silently undid a documented step

The worst finding of the run, and it was only visible because the
simulation followed the documentation exactly.

`eng index` iterates every repository attached to the workspace; its own
comment explains why — *"the workspace is the indexing boundary, so it is
also the re-indexing boundary"*. `eng sync` was never given the same
treatment. It called `findOrCreateRepository` on the workspace directory
and synced that alone.

On the layout the documentation recommends — a workspace root that is the
*parent* of several repositories, detached on purpose — that means sync
skipped all of them, re-registered the root the user had just detached,
and indexed every child repository's documents a second time under the
root's name:

```
$ eng workspace detach .
Detached clean-machine: 0 documents removed
$ eng sync .
clean-machine: 82 scanned, 82 added, …          ← the root, back again
$ eng status .
ai-memory        35
clean-machine    82                             ← everything, twice
engineering      39
```

`repository:path` citations stop distinguishing repositories, which is the
exact hazard the install instructions warn the reader about — created by
the tool, one command after the instruction to avoid it.

Fixed: `Sync` now mirrors `Index`, covering every attached repository and
aggregating per-repository failures. Regression test:
`TestSyncCoversTheWholeWorkspace`, written as the documented layout rather
than as a unit of `Sync`, because the layout is what made the bug
invisible.

### 6. A mistyped subcommand started the server

`engineering-mcp doctr` was a positional argument, which `flag` ignores.
The server started and waited on stdin — indistinguishable from a command
that hung. It now reports the unknown argument and prints usage.

### 7. `eng doctor` pointed at a design document

Replaced with a pointer to `engineering-mcp doctor`, and the "planned, not
yet implemented" section of `eng`'s usage replaced with a Diagnostics
section naming it.

### 8. This repository's own documents were unclassifiable

The four documents this sprint produced all carry front matter, all
describe themselves accurately, and all indexed as `unknown`: the kernel's
front-matter switch has no `GUIDE` case, although `DocTypeGuide` exists
and a taxonomy file can reach it.

The documents whose entire purpose is to be found by someone who does not
know where to look were the ones retrieval could not name.

Not fixed — that is a kernel change and this is a documentation sprint.
Recorded as `KERNEL_REQUIREMENTS.md` #6 and worked around with the
intended tool: a `.engineering.yaml` mapping this repository's own
directories. After it, `eng ask "how do I install Engineering OS"` returns
`ONBOARDING.md`, `QUICKSTART.md` and `INSTALL.md` under a Guides heading.

---

## Became documentation

### 9. `create .` then `detach .` — step two undoes step one

The only documented path to a multi-repository workspace opens by
registering the root and immediately unregistering it. It is not a
mistake — `create` registering its own directory is right when the
workspace *is* one repository — but presented without explanation it reads
as a typo, and a reader who "corrects" it by skipping the detach gets
finding 5's duplication permanently.

Left as it is, and explained. Changing `create` would break the
single-repository case that is most of its use.

### 10. `eng init` and `eng workspace create` are the same command

Identical output, identical effect. Different documents used different
ones, and Sprint 13's proposed onboarding flow used a third phrasing.
`INSTALL.md` now names one and mentions the alias once.

### 11. `eng index .` after setup was redundant

`eng workspace attach` indexes as it attaches. The instruction to run
`eng index .` afterwards made a no-op look like a required step, which
teaches a reader that the tool needs supervision. `index` and `sync` are
now documented as what they are: refresh commands, for later.

### 12. `~/.claude/commands/` does not exist on a fresh machine

`cp review-branch.md ~/.claude/commands/` fails. Every instance now has
`mkdir -p` in front of it.

---

## What moved, and why

Install instructions lived in `integration/claude-code/README.md` — three
directories down, in a file named for an integration a newcomer has not
heard of yet. They are now in `INSTALL.md` at the root, with
`QUICKSTART.md` beside it for the ten-minute path, `ONBOARDING.md` for the
per-repository work, and `docs/CLAUDE_CODE.md` for the client. The
integration README shrank to what it should have been: two files and what
each is for.

## The honest limit of this exercise

A simulation is not a clean machine. Go's module cache, git, and a working
`claude` install were all present, and reconstructing them would have
tested the operating system rather than this project. Findings 1, 2, 3 and
12 came from removing local assumptions; the rest came from following the
steps.

The strongest evidence that the documentation is now true is not this
report. It is that the sequence in `QUICKSTART.md` was executed verbatim
in an empty directory, and produced a working install without a single
step that is not written down.

---

## What the review of this work found

Sprint 15's own branch was reviewed with Engineering OS before merging,
per the standing instruction not to skip review on your own project. It
found more than the clean-machine run did, and one finding was a false
claim in this repository's new code.

### The diagnostic reported failure on a correctly installed machine

`INSTALL.md` sets `ENGINEERING_WORKSPACE` inside `claude mcp add -e ...`,
which places it in the environment of the server Claude Code spawns and
nowhere else. `doctor` resolved the workspace from *its own* environment.
So a developer who followed the instructions exactly, standing in an
application that is not itself inside a workspace, got:

```
  ✘  Workspace              no indexed workspace found …
  ✘  Workspace index        skipped: no workspace resolved
  ✘  Engineering knowledge  skipped: no workspace resolved
  ✔  Claude Code registration   Status: ✔ Connected
  ✘  MCP handshake          the server closed the connection …
```

Four failures one line below a green connection, and the advice attached
to the first — `eng workspace create .` in the application — creates
exactly the nested `.eng/` that `INSTALL.md` warns shadows the real
workspace. The diagnostic manufactured the misconfiguration it exists to
catch. Reproduced, then fixed: `doctor` now reads the registered
environment out of `claude mcp get` and diagnoses the resolution the
*registered server* will perform.

### It advised something that does nothing

The first fix carried the line *"export ENGINEERING_WORKSPACE=…, to use
eng from this directory too"*. `eng` does not read that variable. It
contains no `os.Getenv` call at all; every workspace subcommand takes a
positional path defaulting to the current directory. Verified by grep and
by running it.

The review caught this by checking the claim against the other
repository rather than against the sentence. A developer who followed the
advice would have seen no change and concluded the install was broken —
worse than no advice.

### Every `eng workspace attach` fix it printed would have failed

`eng workspace attach` has no way to be told which workspace to attach
to: it operates on the current directory's and refuses to create one. The
fixes `doctor` printed were bare `eng workspace attach <repo>`, and they
fire precisely when the working directory is *not* the workspace root.
Run as printed, they fail with "run `eng init` first" — whose advice is,
again, the nested `.eng/`. Now prefixed with `cd <workspace>`.
`ONBOARDING.md` had the same defect in its first instruction.

### Rules the review cited, correctly

- `go-wrap-errors` (severity error) — `internal/doctor/system.go` returned
  bare errors and used `fmt.Errorf` without a package prefix, while
  `internal/workspace/resolve.go`, added on the same branch, prefixes
  every error. Two new packages, one branch, opposite conventions. Fixed.
- `no-silent-fallback` — `runDoctor` substituted the bare string
  `engineering-mcp` when `os.Executable` failed, which then made three
  checks compare a real path against a name and report "two builds are
  installed". A substitution indistinguishable from the real thing. Now
  announced on stderr.
- `pr-single-purpose` (severity warn) — this branch is one diagnostic
  subcommand, a tool-surface change, a taxonomy file and ~1,900 lines of
  documentation. The sprint framing is deliberate, so it stands, but the
  note is fair and recorded rather than argued away.

### And one thing the review found that the sprint's own design missed

`find_engineering_rules` names the workspace that answered, with the
argument that a workspace holding stale rules answers with exactly the
confidence of a correct one. `get_context` returns rules too — under its
own heading, and its description recommends it as the broadest
capability — and named nothing. The argument was made once and applied
once. Now applied to both, with a test that covers every tool returning
rules rather than one tool by name.

## Milestone 6: the workflow does not hold as specified

The Definition of Done specifies `claude` → *"Review my current
branch."* → Engineering MCP → review, with "no manual intervention". Run
both ways on the same commit, minutes apart:

| Asked as | Skill that ran | MCP calls | Tool calls |
|---|---|---|---|
| "Review my current branch." | `review-pull-requests` | **0** | 37 |
| `/review-branch` | `review-branch` | **9** | 59 |

The plain sentence was claimed by another project's review skill, which
never names Engineering MCP, so the deferred tools were never loaded. The
command was installed the whole time; installing it is necessary and not
sufficient.

The documentation now tells the reader to type `/review-branch`. That is
one word of manual intervention more than the sprint specified, and it is
the truth. Full measurement in
[`TOOL_DISCOVERY_EXPERIMENT.md`](TOOL_DISCOVERY_EXPERIMENT.md).
