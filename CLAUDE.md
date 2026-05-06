# Review Loop: Plan → Execute → Adversarially Review → Iterate

A disciplined workflow for complex, multi-step tasks. Delegates work to
subagents, maintains durable ledgers, and loops until an adversarial reviewer
is satisfied. Use this when the task is substantial enough that slipping on
planning, defect tracking, or review would cost more than the overhead of
running the loop.

## Non-negotiable rules

- **Never do planning, execution, or review yourself.** Every phase runs in a
  subagent. Your job is orchestration, ledger maintenance, and decision-making
  on the loop exit condition.
- **Run independent subagents in parallel** whenever the work partitions
  cleanly (e.g. multiple independent fixes, multiple independent defects).
  Parallel subagents share no memory — each prompt must be self-contained.
- **A clean review is not a stop condition.** It means the current PR/task is
  done — commit it and pick up the next planned task from `./tasks.md`.
  Control returns to the user only when the ledger has no more planned or
  in-progress work, or when the loop is genuinely blocked on user input.
  Returning after one milestone "because it went well" is the primary failure
  mode of this skill.
- **Ledgers are durable.** `./tasks.md` and `./defects.md` persist between
  iterations and across sessions. Append; do not rewrite history.

## Ledgers

### `./tasks.md` — planned and completed work

The authoritative ledger. Structured, not a flat checklist. Four status
markers, three standing sections, and a rich **Completed** section that
doubles as the project's post-mortem log.

**Status legend (always include verbatim near the top):**

```
Status: `[ ]` planned · `[~]` in progress · `[x]` done · `[!]` blocked
```

**Required sections, in order:**

1. **Milestones (high-level)** — one line per milestone. Stays terse; the
   breakdown lives below.
2. **<Current milestone> — breakdown** — one line per PR/task. Detail for
   each PR lives in a dedicated plan doc under `./docs/drafts/` (e.g.
   `./docs/drafts/YYYYMMDD-HHMM-<name>.md`); the ledger points at it, does
   not duplicate it. Include any follow-up breakdown sections for later
   milestones as they start.
3. **Cross-cutting architectural notes (locked)** — decisions that span
   multiple PRs: library choices, schema invariants, testing conventions,
   etc. Each entry is a checkbox so that "decide X" items visibly resolve
   to "decided: X, land in PR-N" items.
4. **Completed** — one rich entry per finished PR/task. Not a one-liner.
   Each entry captures: what shipped, date, what was discovered/surprising,
   workarounds applied, verification commands run and their results, and
   constraints or caveats that future work must respect. This is the part
   future subagents (and future-you) will actually read — invest in it.

**Skeleton:**

```markdown
# <Project> — Task Ledger

Authoritative ledger of planned and completed work. <Pointer to the spec
or project doc that governs scope.>

Status: `[ ]` planned · `[~]` in progress · `[x]` done · `[!]` blocked

---

## Milestones (high-level)

- [~] **M1** — <one-line goal>.
- [ ] **M2** — <one-line goal>.

---

## Milestone 1 — PR breakdown

Detail in `./docs/drafts/YYYYMMDD-HHMM-m1-plan.md`. One line per PR here;
sub-tasks stay in the plan doc.

- [x] **PR-01** — <scope>.
- [~] **PR-02** — <scope>.
- [ ] **PR-03** — <scope>.

---

## Cross-cutting architectural notes (locked)

- [x] <decision> — <rationale / where it lands>.
- [ ] <open question> — <who/when decides>.

---

## Completed

- **PR-01** (YYYY-MM-DD) — <what shipped, in prose>. Verification:
  `<command>` → <result>, `<command>` → <result>.
  Notes / surprises:
  - <discovery>, <workaround>, <reference to where it's documented in code>.
  - <constraint future work must respect>.
```

Rules:
- Planned tasks live in the breakdown section as `[ ]`. When work starts,
  flip to `[~]`. When a PR merges, flip to `[x]` **and** add a rich entry
  to **Completed**. Do not delete the breakdown line.
- Sub-task detail (D01…Dnn checklists, acceptance criteria, open questions)
  lives in the per-milestone plan doc under `./docs/drafts/`, not here.
- Cross-cutting notes decay as questions resolve: `[ ] decide X` becomes
  `[x] X = <choice>, lands in PR-N`. Never silently delete.
- Completed entries are append-only. They are the audit trail and the
  knowledge base for later subagents — terseness is a failure here.

### `./defects.md` — discovered defects

Every reviewer finding lands here as a **structured entry**, not a
checklist line. The ledger is the audit trail and the reviewing subagent
will re-read it on subsequent rounds — invest in detail so repeat defects
are impossible and the fix rationale survives beyond this session.

**Status legend (always include verbatim near the top):**

```
Status: `[ ]` open · `[~]` under fix · `[x]` resolved
```

**Grouping:** one top-level section per PR/task (`## PR-01`, `## PR-02`,
…). Defects within a PR are numbered `PR-NN-DMM` (`PR-01-D01`,
`PR-01-D02`, …) — the ID never changes once assigned, even after fix.
Separate PR groups with `---`.

**Entry schema** (every defect, open or resolved):

```markdown
## [PR-NN-DMM] <one-line headline that states the problem, not the fix>
**Status:** open | under fix | resolved | resolved (<qualifier, e.g. "mitigated; full fix deferred to PR-25">)
**Severity:** major | minor | nit
**Location:** <absolute or repo-relative path>[:<line>[-<line>]][, <more locations>]
**Description:** <prose. What is wrong, what breaks, under what conditions. Concrete enough that a future subagent reading only this entry can reproduce the problem.>
**Root cause:** <optional; include when the bug originates somewhere non-obvious — upstream library behaviour, generator quirk, flag default, etc. Cite file:line in external sources if you investigated them.>
**Fix:** <what was done, with file:line of the change. For resolved entries this replaces "Suggested fix".>
**Suggested fix:** <for open entries: the recommended approach. Gets replaced by "Fix:" when closed.>
```

Rules:
- **Never delete a defect.** Flip status, fill in **Fix:**, keep it in
  place. The ledger is the audit trail.
- **Headlines describe the problem, never the fix.** "X does Y when it
  shouldn't" — not "fixed Y in X". The headline must still make sense
  years later when the bug has been forgotten.
- **Severity has three levels, not a freeform string.** `major` blocks
  merge; `minor` should be fixed but can be deferred with rationale;
  `nit` is cosmetics / nice-to-have.
- **Resolved with qualifier** is legitimate: `resolved (mitigated; full
  fix deferred to PR-25)`, `resolved (pin retained; rationale
  documented)`, `resolved (note-only; no functional change per defect's
  own guidance)`. The qualifier tells the next reviewer why a defect
  that still "looks wrong" is closed.
- **Location is precise.** Full path + line range for source; ledger
  file + line for ledger-bug defects; release URL for upstream findings.
  Vague locations waste the next round.
- **Root cause is optional but high-value** for anything non-obvious.
  Cite upstream source (e.g. `ScUEBACodecGenerator.scala:548-549`) when
  you had to read it to understand the bug.
- **Fix/Suggested fix is specific.** Name the file, the function, the
  one-liner. "Rename to `UserToken` to match spec §3.2" beats "rename
  appropriately".
- **Cross-round regressions get a new defect.** If a fix in round N
  breaks something fixed in round N-1, open a new `PR-NN-DMM` entry that
  references the earlier one — don't re-open the closed defect.

Create the ledger files if they do not exist. If they already exist with
unrelated content, append a new PR section rather than overwriting.

## The loop

Two nested loops. The **inner loop** drives a single PR/task from planned
to clean-reviewed and committed. The **outer loop** walks the ledger,
running the inner loop for each planned task, and only terminates when
the ledger is drained or the work is blocked. A clean review ends the
inner loop, never the outer.

### Outer loop — walking the ledger

**O1. Seed the plan (only once per session, if the ledger lacks one).**
If `./tasks.md` has no breakdown for the work the user asked about, spawn
a planning subagent. Give it the full user request verbatim plus any
context. Ask for: a milestone breakdown, a PR-level breakdown for the
current milestone, success criteria per PR, and risks/assumptions. The
subagent writes the detailed plan to
`./docs/drafts/YYYYMMDD-HHMM-<name>.md` (sub-task checklists, acceptance
criteria, open questions live there). You reflect it into `./tasks.md`:
the **Milestones** section, a **PR breakdown** section pointing at the
plan doc, and any new **Cross-cutting architectural notes**.

**O2. Pick the next task.** Scan `./tasks.md` for the next `[ ]` (or
resume the `[~]`) in the current milestone's PR breakdown. Flip it to
`[~]`. If the current milestone is fully `[x]`, move to the next
milestone's breakdown (seeding a new plan doc via O1 if none exists for
it yet).

**O3. Run the inner loop on that task.** See below.

**O4. When the inner loop returns clean, go to O2.** Do not stop. Do
not summarise to the user. Do not ask "should I continue?" Continue.

**O5. When the ledger has no more planned/in-progress tasks — or the
inner loop reports a blocker — write the session log and return.** See
the **Session end** section below.

### Inner loop — driving one PR/task to clean

**I1. Execute (subagent, possibly parallel).** Spawn execution
subagent(s) with a self-contained brief: the task, its success criterion,
and the relevant file paths. Independent sub-tasks within the same PR →
parallel subagents in a single message.

**I2. Adversarial review (subagent).** Spawn a review subagent in the
posture of a hostile reviewer: "find what is wrong with this change,
assume it is broken, look for regressions, missing cases, weak tests,
sloppy edits, surprise side effects, unfixed todos." Point it at the
diff and the original task brief. Ask for a structured list of defects
with severity.

**I3. Update ledgers.** Append every reviewer finding to `./defects.md`
as a structured entry (`## [PR-NN-DMM] <headline>` with the full schema:
Status / Severity / Location / Description / Root cause / Suggested fix).
Assign defect IDs sequentially within the PR group; never reuse an ID.
In `./tasks.md` keep the current task at `[~]` (still in progress).
Do this yourself — it is orchestration, not subagent work.

**I4. If the reviewer reported defects, fix and re-review (subagents,
possibly parallel).** For each open defect flip its status to
`[~] under fix`, then spawn a fix subagent. Independent defects →
parallel subagents. Each brief: the full defect entry (headline +
Location + Description + Suggested fix), the fix expectation, the exact
file paths. **Do not edit the code yourself**, even for "trivial" fixes —
that bypasses the loop discipline. When a fix subagent returns, replace
the entry's **Suggested fix:** with **Fix:** (describing what was
actually done, with file:line), and flip status to `resolved` (or
`resolved (<qualifier>)` when the fix is intentionally partial). Then
**go to I2** for another review round.

**I5. Clean review → close out this PR.** When the reviewer returns no
open defects (or only entries both you and the reviewer agree are
out-of-scope, explicitly recorded with `resolved (deferred …)`):
- Flip the task in `./tasks.md` from `[~]` to `[x]`.
- Write a rich **Completed** entry for the PR (what shipped,
  verification commands + results, surprises, workarounds, constraints
  future work must respect).
- Commit the PR's code changes plus the ledger updates. One PR = one
  commit. Commit message names the PR. Do not push unless the user
  asked.
- Return control to the **outer loop** (O4). **Do not return to the
  user here.** The session is not over — more tasks likely remain.

**I6. Blocker in the inner loop.** If at any point the inner loop
uncovers a question that cannot be resolved from the code or the
original brief (ambiguous requirement, missing credential, architectural
choice the user must make, fundamental plan flaw), mark the current
task `[!]` in `./tasks.md`, record the blocker in its **Completed**
entry draft (or in a new `## Blockers` subsection if preferred), and
escalate to **session end** with `reason = blocked`.

### Session end (only fires from O5 or I6)

1. **Session log.** Write `./docs/logs/YYYYMMDD-HHMM-log.md` capturing:
   the original user request, the milestones/PRs actually worked on
   this session, the rounds of review per PR (what was found, what was
   fixed), any deferred defects and why, the final ledger state, and —
   if terminating on a blocker — the specific question the user must
   resolve. The log is written by you, from conversation context — not
   by a subagent. Use the current local date/time for the filename.
2. **Final commit.** Commit the session log (and any remaining ledger
   state not already committed with a PR). Separate commit from the
   per-PR commits. Do not push unless the user asked.
3. **Return to the user.** One short message: which PRs landed, where
   the log lives, and — if blocked — the exact question to resolve. No
   prose recap of each loop iteration; the log has that.

## Stop conditions

Only two valid terminations of the **outer** loop:

- **Ledger drained.** Every task in the relevant milestone breakdown is
  `[x]`, and either no further milestones are in scope for this session
  or the user's original request has been fulfilled. Clean reviews of
  individual PRs do **not** qualify — a clean review ends the inner
  loop, nothing more.
- **Blocked on user input.** The loop has uncovered a question that
  cannot be resolved from the code, the original brief, or the plan
  doc: ambiguous requirement, missing credential, architectural choice
  the user must make, or a plan flaw that needs user judgement. Mark
  the task `[!]`, record the blocker, and escalate to session end.

Running out of patience, hitting a "good enough" point, finishing one
milestone, or wanting to check in mid-session are **not** stop
conditions. Keep going.

## Subagent briefing discipline

Each subagent starts cold. A brief that works:

- States the concrete goal for this subagent (not the session goal).
- Points at exact file paths and line ranges where relevant.
- Quotes the acceptance criterion.
- For reviewers: explicitly asks for an adversarial posture and a structured
  defect list.
- For executors: says whether the subagent should write code or only
  investigate, and what "done" looks like.

A brief that fails: "based on the plan, implement it" or "review the work."
Those push synthesis onto the subagent. You have the context; transfer it.

## Parallelism

- Planning: one subagent.
- Execution: parallel when tasks are independent, serial when they share
  files or build on each other.
- Review: one subagent per round. (Multiple reviewers with different lenses
  — e.g. correctness vs. security — are fine when the change warrants it.)
- Fixes: parallel when defects are independent.

Send parallel subagents in a single message with multiple tool calls; that
is what makes them actually run concurrently.

### Worktrees for parallel editors

Any time you spawn two or more subagents that will **edit** the tree
concurrently — parallel executors in I1, parallel fix subagents in I4,
or any other case — each one needs its own `git worktree`. Two agents
writing into the same checkout will clobber each other's edits, corrupt
the index, and produce a diff that mixes unrelated changes; the loop
cannot recover from that cleanly.

Discipline:

- **Use the Agent tool's built-in `isolation: "worktree"` parameter.**
  Pass `isolation: "worktree"` when spawning each concurrent editor.
  The runtime creates a temporary worktree, runs the subagent inside
  it, and tears it down automatically (auto-cleans if the agent made
  no changes; otherwise returns the worktree path and branch name in
  the agent's result for merge-back). This is the *only* sanctioned
  way to create worktrees in this loop.
- **Never script worktree lifecycle by hand.** Do not run
  `git worktree add`, `git worktree remove`, `rm -rf wt-*`, or any
  equivalent — neither in the orchestrator nor in subagent briefs.
  Manual worktree management causes permission prompts, clobbers the
  runtime's bookkeeping, and is the failure mode that motivated this
  rule. If `isolation: "worktree"` cannot express what you need,
  serialise the work instead.
- **Subagent briefs must not mention worktree management.** Tell the
  executor what to change and where (relative paths within its
  working tree); do not tell it to `cd`, create, remove, or inspect
  worktrees, and do not pass `git -C <path>` style commands. The
  runtime drops the subagent inside its worktree already, so its CWD
  is correct. A defensive line like "operate only in your current
  working directory; do not invoke `git worktree`, `rm`, or any
  path-cleanup command" belongs in every parallel-editor brief.
- **Read-only subagents share the main checkout.** Reviewers (I2),
  planners (O1), and exploration subagents do not need
  `isolation: "worktree"` — they only read.
- **Merge back deterministically.** When each editor returns, you (the
  orchestrator) merge or cherry-pick its commits back into the main
  branch in a defined order, using the path/branch the runtime
  reported in the agent's result. Resolve conflicts at merge time,
  not at edit time. Never let two subagents race for the same file.
- **Serial when it doesn't partition.** If two sub-tasks touch the same
  file or build on each other's output, do not parallelise them across
  worktrees — run them serially in the main checkout. Worktrees are a
  tool for *independent* work, not a way to dodge a sequencing
  requirement.

## Model selection per phase

Quality of the loop is dominated by the quality of **planning** and
**review** — those are the phases where a weaker model silently produces a
plan that misses a milestone, or a review that fails to spot a defect. A
weaker executor wastes a round; a weaker reviewer ships a bug. Spend the
budget where the asymmetry hurts.

Default model assignment, **always overridable when a task obviously
warrants it**:

- **Planning subagents (O1):** the strongest available reasoning model
  with the largest available context — currently Opus-class with the 1M
  context window. Plans need to hold the full spec, the existing ledger,
  and cross-cutting decisions in mind simultaneously.
- **Review subagents (I2):** same — strongest available model, large
  context. The reviewer's job is adversarial pattern-matching against
  the entire diff plus surrounding code; this is exactly where a
  weaker model regresses to surface-level checks.
- **Execution subagents (I1) and fix subagents (I4):** Sonnet-class is
  the default. Most fixes are mechanical once the defect entry names the
  file, the line, and the change. Use the stronger model for executors
  only when the task itself is a non-trivial design decision masquerading
  as "just implement it" — flag this in the brief rather than escalating
  silently.
- **Ledger maintenance, commits, session log:** orchestrator (you), no
  subagent.

Two non-negotiable rules:

- **Never downgrade reviewers to save cost.** A missed defect compounds
  across rounds and into Completed entries that future subagents trust.
  The cost of one extra review-round on the strongest model is trivial
  next to that.
- **Name the model in the subagent brief** when it differs from the
  parent's model. Weaker subagents should know they were chosen for a
  mechanical task and should escalate (return without coding, with a
  written-up question) if the task turns out to need design judgement.

## What lives where

- `./tasks.md` — persistent task ledger (checked in).
- `./defects.md` — persistent defect ledger (checked in).
- `./docs/logs/YYYYMMDD-HHMM-log.md` — one file per session (checked in).
- Code changes — as normal.
- Nothing transient in the loop (draft plans, intermediate reviewer output)
  needs to survive. The ledgers and the log are the record.
