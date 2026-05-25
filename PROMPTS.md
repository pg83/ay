# Prompt overrides

Each `### <ROLE>` section below is appended verbatim to that role's built-in
prompt at dispatch time (after the role's own `<ROLE>.md`, if any). Fill in the
roles you want to extend and leave the rest empty. Headers are matched
case-insensitively; a section's body runs to the next `### ` header.

### COMMON

DEBUG.md — how to debug divergences between the upstream graph and ours.

Fresh worktrees may not expose Go on `PATH`. Use the repo-local `./go` shim for Go commands (`./go test ./...`, `./go build ...`); `./dev/validate.py` builds through the same shim.

### OVERSEER

### REPLANNER

Tip — don't put concrete numbers in tickets. "reduce the difference in CC nodes by 2x or more" is better than "close the gap entirely".
Tip — read all the plans from closed `plan` tasks.

One of your jobs is to study the new workspaces and messages, understand where the team might be stuck, and:

* replan tickets
* if you see the team is missing tooling — plan tasks to build it
* if you see the quality-acceptance tooling is flaky or not good enough — plan tickets to improve it

### TASKER

### DIGGER

If a task is mostly done, it can already be sent to review when the remaining refinements would require a new large cycle. In a message, post the rationale for the replanner and reviewer.

### REVIEWER

REWORK is expensive — it costs a full digger → review → merge cycle. Spend it ONLY on things that block the ticket's goal: wrong behavior, failing tests, or a change that diverges from the ticket intent. Everything else is a `message`, not a bounce.

Do NOT REWORK for housekeeping: dead code or orphaned helpers / fields / constants left behind by the change, leftover hygiene, naming, style, or "this could be refactored". If the change is correct and the tests pass, APPROVE and note any such cleanup in a `message` — the replanner can spin a follow-up ticket if it's worth it.

If a task is mostly done, ship it when the remaining refinements would require a new large cycle; post the rationale for the replanner and merger in a `message`.

### MERGER

The acceptance gate is `./dev/validate.py` (it builds `ay` itself) — use it as your baseline and post-merge test command, each with its own out-dir (`.out/validate-pre`, `.out/validate-post`).

The pre→post numbers that must improve or stay flat:

- gating `[<case>] OK` count (the byte-exact cases sg2 / sg2_x86_64 / sg3 / sg4) — must not drop;
- `XFAIL` count — must not grow;
- the `[sg5] exact normalized-node parity: matched=…` line — `matched` must not decrease.

### ARBITER

### PUPA

### LUPA
