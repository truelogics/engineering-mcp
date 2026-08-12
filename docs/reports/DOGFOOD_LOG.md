---
doc: REPORT
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Dogfood log

> **A note on repository names.** On 2026-08-12 `ai-memory` was renamed
> `engineering-kernel` and `ai-review` became `engineering-review`. Entries
> below are left as they were written. They are dated accounts of what
> happened under the names that existed then, and rewriting a record to
> match the present would quietly falsify the evidence this project's
> decisions rest on. Read `ai-memory` as `engineering-kernel` throughout.

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

### Questions for the observation week (asked after Sprint 11)

1. Does retrieval ever move ahead of reading the code, or is confirming a
   read understanding the honest workflow?
2. Now that hyphens work, does `search_memory` get used?
3. How often does the kernel's snippet-only verification cost a true
   citation? Both completed runs hit it once each.
4. Which rules are retrieved repeatedly and never apply?
   `deterministic-stages`, `no-internal-imports` and
   `rfc-before-public-api-change` were retrieved and dismissed in both
   runs — candidates for narrower `applies_to`, not deletion.

---

# Sprint 12 — the first repository outside this organization

Target 2 of Sprint 12: use Engineering OS on real software projects. This
section records the first one, because almost everything it exposed was
invisible from inside repositories written alongside the tool.

## Run 5 — pivot / feature/vorder-v2-backend

| | |
|---|---|
| Repository | `pivot` — a 12,552-file polyglot monorepo (TypeScript, Go, protobuf), unrelated to Engineering OS |
| Branch | `feature/vorder-v2-backend`, 90 commits, 85 files, +5,773 / −193 |
| Workspace | `pivot/.eng`, resolved by walk-up |
| Engineering OS tool calls | **0** |

### Setup exposed two kernel limitations before a review was possible

**Indexing did not finish.** After ten minutes the database had passed
233 MB and had not left the first directory. The collector skipped a
fixed list of names — `.git`, `node_modules`, `vendor` — which is
indistinguishable from correct on this organization's repositories.

| | markdown files |
|---|---|
| tracked by git | 726 |
| offered to the collector | **22,445** |
| git-ignored | 21,719 (96.8%) |
| agent plugin caches under `.claude/` | 21,690 |

Fixed by asking git what the project disowns. After: **727 scanned, 19
seconds, 24 MB**. `KERNEL_REQUIREMENTS.md` #4.

**The first clean run reported `1 errors` and named nothing** — no file,
no stage, no cause. `IndexResult` counted errors and discarded every
error value. This had already cost this organization once, unrecognised:
four engineering rules were silently unindexed in Sprint 7 by invalid
front matter, and the only signal was the same bare count. Two
occurrences, months apart, same missing information.

Fixed. The first run after printed the cause, which was a real defect in
the project: a plan file whose front-matter block a formatter had mangled
into `## title:` and never closed, silently absent from its index.
`KERNEL_REQUIREMENTS.md` #5.

### The project has knowledge; Engineering OS can classify none of it

| doc_type | documents |
|---|---|
| **unknown** | **662** |
| readme | 64 |
| rule / adr / rfc / standard / architecture | **0** |

91% unclassified, against 36% inside this organization. The project is
not undocumented — it has 416 planning documents, a 72-document
engineering handbook and 124 test documents. It simply does not use
`doc:` front matter, which is the state of every repository on the day it
adopts Engineering OS.

So `find_engineering_rules` returns nothing, `get_context` reports no
ADRs, and the handbook — where that project's engineering standards
actually live — is reachable only as a keyword hit indistinguishable from
an incidental match. Verified directly against the running server. The
new disclosure fired correctly, which is the right behaviour and not a
substitute for having something to say.

### The finding that matters most: it was never reached

Asked "Review my current branch", the model loaded four of the project's
own review skills — a `review-pull-requests` router, a `review-pivot`
product overlay, `review-protobuf`, `review-pivot-proto` — read the
protobuf contracts, and reviewed the branch in domain terms. It called
Engineering OS **zero times**.

That machine carries 20 user-level review skills (`review-pivot`,
`review-nats`, `review-messaging`, `review-realtime`, …) and 11 more
inside the project. This is not the Sprint 11 Run 0 failure, where a
generic `code-review` skill crowded out retrieval that would have helped.
Here the competing skills were **better**: purpose-built, domain-specific,
carrying a `lessons.yaml` of accumulated review history. And Engineering
OS had nothing to counter with — no rules, no ADRs, no classified
architecture.

Both facts point the same way. On this project Engineering OS was not
merely unused; it had no answer worth interrupting for.

**The run completed: 41 turns, 465s, $3.13, four Skills, 25 Bash, 4 Grep,
3 Read, zero Engineering OS calls.** It produced a good review — two
blocking correctness findings with exact line references, of the kind
only domain knowledge yields.

### And the decisive evidence was a document we had indexed

The first blocking finding does not rest on the code. It rests on a
planning document:

> both read paths changed to `Order(ent.Asc(blockdata.FieldVOrderV2), …)`.
> But the block_data backfill was deliberately dropped —
> `plans/to-do/block-fractional-indexing-vorder-v2.md:43-53` says the
> worker code was removed and the read sort is "retained here for
> reference only".

That is exactly the class of knowledge this platform exists to serve: a
written decision the diff contradicts, which no amount of reading the
code would reveal. It is in the workspace. It was indexed successfully.

It reached it with two shell commands:

```
grep -n -i "block_data\|blockdata" plans/to-do/block-fractional-indexing-vorder-v2.md
sed -n '41,56p;176,196p'          plans/to-do/block-fractional-indexing-vorder-v2.md
```

And the reason retrieval could not have supplied it:

```
plans/to-do/block-fractional-indexing-vorder-v2.md | unknown
```

Unclassified, so `get_context` can only ever offer it as an incidental
keyword hit among 662 siblings, never as the decision that governs the
change.

This is the most precise statement of the gap the sprint produced. Not
"the platform was ignored" — the platform held the winning card, could
not name it as a card, and `grep` beat it to the table. Everything the
taxonomy gap (#3) costs is visible in that one finding.

### What this says about where the value has actually been demonstrated

Stated plainly, because the sprint asks for evidence rather than ideas:

- Every review where Engineering OS changed the outcome — Sprint 11's
  seven findings, Sprint 12's repository-that-never-built — was **inside
  the organization whose rulebook it was built alongside**.
- On the first outside project it contributed nothing to the review, and
  the two things it did contribute were kernel bug reports produced by
  *setup*, not by retrieval.
- Its value is currently a function of `applies_to`-scoped rules and
  classified documents. A project with neither gets a working tool with
  an empty rulebook.

That is not an argument against the platform. It is a measurement of what
adoption actually requires, and it was not visible from inside.

## Recurring problems, by the improvement policy

Only these have occurred more than once or across more than one
repository:

1. **Unclassifiable documents** — `KERNEL_REQUIREMENTS.md` #3. Third
   sighting: 36% here, 91% on the outside project, and the org's own
   `KERNEL_POLICY`, `VISION` and `SYSTEM_MAP` among them. Run 5 priced it
   exactly: the document carrying that review's blocking finding was
   indexed, unclassified, and therefore unreachable as anything but a
   keyword hit — while `grep` found it in one call. This is the single
   biggest limit on the tool's usefulness, and it blocks adoption rather
   than merely degrading it. Not fixed: it needs a decision about the
   taxonomy, not code.
2. **A better-targeted skill wins** — second sighting (Sprint 11 Run 0,
   Sprint 12 Run 5), with opposite implications each time. Worth watching
   before acting: in the second case losing was the correct outcome.
3. **Evidence verification fails on true quotes** — `KERNEL_REQUIREMENTS.md`
   #2, hit once in each completed Sprint 11 review. Unfixed by choice.
4. **Retrieval runs second** — every completed run so far. The tools
   confirm an understanding built by reading code rather than forming it.
   Still an observation, not yet a problem anyone has felt.

## Not acted on

Isolated incidents, recorded and left alone per the improvement policy:
the `pending` MCP status seen once at init in `pivot` (a startup-timing
artifact — the server answered correctly when driven directly), and the
transport errors that killed two of four Sprint 11 runs.

---

# Sprint 13 — Repository Taxonomy (ai-memory RFC-0007)

Run 5's conclusion named the problem precisely enough to fix: retrieval
worked, and the kernel could not say what the retrieved document *was*.
This is the result of acting on it.

## What changed

A repository states what its own directories hold, in a
`.engineering.yaml` that lives in the repository:

```yaml
taxonomy:
  plans/**:          Decision
  plans/backlog/**:  Planning
  handbook/**:       Guide
```

The vocabulary the platform reasons over stays ours and stays small —
eight canonical types. The names a company gives its directories stay
theirs and stay open. Patterns reuse RFC-0005's `applies_to` globs, and
the longest match wins.

The safety property is precedence: a mapping applies **only** to
documents that would otherwise be `unknown`. Front matter is what a
document says about itself; a taxonomy is what a repository says about a
directory; the specific claim wins.

## Measured on pivot, with the four lines above

| | before | after |
|---|---|---|
| unknown | 662 (91%) | 177 (25%) |
| `adr` (Decision) | 0 | 153 |
| roadmap (Planning) | 0 | 260 |
| guide | 0 | 66 |
| readme | 64 | 64 |

And the specific document that made the case:

```
plans/to-do/block-fractional-indexing-vorder-v2.md | unknown  →  adr
```

`get_context`, for a change to the block ordering code, now returns:

```
## Architecture decision records
- pivot:plans/to-do/block-fractional-indexing-vorder-v2.md (score 0.80)
- pivot:plans/to-do/block-fractional-indexing-vorder-v2.md (score 0.76)
    ...Backfill input sort (`block_data`): N/A — `block_data`...
```

The passage ranked second is the one the Run 5 reviewer actually used for
its blocking finding, reached there by `grep`. It is now the second-ranked
decision for that change, retrieved.

## The regression check that mattered more than the improvement

A taxonomy change that quietly reclassified existing documents would look
identical to a regression in every consumer that groups by type. So the
acceptance criterion was a document-for-document comparison of this
organization's own workspace, before and after:

```
101 documents, 1 line of difference — rfcs/0007-repository-taxonomy.md,
the RFC written for this change, correctly classified as an rfc.
```

Nothing moved, because nothing *could* move: `unknown` is the only
eligible input.

## New friction, recorded not fixed

**One document can occupy a whole section.** Seven of the nine entries in
that Architecture-decision-records section are chunks of the same file.
Useful here — the passages differ and two of them matter — but a section
that can be monopolised by one document will crowd out a second relevant
decision. Second sighting of chunk-level duplication (Sprint 11 Run 3 saw
the same file at 0.80 and 0.44). Not yet costly enough to act on.

**A taxonomy is a judgement, and I made it.** `plans/** → Decision` is my
reading of that repository, written by someone who does not work on it.
It happens to be right about the file that mattered. The onboarding flow
this implies — a stranger guessing at directory meanings — is worse than
the repository's own team writing four lines, and the design assumes the
team writes them.

## The onboarding flow this changes

Before:

```
eng init  →  eng index
```

Now:

```
eng init  →  teach the repository to describe itself  →  eng index
```

That middle step is the first genuine onboarding problem the project has
found, and it was invisible until Engineering OS was pointed at something
it had not been built alongside.

---

# Validation Phase 1

Usage is now the source of the roadmap. `engineering/KERNEL_POLICY.md`
Rule #8 makes it binding: a capability must name the entry below that
exposed the need for it, or the work does not start.

## Run 6 — ai-memory / sprint-11-fts-hyphen-fix (the taxonomy branch)

| | |
|---|---|
| Repository | `ai-memory` |
| Task | Review the RFC-0007 taxonomy work before merging it |
| Workspace | `…/truelogics` (walk-up) |
| MCP tools called | **0** |
| Other tools | Skill ×1, Bash ×36 |
| Turns / wall / cost | 39 / 365s / $2.55 |
| Outcome | **Complete.** One blocking finding, three should-fix |

### Useful findings — three real, one blocking

**Blocking, and it invalidated my own verification.** Adding a
`.engineering.yaml` changes no markdown file's bytes, so the
content-hash short-circuit reported every existing document `Unchanged`
and left it `unknown`. RFC-0007 worked on a fresh index and did nothing
on an already-indexed repository — the only path that matters, since the
repositories needing a taxonomy are the ones already indexed without one.

My Sprint 13 verification had missed it because it began with
`rm -rf .eng`. The review did not take the tests' word for it; it ran the
real pipeline twice and showed the second run reporting `Unchanged:1`
with the type still `unknown`. Reproduced by hand before fixing.

**A malformed taxonomy in one repository aborted the whole workspace**,
leaving every repository after it in the list stale with nothing on
screen to say so. A new error class the change had introduced: `Index`
used to fail only on collector and storage faults, never on a
user-authored file inside a foreign repository.

**`pkg/memory.IndexResult` never received the `Failures` field** the CLI
gained in Sprint 12, so `engineering-mcp` — the one SDK consumer — still
reported `1 errors` with no file and no cause. The Sprint 12 fix had
landed for the CLI and quietly not for the SDK.

All three fixed pre-merge. This is the daily workflow working exactly as
specified: implement, review, improve, commit.

### Incorrect findings

None. One finding was correct but immaterial here — `git check-ignore`
C-quotes non-ASCII paths without `-z`, so ignored files with such names
are never dropped. It fails open, and no repository in this workspace has
one. Recorded, unfixed.

### Missing knowledge, taxonomy, rules

None identified. Every finding came from the code and the RFC.

### Friction — and the observation that now clears the improvement rule

**Zero MCP calls, again.** The reviewer loaded `review-pull-requests` —
a skill belonging to another project's ecosystem — inside Engineering
OS's own repository, and never called the platform.

Third sighting, third repository:

| | repository | competing skill | outcome |
|---|---|---|---|
| Sprint 11 Run 0 | engineering-mcp | `code-review` | retrieval suppressed; would have helped |
| Sprint 12 Run 5 | pivot | `review-pivot` + 3 others | retrieval suppressed; losing was correct |
| Phase 1 Run 6 | ai-memory | `review-pull-requests` | retrieval suppressed; unclear |

That clears the improvement rule twice over — repeated, and across
multiple repositories — and arguably a third time, since Validation Phase
1's primary goal is that Engineering MCP provides knowledge
*automatically*, which cannot happen if the skill never fires.

The cause is not architectural. It is the descriptions:

```
review-pull-requests: "...review a PR, inspect a diff, check changes
                       before merge, pre-merge or adversarial review,
                       find bugs before bots..."
review-branch:        "Review the current branch using Engineering OS
                       repository knowledge"
```

Theirs enumerates the phrases a developer actually says. Ours states what
it uses, which is a fact about the implementation rather than the
request.

**Not fixed, deliberately, and this is the open question.** In Run 5
losing was the *right* outcome — those domain skills were better, and a
description tuned to win everywhere would have made that review worse.
The narrow fix is to become discoverable for "review my branch before
commit" without trying to outrank purpose-built domain reviewers. Whether
that distinction can be expressed in a description at all is genuinely
unclear, which is why this is logged rather than patched.

### Latency and cost

365s, $2.55. Against Sprint 11's completed runs (240–285s, $1.50–1.56)
and Run 5 (465s, $3.13). The trend is upward and tracks diff size rather
than anything the platform controls.

### The thing worth noticing

This review found a blocking defect in a feature shipped four hours
earlier, whose author had verified it and believed it worked. The
verification was wrong in a way that only shows up on the upgrade path,
and the review found that path without being told to look for it.

It did so while calling Engineering OS zero times.

## Sprint 15 — installing it as a stranger would

Not a review run. A different kind of use, and it found more than the last
three reviews put together.

Installation was performed from scratch in an empty directory with a fake
`$HOME` and only the documentation as a guide. Twelve findings, four of
them blocking. The full record is
[`SPRINT_15_CLEAN_MACHINE.md`](SPRINT_15_CLEAN_MACHINE.md); what belongs
here is what it says about the dogfooding method itself.

**The most severe bug was invisible to every review so far, and obvious
within minutes of following the instructions.** `eng sync` synced the
workspace directory alone, re-registering the root that the install
instructions tell you to detach, and indexing every child repository's
documents a second time under one name. Six dogfooding runs never touched
it, because a developer who already has a working index never runs setup
again. The install documentation was the test case.

**Reviewing the code you wrote and installing the product you built are
different exercises, and the second one had never been done.** Every
finding in this log until now came from pointing the tool at a diff.
Nothing pointed it at the gap between what the repository contains and
what a stranger can do with it, and that gap included a default branch
that does not compile.

**One thing this confirms about Rule #8.** The rule asks which real review
exposed a limitation. Nothing in it says the exposure has to be a review.
`eng sync` was named by an installation, and the requirement recorded as
`KERNEL_REQUIREMENTS.md` #6 was named by writing documentation. Both are
usage; both produced reproducible evidence; neither would ever have been
produced by a review of a diff.

**The measurement to keep.** The claim that the documentation is now true
does not rest on this log. It rests on `QUICKSTART.md` having been
executed verbatim in an empty directory, producing a working install with
no step that is not written down. That is repeatable, and it should be
repeated the next time either repository's setup changes.
