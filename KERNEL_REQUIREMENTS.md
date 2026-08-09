---
doc: KERNEL_REQUIREMENTS
audience: [human, agent]
status: draft
owner: engineering-mcp
last_reviewed: 2026-08-09
---

# Kernel Requirements

The kernel is consumer-driven (`engineering:KERNEL_POLICY.md` Rule #1), so
each consumer keeps its own list. This is the second consumer's; AI Review's
is at `ai-review/KERNEL_REQUIREMENTS.md`.

Everything here was reproduced against the running kernel before it was
written down. A requirement is not recorded until the gap has been
reproduced — a rule this organization learned by recording a blocker twice,
in opposite directions, from documentation rather than from behavior.

Neither item below was worked around in this repository. Per the Kernel
Rule, a consumer that discovers a kernel limitation records it and stops.

## 1. ~~`Search` passes its query to FTS5 unescaped, so a hyphen is a crash~~ — RESOLVED (Sprint 11)

Fixed in `ai-memory` by `search.UserQuery`, applied at the two points raw
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

This is `ai-review/KERNEL_REQUIREMENTS.md` #15 (`ContextPackage`'s `Snippet`
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
promoted into the kernel to achieve (`ai-memory/rfcs/0006`).

**What's needed:** the ability to verify an excerpt against a document's
source content, not against one retrieved highlight. `ai-review`'s #14 and
#15 describe the same underlying shortfall from the review side.

**Not needed:** a workaround here. This repository now tells its client the
difference between "unchecked" and "checked but unverifiable" and forbids
silently dropping the latter, which is a disclosure, not a substitute.

## 3. The `doc:` taxonomy is closed, so most organizational documents are unclassifiable

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
