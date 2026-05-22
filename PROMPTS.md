# Prompt overrides

Each `### <ROLE>` section below is appended verbatim to that role's built-in
prompt at dispatch time (after the role's own `<ROLE>.md`, if any). Fill in the
roles you want to extend and leave the rest empty. Headers are matched
case-insensitively; a section's body runs to the next `### ` header.

### COMMON

DEBUG.md — how to debug divergences between the upstream graph and ours.

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

If a task is mostly done, it can be shipped when the remaining refinements would require a new large cycle. In a message, post the rationale for the replanner and merger.

### MERGER

The acceptance gate is `./dev/validate.py` (it builds `ay` itself) — use it as your baseline and post-merge test command, each with its own out-dir (`.out/validate-pre`, `.out/validate-post`).

The pre→post numbers that must improve or stay flat:

- gating `[<case>] OK` count (the byte-exact cases sg2 / sg2_x86_64 / sg3 / sg4) — must not drop;
- `XFAIL` count — must not grow;
- the `[sg5] exact normalized-node parity: matched=… our_total=… ref_total=…` line — `matched` must not decrease, and `our_total` must move toward `ref_total` (the gap shrinks or holds).

### ARBITER

### PUPA

### LUPA
