---
doc: RUNBOOK
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Onboarding a repository

[`INSTALL.md`](INSTALL.md) is a per-machine job you do once. This is the
per-repository one, and you do it for every repository you want reviews
to understand.

It takes a few minutes. Most of that is one decision only you can make.

## What "onboarded" means

Engineering OS does not read your code. It reads what your organization
has *written* — rules, decisions, architecture, specifications — and
answers questions about it while a client reviews your code.

So a repository is onboarded when:

1. its documents are in the index, and
2. Engineering OS knows what those documents **are**.

The second half is the one that gets skipped, and skipping it is why a
first attempt so often returns nothing useful. On the first repository
outside this organization, 91% of documents were classified `unknown` —
indexed, searchable, and semantically invisible. A review's blocking
finding rested on a planning document that retrieval could only ever
return as one keyword hit among 662 siblings.

## 1. Attach it

Attach it to the workspace that already holds your **rulebook**. A
workspace containing only your application has nothing to consult, and
answers every question about rules with a confident "none".

```bash
cd /path/to/your/workspace-root
eng workspace attach /path/to/your-repository
eng workspace list
```

**Run this from the workspace root.** `eng workspace attach` has no way to
be told which workspace to attach to — it uses the one in the current
directory, and refuses to create one. Run from your repository instead, it
fails with "run `eng init` first", and following *that* advice creates a
second workspace nested inside your repository, which then shadows the
real one for everything started there.

Attach indexes as it goes. If `list` shows a document count of zero, the
repository has no markdown at its top levels — which is a real answer, and
step 3 is where you do something about it.

## 2. Check what the index thinks it has

```bash
cd /path/to/your-repository
eng doctor
```

The two checks that matter here:

- **Workspace index** — is *this* repository listed? A workspace can be
  perfectly healthy and contain everything except the code you are about
  to review.
- **Engineering knowledge** — given ten real files from this repository,
  how many rules, ADRs and related documents come back? Zero rules is the
  signal that something below is undone.

## 3. Teach the repository to describe itself

This is the step that does the work.

Engineering OS reasons over a small set of canonical types. A document can
declare its own with `doc:` front matter:

```markdown
---
doc: ADR
---
```

Most repositories have no such front matter and never will — writing it
into hundreds of existing files is not a reasonable ask. So instead the
repository declares what its *directories* mean, once, in
`.engineering.yaml` at the repository root:

```yaml
taxonomy:
  plans/**:      Decision
  handbook/**:   Guide
  product/**:    Specification
  research/**:   Reference
  adr/**:        Decision
  docs/api/**:   Reference
```

Canonical types: `Rule`, `Decision`, `Architecture`, `Specification`,
`Guide`, `Planning`, `Reference`, `Other`.

Patterns use the same glob syntax as a rule's `applies_to` — `**` crosses
directory separators, `*` does not — and the **longest matching pattern
wins**, so `plans/backlog/**` can say something different from `plans/**`.

Then re-index so the new classification is applied:

```bash
eng index /path/to/your/workspace-root
```

### Rules for writing one

**Only the repository's own people should write this file.** It is a claim
about what your directories mean, and getting it wrong is worse than
leaving it out — a mislabelled directory returns confidently wrong
documents in a category a reviewer trusts. Never let an outsider, human or
agent, invent mappings for a repository they do not work in.

**Map what you have, not what you wish you had.** If `plans/` holds a mix
of decided and speculative work, `Decision` overclaims. `Planning` is the
honest label, and honest labels are the entire point.

**Front matter always wins.** These lines only classify documents that
would otherwise be unclassified, so adding a taxonomy can never change how
an already-classified document is retrieved. That is what makes the file
safe to add to a repository that is already indexed.

**Commit it.** `.engineering.yaml` is engineering knowledge about the
repository and belongs with it, the same as a linter config. `.eng/` is
generated state and belongs in `.gitignore`.

Four lines of mapping on that first outside repository took `unknown` from
662 documents to 177 and put the decisive planning document at the top of
the decisions section, at 0.80. The full account is in
[`docs/reports/DOGFOOD_LOG.md`](docs/reports/DOGFOOD_LOG.md).

## 4. Write rules, if you have any

A **rule** is a standard that governs files. It is selected by what it
declares it governs, not by keyword — a change almost never mentions the
rule it breaks.

```markdown
---
doc: RULE
id: no-raw-sql
severity: error
applies_to: "internal/billing/**/*.go"
---

# Invoices are written through the store

Billing code never writes raw SQL for invoices. …
```

- `applies_to` is a glob, or a list of them. **A rule with no `applies_to`
  is universal** and comes back for every change — which is correct for
  something like "every error is wrapped with context" and wrong for
  anything language- or area-specific.
- Rules live in the rulebook repository, not scattered through the code.
- You do not need many. Ten rules that are actually enforced beat a
  hundred that are aspirational, because a reviewer that cites a rule
  nobody follows teaches people to ignore citations.

Re-index after adding one:

```bash
eng index /path/to/your/workspace-root
```

Then confirm it can be found:

```bash
cd /path/to/your/workspace-root      # eng search reads the workspace here
eng search no-raw-sql
```

## 5. Confirm end to end

```bash
cd /path/to/your-repository
eng doctor                      # Engineering knowledge should now report rules
eng review                      # checks the setup, then hands over to Claude Code
```

> Review my current branch.

The review ends with a retrieval record: which rules came back, which
actually applied, and which knowledge was retrieved and turned out to be
irrelevant. Read that section. It is how the rulebook earns its place, or
fails to — and "retrieved, irrelevant" repeated across several reviews is
a rule that needs rewriting or deleting.

## Keeping it current

```bash
eng sync /path/to/your/workspace-root      # incremental
eng index /path/to/your/workspace-root     # full, when in doubt
```

Both cover every attached repository. Nothing detects staleness on its
own.
