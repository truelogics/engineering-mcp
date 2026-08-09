---
doc: REPORT
audience: [human, agent]
status: living
owner: engineering-mcp
last_reviewed: 2026-08-10
---

# Tool Discovery Experiment

**Question: why doesn't Claude Code call Engineering MCP?**

Not "how do we make it". The observation earned investigation under
`KERNEL_POLICY.md` Rule #8 — three sightings, three repositories — and
the symptom is known while the cause is not.

| | repository | competing skill | MCP calls |
|---|---|---|---|
| Sprint 11 Run 0 | engineering-mcp | `code-review` | 0 |
| Sprint 12 Run 5 | pivot | `review-pivot` + 3 | 0 |
| Phase 1 Run 6 | ai-memory | `review-pull-requests` | 0 |

## Candidate causes

1. Tool descriptions.
2. Claude Code's tool-selection heuristics.
3. Prompt wording.
4. MCP registration.
5. The model deciding it already has enough information.
6. The model preferring filesystem tools.
7. **A competing skill capturing the task before tool selection happens.**

The seventh is not in the original list and is the one the existing
evidence points hardest at. In all three sightings the transcript opens
the same way: the model's *first* action is `Skill(...)`, and everything
after follows that skill's procedure. Tool choice never occurs as a free
decision, because a procedure was already in hand.

## Why the A/B/C description experiment is not the right first move

The proposed design varies descriptions — current, developer-language,
minimal — and measures which tools get called. It tests cause 1 while
holding cause 7 uncontrolled, and cause 7 is upstream of it: if a skill
captures the task, no description is ever consulted, and all three arms
return zero. Three zeros would look like "descriptions don't matter" when
the truth would be "descriptions were never read".

There is already a data point that descriptions are not the binding
constraint. **Sprint 11 Arm A**: same descriptions as today, same bare
prompt, skills disabled — the model called Engineering MCP **11 times
unprompted**, including `verify_evidence` four times before citing
anything. Nobody told it to.

That is one observation, on a different repository and an older branch,
so it indicates rather than settles.

## Stage 1 — isolate skill capture (running)

One variable. Same repository, same branch, same commit, same prompt,
same descriptions. The only difference is whether `Skill` is available.

| Arm | Skills | Descriptions | Prediction |
|---|---|---|---|
| 1 | available | current | 0 MCP calls, `Skill` first |
| 2 | disabled | current | MCP called; `find_engineering_rules` early |

Target: `ai-memory` @ `sprint-11-fts-hyphen-fix`, prompt *"Review my
current branch."*

**If arm 2 calls the tools:** cause 7 is confirmed and causes 1, 3, 4 and
6 are largely exonerated — discovery works fine when nothing outranks it.
The problem is not that the tools are undiscoverable, it is that they are
never reached. Rewriting descriptions would then be solving the wrong
problem, exactly as warned.

**If arm 2 also returns zero:** skill capture is not the cause, and the
A/B/C description experiment becomes the correct next step, run in this
same skills-disabled condition so the variable is actually isolated.

Either way Stage 1 is one run's worth of information that changes what
Stage 2 should be.

## Stage 1 result — both hypotheses were wrong

Same repository, same branch, same commit, same prompt, same
descriptions. Only `Skill` availability differed.

| Arm | Skills | `ToolSearch` | MCP calls | Tool calls | Turns | Wall | Cost | First action |
|---|---|---|---|---|---|---|---|---|
| 1 | available | **1** | **3** | 40 | 42 | 475s | $2.72 | `Skill(review-branch)` |
| 2 | disabled | **0** | **0** | 42 | 43 | 483s | $2.87 | `Bash` |

Arm 2 — the condition I predicted would *restore* tool use — called
Engineering MCP zero times in 42 tool calls. Arm 1, with the competing
skills I blamed, called it three times. Both arms did comparable work for
comparable cost; only one of them consulted the organization's knowledge.

The prediction was backwards, and so was the diagnosis.

### The actual mechanism

**The MCP tools are not in the model's prompt.** This environment defers
them:

```json
"total_deferred_tools": 34
```

A deferred tool's schema is absent until it is fetched by name with
`ToolSearch`. Until then the tool cannot be called at all — not
disfavoured, not outranked, *uninvokable*, no matter what its description
says.

That single fact explains every observation:

- **Arm 1** loaded `review-branch`, whose body names the four tools
  explicitly. That triggered
  `ToolSearch(select:mcp__engineering__find_engineering_rules,…)`, the
  schemas loaded, and the tools became callable. They were then called.
- **Arm 2** never issued a `ToolSearch`. The four tools were listed in
  `--allowedTools` and were still unreachable, because nothing named
  them. Zero MCP calls was not a preference; it was the only possible
  outcome.
- **Runs 0, 5 and 6** each loaded a *different* project's review skill —
  `code-review`, `review-pivot`, `review-pull-requests` — none of which
  mentions Engineering MCP. No mention, no `ToolSearch`, no schemas, no
  calls.
- **Sprint 11 Arm A**, the one run where an unprompted model called MCP
  11 times, used `--strict-mcp-config` with the engineering server as the
  *only* MCP server. A small enough tool list that nothing was deferred.

### What this exonerates, and what it indicts

Descriptions (cause 1), prompt wording (3), registration (4) and a
preference for filesystem tools (6) are all exonerated for the observed
failures. A description cannot lose a competition it never entered.

Skill capture (cause 7, my hypothesis) is exonerated too, and is closer
to the opposite of true: in Arm 1 the skill is the *only reason*
discovery happened. `review-branch` works precisely because it names the
tools, which is the behaviour that loads them.

The cause is **tool-list deferral** (cause 2): with 34 deferred tools,
Engineering MCP's four are invisible until something names them, and the
only thing that ever names them is our own skill.

### Consequences

1. **Stage 2 as designed would have measured nothing.** Three
   description variants, all deferred, all unnamed, all zero. Three
   zeros reading as "descriptions don't matter" when descriptions were
   never read. This is exactly the wrong problem the instruction warned
   against solving — and I had proposed a different wrong problem.
2. **`review-branch` is load-bearing infrastructure, not convenience.**
   It is currently the only path by which these tools become callable at
   all. That reframes the Sprint 11 note that "the command isn't what
   makes retrieval happen" — in this environment, it is exactly that.
3. **Environment shapes the result more than the product does.** The
   same server, descriptions and prompt produce 11 calls or 0 depending
   on how many other tools are installed. Any future measurement must
   record the deferral state, or it is not reproducible.

### Not yet known

Whether deferral is triggered by tool count, server count, or context
size; what the threshold is; and whether Engineering MCP's four tools
would stay resident on a machine with fewer MCP servers installed.
That is the next thing worth measuring, and it is a question about the
client, not about this repository.

## Stage 2 — descriptions, if still warranted

Only inside whichever condition Stage 1 shows lets a decision happen.
Variants: current; developer-language ("Review repository changes using
engineering knowledge"); minimal capability statement, no persuasion.
Measured on which tools, in what order, and whether review quality moves.

Not run yet. Running it before Stage 1 reports would be measuring a
variable that may never be consulted.

## Constraints

No tool is renamed, added, or merged until the cause is understood.

## A caution about what "success" means here

The instruction is deliberately *why*, not *how*, and there is a reason
to keep it that way. In Sprint 12 the model ignored Engineering MCP on
Pivot and produced a better review than Engineering MCP could have
supported — that project's own domain skills were genuinely superior, and
the platform had no rules, no ADRs and nothing to offer. **Losing was the
correct outcome.**

So a fix that makes Engineering MCP win everywhere would make that review
worse. The target is not "called more often". It is "reached when it has
something to say" — and if Stage 1 confirms skill capture, the honest
conclusion may be that the two should compose rather than compete.

## Stage 1 confirmed in ordinary use (Sprint 15)

Not an experiment. Two reviews of Sprint 15's own branch, run because the
sprint requires a review, on the same machine and the same commit, minutes
apart. The only difference was how the request was phrased.

| Asked as | Skill that ran | `ToolSearch` | MCP calls | Tool calls | Turns | Wall | Cost |
|---|---|---|---|---|---|---|---|
| "Review my current branch." | `review-pull-requests` | 0 | **0** | 37 | 39 | 461s | $2.92 |
| `/review-branch` | `review-branch` | 1 | **9** | 59 | 61 | 628s | $4.29 |

The mechanism is exactly the one Stage 1 identified. `review-pull-requests`
is another project's skill; it does not mention Engineering MCP; nothing
issued a `ToolSearch`; the four tools stayed deferred and uninvokable for
the whole run. `review-branch` names them, so they loaded and were used —
`find_engineering_rules`, `get_context`, and `verify_evidence` six times.

Three things this adds to Stage 1.

**Installing the command is necessary and not sufficient.** The command
*was* installed. `doctor` confirms it. It still lost the match, because
skill selection happens on the phrasing and the phrasing was generic. The
documentation now says to invoke `/review-branch` by name rather than to
describe the task, and that is a change Stage 1 alone would not have
produced.

**The difference is visible in the output, not just the telemetry.** The
run that reached the platform cited `engineering:rules/go-wrap-errors.md`,
`no-silent-fallback.md` and `pr-single-purpose.md` by name and verified
each quote before using it. The run that did not was a competent review
that could not have named a single organizational rule, because it never
had access to one. Both found real defects. Only one could say which
standard a defect violated.

**The Sprint 15 Definition of Done is not met as literally worded.** It
specifies `claude` → *"Review my current branch."* → Engineering MCP,
with "no manual intervention". On a machine with one review skill that
holds. On this one it does not, and the honest report is that the
workflow requires typing a command rather than a sentence.
