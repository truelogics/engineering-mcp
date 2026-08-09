---
doc: REPORT
audience: [human, agent]
status: final
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Sprint 11 validation — Claude Code on a real project

Date: 2026-08-09. Reviewer under test: Claude Code (headless, `claude -p`),
connected to `engineering-mcp` over stdio and to nothing else
(`--strict-mcp-config`).

Sprint 11 added no capability. The four tools were already proven in Sprint
8–9; what was missing was the wire between them and a developer doing normal
work. This report records what happened when that wire was connected and
pointed at a real branch.

## Setup

| | |
|---|---|
| Workspace | `/Users/…/truelogics` (one `.eng/memory.db`) |
| Repositories attached | `engineering`, `vision`, `roadmap`, `ai-memory`, `ai-review`, `engineering-mcp` |
| Documents indexed | 97, 0 errors |
| Repository reviewed | `engineering-mcp` |
| Branch | `sprint-11-claude-code`, base `e5491e8`, one commit `d4b9e28` |
| Changed files | `integration/claude-code/{README.md,mcp.json.example,review-branch.md}`, +198 lines, all new |

`review-benchmarks` was deliberately **not** attached. Its fixtures contain
plausible ADRs that were never decided, and a reviewer cannot tell them from
real ones.

`eng workspace create` registers its own directory. At a workspace root that
is a *parent* of the repositories, this indexes all of them under one name
and destroys the `repository:path` qualification that makes citations
checkable. Detaching the root was necessary.

## Three runs

| Run | Prompt | Skills | MCP calls | Tool errors | Turns | Wall | Cost |
|---|---|---|---|---|---|---|---|
| 0 | "Review my current branch." | enabled | **0** | 1 | 3 | 398s (aborted) | $0.36 |
| A | "Review my current branch." | disabled | 11 | 2 | 33 | 271s | $1.39 |
| B | `/review-branch` command body | disabled | 15 | 0 | 39 | 268s | $1.49 |

### Run 0 — a generic review skill suppresses retrieval entirely

The server connected (`mcp_servers: [engineering: connected]`). The model's
first action was to invoke a locally installed `code-review` skill, after
which it read the diff, listed the tree and grepped the source. It called
Engineering OS **zero times** in 398 seconds, and was aborted rather than
completing.

This is the sprint's most operationally important finding, and it is not
about the platform: **an available capability loses to an installed
procedure.** A skill that already knows how to review does not go looking for
organizational knowledge, because nothing in its procedure says to.

Caveat, stated plainly: the run was aborted by the harness, and subagent
tool calls do not appear in the top-level trace, so "zero" is zero *at the
orchestrating level* over the observed window. It is not proof that no
retrieval would ever have occurred.

### Run A — with the competing skill removed, retrieval happens unprompted

Given only the four tools and the bare sentence "Review my current branch",
the model called `find_engineering_rules` with the changed paths, then
`get_context`, then `search_memory`, and — with no instruction to do so —
`verify_evidence` four times before citing.

That is worth stating precisely because it corrects an assumption this
sprint began with. The tool descriptions alone were sufficient to produce
retrieve-then-verify behavior. The command is not what makes retrieval
happen; it is what makes it survive contact with a competing procedure, and
what makes the failure modes legible.

### Run B — the command changes what is retrieved, not whether

Run B called `verify_evidence` ten times against Run A's four, read seven
files against five, and produced zero tool errors. Both arms found the same
top three defects independently.

## What the reviews found

Every finding below was reproduced by hand against the running server
afterwards; none is reported on the reviewer's word alone.

### Confirmed defects

**1. `search_memory` fails on any hyphenated query.** The FTS5 query is
passed unescaped, so a hyphen parses as column syntax:

```
"engineering-mcp kernel policy"  → SQL logic error: no such column: mcp (1)
"kernel policy consumer driven"  → 3 results
```

The organization's own repository name crashes its own search. Reproduced
through MCP and independently through `eng search`, so it is in the kernel's
search path, not the adapter. **Not fixed** — it lives in `ai-memory`, and
Sprint 11's scope excludes kernel changes. Recorded here as a kernel defect.

**2. `verify_evidence` rejected true, verbatim citations.** Both arms hit
this; six of Run B's nine verifications failed on correctly-quoted text. Two
independent causes, separated by a 2×2 run afterwards:

| excerpt | `changed_paths` passed | result |
|---|---|---|
| inside the retrieved snippet | yes | **VERIFIED (high)** |
| inside the retrieved snippet | no | NOT VERIFIED |
| deeper in the same file | yes | NOT VERIFIED |
| deeper in the same file | no | NOT VERIFIED |

- *Cause 1 — this repository's bug.* `verify_evidence` called `ContextFor`
  with empty options while `find_engineering_rules` scopes by path. A rule
  the server had just returned was absent from the context the verifier
  checked against. **Fixed** this sprint: the tool now accepts
  `changed_paths` and forwards them, and says so when they are missing.
  Rows 1 vs 2 show the fix is load-bearing.
- *Cause 2 — a kernel limitation.* Evidence matches against the retrieved
  snippet (40–200 chars, top-scoring chunk only), never the file. Any true
  quote from further down a document fails. Row 3 shows `changed_paths` is
  necessary but not sufficient. **Not fixed**, per the Kernel Rule: recorded,
  not worked around.

The second cause matters more than it looks. The command originally said
"if NOT VERIFIED, read the file and quote it correctly, or drop the claim" —
but re-reading the file cannot change the verdict, so the only reachable
branch was *drop*. A gate that fails closed on true citations trains a
reviewer to stop citing. The command now distinguishes "unchecked" from
"checked but unverifiable" and forbids silently dropping the latter.

**3. `get_context` returns the diff as its own context.** The branch's own
new `README.md` came back as the top-ranked related document at score 0.80,
above every genuine organizational document. `ai-review`'s PromptBuilder
already drops context items whose path is a changed file; the MCP path has
no such filter. Left in the platform deliberately — filtering is a consumer
decision (`KERNEL_POLICY` Rule #7), and duplicating AI Review's logic here is
out of scope. The command now tells the reviewer to discard it.

**4. Scope-selected rules carry no ranking signal.** All three governing
rules returned at score `0.00`, while irrelevant topic matches scored 0.13–0.19.
This is the Sprint 9 ranking gap surfacing through a second consumer, and it
is the standing argument for Engineering Signals. Unchanged this sprint;
recorded as a second independent sighting.

**5. `review-branch.md` had no organizational front matter.** Fixed — but
the fix does not achieve what the rule promises, which is 6b below.

Both arms overstated this one — they reported the file as invisible to the
knowledge system. It is not: a content search returns it at 0.80. The
accurate statement is the one its own rule makes — *retrievable but
unclassified* — so it can never be returned **as** a rule or ADR. The reviews
were right about the defect and wrong about its severity, which is a useful
reminder that a confident review still needs its claims reproduced.

**6. Two divergent install documents**, with different registration
mechanisms, and the new one unreachable from the root README. Fixed.

**6b. Following the front-matter rule does not produce classification.**
Found while fixing 5, and larger than the finding that produced it.

Adding `doc: RUNBOOK` satisfies `doc-front-matter.md` and changes nothing:
the file still indexes as `unknown`. The kernel recognizes six `doc:` values
(`RFC`, `ADR`, `RULE`, the architecture set, the roadmap set, `README`); the
organization writes about twenty-five. Everything else falls through.

Measured across the workspace:

| doc_type | documents |
|---|---|
| **unknown** | **36** |
| readme | 28 |
| rule | 11 |
| rfc | 11 |
| standard | 8 |
| roadmap | 4 |
| adr | 1 |

`unknown` is the largest class in the organization's knowledge base — larger
than `readme`, and thirty-six times `adr`. It contains `KERNEL_POLICY`,
`VISION`, `SYSTEM_MAP`, `PRODUCT_DEVELOPMENT`, `PHILOSOPHY`, `PIPELINE`,
`CONTRIBUTING`, `STYLE_GUIDE`, both `KERNEL_REQUIREMENTS`, and this report.
Six documents carry no `doc:` key at all.

The rule states the consequence itself: an unclassified document "can never
be returned as the ADR or the rule that governs a change." So the documents
that define how this organization works can only ever surface as generic
related-document noise. That is visible in both runs — `get_context`
returned "Architecture decision records: (none)" while putting
`vision:SYSTEM_MAP.md` and `vision:VISION.md` in the discard pile.

Not fixed. It is a kernel taxonomy gap, not a documentation error — the
documents are compliant. Recorded as `KERNEL_REQUIREMENTS.md` #3.

**7. An over-stretched citation in my own README.** It cited
`engineering:rules/no-silent-fallback.md` for stale-index risk. That rule
governs Go and concerns substituting a fake when a dependency is missing;
the analogy borrowed the rule's authority for a case it does not make.
Fixed — the observation now stands on its own.

### Open, not fixed here

- Kernel defects 1, 2 (cause 2) and 6b belong to `ai-memory`. All three are
  recorded in this repository's `KERNEL_REQUIREMENTS.md`, unfixed and not
  worked around.
- `engineering:CAPABILITIES.md` listed every Claude Code row as "Planned"
  and still marked evidence verification blocked for `engineering-mcp`,
  which RFC-0006 had already made false. Corrected this sprint. Claude Code's
  Verify evidence row is marked ⚠️ Partial, not ✅, because of kernel
  defect 2 — a gate that mostly refuses is not a working capability.

## What repository knowledge genuinely improved the review

This is the question the sprint exists to answer.

**It earned its place.** In both arms the findings that came from the
rulebook were findings the diff did not contain. `doc-front-matter.md`
supplied not just "add front matter" but the *reason it matters here* — that
an unclassified document cannot be returned as a governing rule — which
turns a cosmetic nit into a functional defect. `get_context` surfaced
`docs/CLAUDE_CODE.md`, producing the duplicate-documentation finding;
nothing in the diff mentions that file. Run B retracted a drafted finding
after reading `ai-memory/internal/cli/cli.go` — retrieval prevented a false
positive as well as producing true ones.

**Path-scoped rule selection is the load-bearing capability.** Every
rule-derived finding came from `find_engineering_rules`, selected by
`applies_to` scope. Keyword search would not have found them: the change
says nothing about front matter or capability contracts. RFC-0005's premise —
*a violation rarely quotes the rule it violates* — held in a real review.

**Roughly half of what came back was noise.** Both arms listed the same
irrelevant retrievals: `vision:SYSTEM_MAP.md`, `vision:VISION.md`,
`ai-review:docs/PIPELINE.md`, `ai-memory:rfcs/0001`, `ai-review:rfcs/0001` —
all matched on "MCP" and "Claude Code" as topic words. Plus the diff itself.
A reviewer has to know to discard these, and nothing in the output helps.

**The strongest signal is negative.** `no-silent-fallback.md` was *not*
returned for these paths (`applies_to: go`), and that absence is what exposed
finding 7 — an over-citation I had written by hand. Scoping produced a true
finding by staying silent.

## Contamination

I authored the branch under review, the rulebook it is judged against, and
this report. Both arms ran with no human evaluator. Two of the seven findings
concern documents I wrote in the same session.

This is internal validation, not external evaluation. The reproductions above
are what stand in for an independent evaluator, and they are weaker: they
confirm that a claimed defect is real, not that the review was complete or
that its severity ordering is right. External evaluation remains future work.

## Definition of Done

| Criterion | Status |
|---|---|
| Claude Code connects to Engineering MCP | Yes — `engineering: connected`, three runs |
| Claude Code automatically retrieves Engineering OS knowledge | Yes, when no competing review skill is installed (Run A, unprompted). No, when one is (Run 0) |
| Claude Code reviews a real project using repository knowledge | Yes — 8 defects, 4 traceable to retrieved rules and documents |
| The workflow is usable during normal development | Partly. Usable, with three recorded kernel defects and ~50% retrieval noise |
| We identify what knowledge improves engineering decisions | Yes — path-scoped rules, above |

The honest summary: the integration works and immediately paid for itself by
finding real bugs in the branch that introduced it, including two in the
evidence path that would have quietly degraded every future review. It is not
yet frictionless — hyphens crash search, half of retrieval is noise, the
verification gate is only half fixed, and a third of the knowledge base is
unclassifiable.

Worth naming plainly, because it is the sprint's actual argument: every one
of those four problems was found by pointing the system at itself for one
afternoon. None of them was visible in nine sprints of design documents,
benchmarks and unit tests. The fastest way to learn what a knowledge system
gets wrong is to make it answer a real question about real work.

## What to watch during the observation week

Per the standing instruction to use this daily before building anything else:

1. Does the retrieval-first ordering survive when the developer is in a
   hurry, or does the generic path win as it did in Run 0?
2. Does the noise fraction fall on changes that touch code rather than docs?
3. How often is a citation dropped because of kernel defect 2? Each instance
   is a true finding weakened.
4. Which rules are retrieved often and never applied? Those are candidates
   for narrower `applies_to` scope, not for deletion.
