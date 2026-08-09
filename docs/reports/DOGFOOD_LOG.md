---
doc: REPORT
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Dogfood log

Sprint 11 Milestone 5: run the workflow on real repositories and record
what happened. Observations only — fixes belong to a later sprint, once
there is enough here to see a pattern rather than a coincidence.

One entry per review. The fields are the ones the sprint asked for, plus
two that turned out to matter more than expected: which workspace
answered, and what the developer still had to do by hand.

---

## Setup under test

Registered once, at user scope, with no workspace argument:

```bash
claude mcp add engineering --scope user \
  -e ENGINEERING_WORKSPACE=/Users/…/truelogics \
  -- ~/.local/bin/engineering-mcp
```

`/review-branch` installed to `~/.claude/commands/`. Nothing is
project-scoped, so every run below started with nothing but `cd <repo>`
and `claude`.

---

## Run 1 — ai-memory / sprint-11-fts-hyphen-fix (first attempt)

| | |
|---|---|
| Repository | `ai-memory` |
| Branch | `sprint-11-fts-hyphen-fix`, base `419cb46` |
| Changed files | `internal/cli/cli.go`, `internal/search/search.go`, `internal/search/userquery_test.go`, `pkg/memory/memory.go` |
| Workspace that answered | `…/truelogics` (walk-up) |
| Tools used | `find_engineering_rules` ×1, `get_context` ×1, `verify_evidence` ×3 |
| Turns / wall / cost | 27 / 380s / $1.29 |
| Outcome | **Incomplete** — `API Error: Connection closed mid-response` |

Ended on a transport failure, not a product failure, and the run got far
enough to settle the sprint's first three milestones before it died.

**What it establishes.** The server appeared as `engineering: connected`
with no `--mcp-config`, no project `.mcp.json`, and no approval prompt —
the user-scope registration works. The model chose the `review-branch`
skill over the machine's generic `code-review` skill, which the earlier
Sprint 11 validation had found suppressing retrieval entirely. It derived
repository root, branch, base and diff from git unaided, then called
`find_engineering_rules` with all four changed paths before reading any
source.

**The fix being used correctly.** All three `verify_evidence` calls
passed `changed_paths`. A session with no memory of this sprint read the
updated command and used the repaired argument, which is the only real
test of whether a tool-shaped fix landed.

**Friction.** It spent calls 8–14 re-deriving from source what
`get_context` could have told it, and only called `get_context` at step
15 — after building its own picture. Retrieval placed second to reading
the code, despite the command saying retrieve first.

---

## Run 2 — engineering-mcp / sprint-11-claude-code (first attempt)

| | |
|---|---|
| Repository | `engineering-mcp` |
| Branch | `sprint-11-claude-code`, base `e5491e8` |
| Workspace that answered | `…/truelogics` (walk-up) |
| Tools used | none — died first |
| Turns / wall / cost | 10 / 64s / $0.48 |
| Outcome | **Incomplete** — `API Error: Connection closed mid-response` |

Died during git and file inspection, before any retrieval. Recorded
because a log that only keeps successful runs overstates how reliable the
workflow is: two of two first attempts ended in a transport error, and a
developer would have experienced both as the tool failing.

---

## Run 3 — engineering-mcp / sprint-11-claude-code (retry)

| | |
|---|---|
| Workspace that answered | `…/truelogics` (walk-up) |
| Tools used | `find_engineering_rules` ×1, `get_context` ×1, `verify_evidence` ×4, `search_memory` ×0, Bash ×19, Read ×6 |
| Rules retrieved | 8 — applied 2, checked-and-compliant 3, not applicable 3 |
| Evidence | 2 verified, 2 kept as read-from-disk with the kernel limitation disclosed |
| Turns / wall / cost | 35 / 240s / $1.56 |
| Outcome | **Complete.** Five findings, one blocking |

**The blocking finding is the most valuable output of the sprint.**
`.gitignore:2` read `engineering-mcp` with no leading slash, so git
matched it at any depth — including the directory `cmd/engineering-mcp/`.
The server's `main` package had **never been committed**, in any commit,
since Sprint 8. The public repository held 16 files and could not build,
and every install instruction begins
`go build ./cmd/engineering-mcp`.

Reproduced before acting:

```
$ git check-ignore -v cmd/engineering-mcp/main.go
.gitignore:2:engineering-mcp    cmd/engineering-mcp/main.go
$ git log --all --oneline -- cmd/     # no output — never tracked
$ git ls-files | wc -l                # 16
```

Nine sprints of design documents, RFCs, unit tests, a benchmark suite and
a prior end-to-end validation did not catch it. `go build ./...` passes
locally because the file is present in the working tree; only a clone
would fail, and nobody had cloned it.

It also found that the resolver returned bare unwrapped errors
(`go-wrap-errors`, `severity: error`, governs the file), that
`mcp.json.example` still pinned an absolute `--workspace` — the exact
practice the new resolver argues against — and that walk-up outranks an
explicitly configured `$ENGINEERING_WORKSPACE`, so a stray `.eng/` holding
*a few wrong rules* answers with no disclosure at all, since the
zero-rule disclosure only fires when the list is empty.

All fixed except the precedence order, which was answered instead by
naming the answering workspace in every rule response. Reordering would
break the legitimate case of a project owning its own workspace.

---

## Run 4 — ai-memory / sprint-11-fts-hyphen-fix (retry)

| | |
|---|---|
| Workspace that answered | `…/truelogics` (walk-up) |
| Tools used | `find_engineering_rules` ×1, `get_context` ×1, `verify_evidence` ×2, Bash ×22 |
| Rules retrieved | 7 — applied 1, explicitly ruled out 2 |
| Turns / wall / cost | 33 / 285s / $1.50 |
| Outcome | **Complete.** Three findings, both substantive ones real |

It did not take the unit tests' word for the fix. It built an in-memory
FTS5 table and ran the raw and escaped queries against real SQLite,
confirming the crash, the repair, and that implicit AND survived.

Two real defects in a change written an hour earlier:

1. **An 839 KB stale workspace database was committed** — swept in by a
   `git add -A` after the three stale `.eng/` directories were renamed
   aside. `.gitignore` had `/*.db` (root-anchored) and `.eng/` (exact
   name); `.eng.stale-20260809/` matched neither. Cited
   `pr-single-purpose`, verified high confidence.
2. **`UserQuery` still handed FTS5 a raw empty string** — `""` is
   `fts5: syntax error near ""`, the exact failure class the function was
   written to remove, reachable from both fixed entry points. Worse, the
   accompanying test *pinned the broken behaviour as intended*. The
   function now reports that there is nothing to search and callers
   return no results.

It also noted the escaping primitive now exists twice — the commit
message diagnosing "one path had the fix, the neighbouring one never got
it", then fixing it by adding a second copy. Recorded, not fixed.

And it flagged a cross-repository consequence unprompted: this branch
resolves `KERNEL_REQUIREMENTS.md` #1, which makes the "split hyphenated
names into separate words" workaround in `review-branch.md` stale.

---

## Observations after four runs

**The workflow works.** `cd <repo>` and `claude`, nothing project-scoped,
no approval prompt, no manual context. Both completed runs derived
repository, branch, base and diff from git unaided, retrieved before
reasoning, and cited `repository:path`.

**It found real, shipped defects immediately.** A repository that could
not build, a binary committed by accident, and a bug pinned by its own
test — none of them visible to the tests, the benchmarks or the previous
sprint's validation.

**Reviewing its own author's work is where it pays.** Every substantive
finding was against code written the same day by the same author. The
value is not that the reviewer is clever; it is that it is not the person
who just wrote the change.

**Retrieval is not yet first, despite being told to be.** Run 1 called
`get_context` at step 15, after building its own picture from source. Run
4 used 22 Bash calls to 4 MCP calls. The tools are used to *confirm* an
understanding reached by reading code, not to form it. Whether that
ordering matters is worth watching before it is worth fixing.

**`search_memory` went unused in the completed engineering-mcp run**, and
the reviewer said why: `get_context` had already returned what it needed,
and the topic terms were hyphenated repository names — the crash that was
still open at the time. That reason is now gone; watch whether it gets
used at all.

**Half of retrieval is still noise.** Six of twenty related documents in
Run 3 were the branch's own files, ranked top at 0.80–0.49, and
`get_context` reported "Architecture decision records: (none)" — the
taxonomy gap in `KERNEL_REQUIREMENTS.md` #3.

**Reliability is the weakest link, and it is not ours.** Two of four runs
died mid-response on a transport error. Cost is $1.30–$1.56 and wall time
240–380s per review, which is affordable per branch and would not be per
commit.

### Questions for the observation week

1. Does retrieval ever move ahead of reading the code, or is confirming a
   read understanding the honest workflow?
2. Now that hyphens work, does `search_memory` get used?
3. How often does the kernel's snippet-only verification cost a true
   citation? Both completed runs hit it once each.
4. Which rules are retrieved repeatedly and never apply?
   `deterministic-stages`, `no-internal-imports` and
   `rfc-before-public-api-change` were retrieved and dismissed in both
   runs — candidates for narrower `applies_to`, not deletion.
