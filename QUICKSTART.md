---
doc: RUNBOOK
audience: [human]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-12
---

# Quickstart

Three commands, from nothing to a review that cites your organization's
own engineering decisions.

You need [Go 1.25 or newer](https://go.dev/dl/), git, and
[Claude Code](https://claude.com/claude-code).

## 1. Install `eng`

```bash
go install github.com/truelogics/engineering-kernel/cmd/eng@latest
export PATH="$(go env GOPATH)/bin:$PATH"        # add to your shell profile
```

Nothing to clone. `eng` is the shell of the Engineering OS; it installs
the rest.

## 2. Set it up

```bash
eng setup ~/engineering-os \
  --rules git@github.com:truelogics/engineering.git \
  --repo  ~/code/your-application
```

That one command creates the workspace, clones and indexes what you
named, installs `engineering-mcp`, registers it with Claude Code, and
installs the `/review-branch` command.

`--rules` is whichever repository holds your organization's rules, ADRs
and standards. Without it the tools work perfectly and have nothing to
say. `--rules` and `--repo` take a local path or a git URL and may be
repeated.

## 3. Use it

```bash
cd ~/code/your-application
eng doctor        # eight checks, ordered the way the system is layered
claude
```

> /review-branch

Claude Code works out the branch, base and changed files from git, asks
Engineering OS which rules govern those files and what has been decided
around them, and then reviews.

**Type the command, not a sentence.** Asking *"Review my current
branch."* in plain language works only if nothing else claims it.
Measured on this repository, on the same commit, minutes apart:

| Asked as | Skill that ran | Engineering MCP calls |
|---|---|---|
| `/review-branch` | `review-branch` | **9** of 59 tool calls |
| "Review my current branch." | `review-pull-requests` | **0** of 37 |

Both produced a review. Only one consulted the organization's knowledge.

## Keeping it current

The index is a snapshot, and nothing detects staleness on its own. After
your engineering documents change:

```bash
eng update ~/engineering-os
```

## When something is wrong

`eng doctor` first, from inside the repository you are working in. It
checks the binaries, the workspace, the index, retrieval, the Claude Code
registration, the protocol handshake and the `/review-branch` command,
and names the first thing that is broken. Fix the first `✘`; the ones
below it are usually its symptoms.

`eng setup` is safe to re-run — that is how you add a repository, or
repoint Claude Code after rebuilding.

## Next

- [`INSTALL.md`](INSTALL.md) — the same install with what each step
  produces, what goes wrong, and how to do it by hand.
- [`ONBOARDING.md`](ONBOARDING.md) — nothing is retrieved from your
  application's own documents until you tell it what its directories
  mean.
- [`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md) — the Claude Code side in
  detail, including what to do when it goes quiet.
