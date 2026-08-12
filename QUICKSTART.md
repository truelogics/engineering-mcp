---
doc: RUNBOOK
audience: [human]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Quickstart

Ten minutes, from nothing to a review that cites your organization's own
engineering decisions.

You need [Go 1.25 or newer](https://go.dev/dl/), git, and
[Claude Code](https://claude.com/claude-code). If something below fails,
[`INSTALL.md`](INSTALL.md) explains each step and what it produces.

## 1. Clone

```bash
mkdir -p ~/engineering-os && cd ~/engineering-os

git clone git@github.com:truelogics/engineering-kernel.git
git clone git@github.com:truelogics/engineering-mcp.git
git clone git@github.com:truelogics/engineering.git     # your rulebook
```

The third one is whichever repository holds your organization's rules,
ADRs and standards. Without it the tools work perfectly and have nothing
to say.

## 2. Build

```bash
mkdir -p ~/.local/bin
(cd engineering-kernel       && go build -o ~/.local/bin/eng ./cmd/eng)
(cd engineering-mcp && go build -o ~/.local/bin/engineering-mcp ./cmd/engineering-mcp)
export PATH="$HOME/.local/bin:$PATH"        # add to your shell profile
```

## 3. Index

```bash
cd ~/engineering-os
eng workspace create .
eng workspace detach .                      # the root is a container, not a repository
eng workspace attach ./engineering          # the rulebook
eng workspace attach /path/to/your-application
```

`attach` indexes as it goes; there is no separate index step on first
setup.

## 4. Register with Claude Code

Once, for every project you will ever open:

```bash
claude mcp add engineering --scope user \
  -e ENGINEERING_WORKSPACE="$HOME/engineering-os" \
  -- "$HOME/.local/bin/engineering-mcp"
```

## 5. Install the review command

```bash
mkdir -p ~/.claude/commands
cp ~/engineering-os/engineering-mcp/integration/claude-code/review-branch.md ~/.claude/commands/
```

Not optional. On a machine with several MCP servers installed, tool
schemas are deferred and a tool is unreachable until something names it —
this command is what names these four. See
[`docs/reports/TOOL_DISCOVERY_EXPERIMENT.md`](docs/reports/TOOL_DISCOVERY_EXPERIMENT.md).

## 6. Check it

```bash
eng doctor
```

Eight checks, in the order the system is layered. Fix the first `✘`; the
ones below it are usually its symptoms.

`eng` is the entry point to all of Engineering OS (RFC-0008); `eng doctor`
runs `engineering-mcp doctor` for you, so you never have to know which
repository answers which question.

## 7. Use it

```bash
cd /path/to/your-application
claude
```

> /review-branch

Claude Code works out the branch, base and changed files from git, asks
Engineering OS which rules govern those files and what has been decided
around them, and then reviews.

**Type the command, not a sentence.** Asking *"Review my current branch."*
in plain language works only if nothing else claims it. Measured on this
repository, on the same commit, minutes apart:

| Asked as | Skill that ran | Engineering MCP calls |
|---|---|---|
| `/review-branch` | `review-branch` | **9** of 59 tool calls |
| "Review my current branch." | `review-pull-requests` | **0** of 37 |

Both produced a review. Only one consulted the organization's knowledge.

## Next

- Nothing is retrieved from your application's own documents until you
  attach it and, usually, tell it what its directories mean —
  [`ONBOARDING.md`](ONBOARDING.md).
- The Claude Code side in detail, including what to do when it goes
  quiet — [`docs/CLAUDE_CODE.md`](docs/CLAUDE_CODE.md).
