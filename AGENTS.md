# Agent working agreements

## Every change goes through a pull request

**Hard rule. No exception for size** — documentation-only and one-line
changes included. One fix or one feature, one branch, one PR.

Never commit to `main` directly, and never merge a branch into `main`
locally. `main` moves only when a PR is merged.

Opening the PR is where the work ends. Merging is the owner's decision,
not the author's — including when the author is an agent, and including
when the owner has said "push" or "ship it". Those mean *get it ready to
merge*.

A merge that skipped review is indistinguishable, afterwards, from one
that passed it. The PR is the only place a change is visible as a
proposal rather than as a fact.

## Before pushing

```bash
go build ./... && go vet ./... && go test ./...
```

Green, not "green apart from". Report failures with their output; say
out loud when a step was skipped.

## Coupled repositories

`engineering-kernel`, `engineering-mcp` and `engineering-review` change
together often. Open a PR in each, say in both bodies that neither half
works alone, and merge them together.

Do not add to a CLI, a transport or a consumer what belongs in the
kernel — `engineering:KERNEL_POLICY.md`, Rule #6.

## The full agreements

Versioned and indexed in the [`engineering`](https://github.com/truelogics/engineering)
rulebook, which is the source of truth for this file:

- `rules/every-change-through-a-pr.md`
- `rules/pr-single-purpose.md` — what belongs in one PR
- `rules/review-exposed-the-need.md` — what a new capability must cite
- `KERNEL_POLICY.md`
