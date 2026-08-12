---
doc: KERNEL_REQUIREMENTS
audience: [human, agent]
status: draft
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Kernel Requirements

The kernel is consumer-driven (`engineering:KERNEL_POLICY.md` Rule #1), so
each consumer keeps its own list. This is the second consumer's; Engineering Review's
is at `engineering-review/KERNEL_REQUIREMENTS.md`.

Everything here was reproduced against the running kernel before it was
written down. A requirement is not recorded until the gap has been
reproduced — a rule this organization learned by recording a blocker twice,
in opposite directions, from documentation rather than from behavior.

Nothing below was worked around in this repository. Per the Kernel
Rule, a consumer that discovers a kernel limitation records it and stops.

## 1. ~~`Search` passes its query to FTS5 unescaped, so a hyphen is a crash~~ — RESOLVED (Sprint 11)

Fixed in `engineering-kernel` by `search.UserQuery`, applied at the two points raw
human text enters (`pkg/memory.Search`, `eng search`). Kept below because
the shape of the miss is the useful part: the kernel had solved this once
on the retriever path and the neighbouring path never got the fix.

Found in Sprint 11's first real review, twice, on ordinary input.

```
memory.Search("engineering-mcp kernel policy")
  → sqlite: SQL logic error: no such column: mcp (1)
memory.Search("kernel policy consumer driven")
  → 3 results
```

FTS5 reads `a-b` as column syntax. The organization's own repository names
are hyphenated, so `search_memory` fails on the vocabulary it exists to
search. Reproduced through MCP and independently through `eng search`, so it
is not the adapter.

**The kernel has already solved this once, on the other path.**
`internal/retriever/keywords()` quotes every term as an FTS5 phrase before
joining, with a comment explaining exactly this class of failure — which is
why `get_context` and `find_engineering_rules` handle hyphens correctly.
`internal/search.Search()` (`search.go:87`) passes `query` straight to
`Storage.SearchChunks` → `chunks_fts MATCH ?` with no such treatment.

**What's needed:** the same escaping on the search path. This is not a new
capability and needs no RFC — it is one path missing a fix the neighbouring
path already carries.

## 2. Evidence can only verify the top chunk's highlight — second sighting

This is `engineering-review/KERNEL_REQUIREMENTS.md` #15 (`ContextPackage`'s `Snippet`
is a truncated FTS5 highlight, not source content) reaching a second,
independent consumer. Recording the new detail rather than restating it.

`ContextPackage.VerifyEvidence` matches an excerpt against the retrieved
snippet, and only the top-scoring chunk per document is retained. A quote
that is verbatim, and from a document the kernel itself just returned, fails
whenever it falls outside that one highlight. Isolated by a 2×2 against the
running server:

| excerpt | scoped to changed paths | result |
|---|---|---|
| inside the retrieved snippet | yes | VERIFIED (high) |
| inside the retrieved snippet | no | NOT VERIFIED |
| deeper in the same file | yes | NOT VERIFIED |
| deeper in the same file | no | NOT VERIFIED |

Row 2 was this repository's own bug and is fixed here — `verify_evidence` now
forwards `changed_paths`, so a scope-selected rule is present in the context
being checked. Row 3 is the kernel limitation, and it is the one that bites:
a reviewer who opens the file and quotes it correctly — exactly what good
practice demands — gets NOT VERIFIED.

The consequence is not a missing feature but an inverted incentive. A
verification gate that fails closed on true citations teaches a consumer to
stop citing, which is the opposite of what the Evidence capability was
promoted into the kernel to achieve (`engineering-kernel/rfcs/0006`).

**What's needed:** the ability to verify an excerpt against a document's
source content, not against one retrieved highlight. `engineering-review`'s #14 and
#15 describe the same underlying shortfall from the review side.

**Not needed:** a workaround here. This repository now tells its client the
difference between "unchecked" and "checked but unverifiable" and forbids
silently dropping the latter, which is a disclosure, not a substitute.

## 3. ~~The `doc:` taxonomy is closed, so most organizational documents are unclassifiable~~ — RESOLVED (Sprint 13, engineering-kernel RFC-0007)

A repository now states what its own directories hold, in a
`.engineering.yaml` that lives in the repository and maps path globs onto
a small closed set of canonical types. The vocabulary the platform
reasons over stays ours and stays small; the names a company gives its
directories stay theirs and stay open.

Measured on the project below, after four lines of mapping:

| | before | after |
|---|---|---|
| unknown | 662 (91%) | 177 (25%) |
| decisions (`adr`) | 0 | 153 |
| guides | 0 | 66 |
| planning | 0 | 260 |

`plans/to-do/block-fractional-indexing-vorder-v2.md` — the document whose
absence from retrieval is the whole reason this requirement existed — now
classifies as a decision and comes back from `get_context` under
Architecture decision records at 0.80, with the exact passage the reviewer
used ranked second.

This organization's own 101 documents classify identically, document for
document, because a mapping only ever fills in `unknown` and never
overrules a document's own front matter.

The original text is kept below: the shape of the miss is the useful
part, and the numbers are the evidence the RFC was written against.

### The original requirement

`engineering:rules/doc-front-matter.md` requires every markdown document to
declare what it is, and states the consequence of not doing so: an
unclassified document "can never be returned as the ADR or the rule that
governs a change."

A document can follow that rule exactly and still be unclassified.
`inferDocType` recognizes six families — `RFC`, `ADR`, `RULE`, the
architecture set, the roadmap set, `README` — and the organization writes
roughly twenty-five distinct `doc:` values. Everything else becomes
`unknown`.

Measured across the Sprint 11 workspace (99 documents, six repositories):

| doc_type | documents |
|---|---|
| **unknown** | **36** |
| readme | 28 |
| rule | 11 |
| rfc | 11 |
| standard | 8 |
| roadmap | 4 |
| adr | 1 |

`unknown` is the largest class, and it holds the documents that define how
the organization works: `KERNEL_POLICY`, `VISION`, `SYSTEM_MAP`,
`PRODUCT_DEVELOPMENT`, `PHILOSOPHY`, `PIPELINE`, `STYLE_GUIDE`,
`CONTRIBUTING`, and both consumers' `KERNEL_REQUIREMENTS`. Only six of the
36 are missing front matter; the other thirty are compliant and unrecognized.

The visible effect during review is that `get_context` reports "Architecture
decision records: (none)" while the architecture documents sit in the
related-documents list, indistinguishable from incidental keyword matches.

**What's needed:** a decision, not necessarily code — either the recognized
set grows to match what the organization writes, or `doc:` accepts an
extension mechanism, or the organization narrows its vocabulary to the six.
The current state is the worst of the three, because it looks like
compliance and produces none.

**Not needed:** consumers guessing document classes from paths or titles.
That would put the taxonomy in two places and make the kernel's answer the
less authoritative one.

### Second sighting, on an outside project (Sprint 12)

The first real project Engineering OS was pointed at is a 12,500-file
polyglot monorepo with substantial written knowledge: 416 planning
documents, a 72-document engineering handbook, 124 test documents.

Indexed, it classifies as:

| doc_type | documents |
|---|---|
| **unknown** | **662** |
| readme | 64 |
| rule / adr / rfc / standard / architecture | **0** |

91% unclassified, against 36% inside this organization. The project uses
no `doc:` front matter at all, which is the normal state of a repository
that has not adopted this convention — that is, every repository, on the
day it adopts Engineering OS.

The consequence is not degraded review, it is no grounding whatsoever:
`find_engineering_rules` can return nothing, `get_context` reports no
ADRs, and the 72-document handbook — which is where that project's
engineering standards actually live — is reachable only as keyword-matched
"related documents", indistinguishable from an incidental hit.

This moves the requirement from an organization's own inconsistency to
the adoption path for every new project.

## 4. Collecting a real project ingests what the project has disowned — FIXED (Sprint 12)

Recorded because the shape recurs, and because the fix is a precedent for
where this kind of knowledge belongs.

`filesystem.Collector` skipped a fixed set of directory names — `.git`,
`node_modules`, `vendor`. On this organization's repositories that is
indistinguishable from correct. On the first outside project:

| | markdown files |
|---|---|
| tracked by git | 726 |
| offered to the collector | **22,445** |
| of those, git-ignored | 21,719 (96.8%) |
| of those, tool caches under `.claude/` | 21,690 |

Indexing had not finished after ten minutes and the database had passed
233 MB, all of it agent plugin caches. After the fix: 727 scanned, 19
seconds, 24 MB.

A fixed list of names cannot express what a project considers its own,
and extending it is whack-a-mole — `.claude` today, the next tool's
directory tomorrow. Every repository already publishes the answer in
`.gitignore`. The collector now asks git, in one batched `check-ignore`
call, and keeps every path when git cannot answer: narrowing scope is an
optimization, and silently indexing *less* than asked would be the worse
failure.

## 5. An index error count names neither the file nor the cause — FIXED (Sprint 12)

`IndexResult` carried `Errors int` and discarded every error value. A
developer saw `727 scanned, 726 added, 1 errors` and had nowhere to
start — no file, no stage, no reason.

Recorded rather than merely fixed because it had already cost this
organization once and was not recognized: in Sprint 7 four engineering
rules were silently unindexed by invalid `applies_to` front matter, and
the only signal was the same bare count. Two occurrences, the same
missing information, months apart.

`IndexResult.Failures` now carries a path and a reason per failure, and
the CLI prints them. The first run after the change immediately named a
real defect in the outside project — a plan file whose front-matter block
had been mangled by a formatter into `## title:` and never closed — which
had been silently absent from that project's index.

## 6. Two canonical types cannot be declared in front matter — first sighting

RFC-0007 established eight canonical types, and `.engineering.yaml` can
map a directory onto any of them. A *document* cannot: the parser's
front-matter switch maps `RFC`, `ADR`, `RULE`, `ARCHITECTURE` (and its
aliases), `ROADMAP` and `README`, and nothing else. `doc: GUIDE` and
`doc: SPECIFICATION` fall through to `unknown`, although
`DocTypeGuide` and `DocTypeSpecification` both exist and both are
reachable through a taxonomy file.

Reproduced in this repository while writing Sprint 15's install
documentation. `INSTALL.md`, `QUICKSTART.md` and `ONBOARDING.md` all
carry front matter, all describe themselves accurately, and all indexed
as `unknown` — so the four documents whose entire purpose is to be found
by someone who does not know where to look were the ones retrieval could
not name.

The workaround here is the taxonomy file, which is the intended tool and
does work: `.engineering.yaml` maps them to `Guide` and they now return
under a Guides heading. It is a workaround nonetheless, because a
document that states its own type should not need a second file to
restate it, and because the precedence rule — front matter wins over
taxonomy — cannot apply to a value front matter is unable to express.

Not worked around in this repository, and deliberately not fixed here:
adding two cases to the kernel's switch is a kernel change, and Sprint 15
is a developer-experience sprint. Recorded, with the review that exposed
it named, per `engineering:KERNEL_POLICY.md` Rule #8.
