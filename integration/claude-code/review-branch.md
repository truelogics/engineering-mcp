---
description: Review the current branch using Engineering OS repository knowledge
allowed-tools: Bash(git:*), Read, Glob, Grep, mcp__engineering__find_engineering_rules, mcp__engineering__get_context, mcp__engineering__search_memory, mcp__engineering__verify_evidence
doc: RUNBOOK
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-09
---

Review the current branch of the repository you are standing in.

Engineering OS does not review anything. It answers questions about what
this organization has already decided. You do all of the reasoning.

## 1. Establish what changed

Work it out from git; never ask the developer for it.

- Repository: `git rev-parse --show-toplevel`
- Branch: `git branch --show-current`
- Base: `git merge-base HEAD origin/main`, falling back to `origin/master`,
  then `main`, then `master`
- Changed files: `git diff --name-only <base>...HEAD`, plus
  `git status --porcelain` for uncommitted work
- The change itself: `git diff <base>...HEAD`

If the branch is identical to its base, review the uncommitted working tree
instead. If there is nothing in either, say so and stop — do not review the
whole repository.

Summarize the change in one sentence. That sentence is the `task` argument
for every retrieval below.

## 2. Retrieve engineering knowledge — before forming any opinion

This ordering is the point of the whole system. Retrieve first, so the review
is grounded in what the organization decided, rather than assembled from
general practice and justified afterwards.

- `find_engineering_rules(changed_paths, task)` — the rules that declare they
  govern these files. Rules are selected by scope, not keyword, because a
  change rarely mentions the rule it breaks.
- `get_context(task, changed_paths)` — ADRs, architecture documents and
  related decisions surrounding the change.
- `search_memory(query)` — when the diff raises a specific topic (a
  dependency, a pattern, a subsystem) and you want to know what has been
  written about it.

Snippets are short — 40 to 200 characters, enough to judge relevance and not
enough to quote. When a rule or ADR actually bears on the change, open the
file at the `repository:path` you were given and read it.

Two things to discard as you read the results:

- **Your own diff.** If a changed file is indexed, it comes back as
  "related documents", often ranked highest, because it matches the task
  description better than anything else does. It is the thing under review,
  not knowledge about it.
- **The scores on rules.** Rules are selected by path scope, not by search,
  so they usually score `0.00`. That is not a relevance judgement and does
  not mean the rule is weak — nothing here ranks rules for you. Decide which
  ones bear on the change by reading them.

## 3. Reason

For each finding: what is wrong, where (`file:line`), and why it matters here.

- A finding that rests on a repository rule or decision must cite it as
  `repository:path`.
- Before writing any citation, call
  `verify_evidence(task, document, excerpt, changed_paths)`. Pass the same
  `changed_paths` you gave `find_engineering_rules`: rules are selected by
  path scope, and without the paths a rule the server just handed you is
  absent from the context the verifier checks against.
- A NOT VERIFIED result does not mean the quote is wrong. The verifier
  compares against the passage that was *retrieved*, not the file on disk,
  and only the top-scoring passage per document is kept. A verbatim quote
  from further down a file you have read will fail. So:
  - If you have not read the file, treat NOT VERIFIED as a warning: open the
    file and check the quote before using it.
  - If you have read the file and the quote is verbatim, keep the citation
    and mark it unverified, e.g. *(read from disk; not verifiable through
    the tool — see the known kernel limitation)*.
  Do not silently drop a citation you know to be true. Publishing an
  unchecked quote and discarding a checked one are both failures; say which
  one you are looking at.
- Findings from general engineering practice are welcome, but label them as
  such. Do not dress general advice as organizational policy.
- If no rule governs these files, say so plainly. That is an answer, not a
  gap to fill with invented standards.

Order findings by what would actually block a merge. Do not pad the list.

## 4. Report

```
## Review: <repository> / <branch>

<one-line summary of the change>

### Findings
<ordered by severity; each with file:line, reasoning, and a citation where one exists>

### Retrieval record
- Repository / branch / base:
- Changed files:
- MCP tools called:
- Rules retrieved (and which ones actually applied):
- Evidence verified / failed to verify:
- Repository knowledge that changed this review:
- Repository knowledge that was retrieved but irrelevant:
```

The last two lines matter as much as the findings. They are how we learn
which repository knowledge genuinely improves engineering decisions, and
which merely fills a prompt.
