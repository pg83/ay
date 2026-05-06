# Yatool — Recreate ymake — Defect Ledger

Append-only audit trail of reviewer findings. One section per PR (`## PR-NN`).

Status: `[ ]` open · `[~]` under fix · `[x]` resolved

Entry schema:

> ## [PR-NN-DMM] <one-line headline that states the problem, not the fix>
> **Status:** open | under fix | resolved | resolved (<qualifier>)
> **Severity:** major | minor | nit
> **Location:** <repo-relative path>[:<line>[-<line>]]
> **Description:** <prose>
> **Root cause:** <optional>
> **Fix:** | **Suggested fix:** <prose>

---

## PR-01

### [PR-01-D01] `gen -h`/`-help`/`--help` short-circuits to flag's auto-usage and exits 0 instead of printing the stub message and returning 1
**Status:** resolved
**Severity:** major
**Location:** main.go:50-69 (cmdGen, cmdCompare, cmdInspect)
**Description:** Spec for PR-01 stubs is unconditional: `gen`/`compare`/`inspect` "print `<name>: not implemented yet` to stderr and return 1." Each stub called `flag.NewFlagSet(name, flag.ExitOnError).Parse(args)` with no flags registered; passing `-h`/`-help`/`--help` made the flag package invoke its built-in usage printer and `os.Exit(0)` BEFORE the `fmt.Fprintln(os.Stderr, ...)` line ever ran. Reproduced: `./yatool gen -h` → stderr `Usage of gen:`, exit 0. Two failures: stub message missing, exit code 0 not 1. Same for `compare`/`inspect`.
**Root cause:** `flag.NewFlagSet(..., flag.ExitOnError)` registers an implicit `-h`/`-help`/`--help` handler that prints `Usage of <name>:` and calls `os.Exit(0)` even when no flags are defined.
**Fix:** Removed the `fs.Parse(args)` call in `cmdGen`/`cmdCompare`/`cmdInspect` (main.go:50-69). Kept `flag.NewFlagSet(name, flag.ExitOnError)` construction per spec/D3, assigned to `_ =` (since the flagset is now unused). Parameter changed to `_ []string` (args no longer consumed). Verified: `./yatool gen -h`, `./yatool gen --help`, `./yatool gen --foobar`, `./yatool gen foo bar`, `./yatool compare -h`, `./yatool inspect --foobar` all now print `<name>: not implemented yet` to stderr and exit 1.

### [PR-01-D02] `gen --foobar` exits 2 with empty auto-usage, never prints stub message
**Status:** resolved
**Severity:** minor
**Location:** main.go:50-69
**Description:** Sibling of D01. `flag.ExitOnError` on an unknown flag printed `flag provided but not defined: -foobar` and an empty `Usage of gen:` block to stderr, exited 2. Stub message "gen: not implemented yet" was never emitted; exit code was 2 (which the spec reserves for "no args" / "unknown subcommand"), conflating two distinct exit-code domains.
**Fix:** Same edit as D01 (removed `fs.Parse(args)` in the three stubs). Verified `./yatool gen --foobar` → exit 1 with stub message.

### [PR-01-D03] `fs.Parse(args)` return value discarded without explicit `_ =` in three stubs
**Status:** resolved
**Severity:** nit
**Location:** main.go:52, 59, 66
**Description:** `flag.FlagSet.Parse` returned an `error` discarded without `_ =`. With `flag.ExitOnError` the discard was technically safe but tripped `errcheck`.
**Fix:** Same edit as D01/D02 — `fs.Parse(args)` call removed entirely from the three stubs. Discard question moot.

### [PR-01-D04] Working tree at review time contains undeclared modification to `tasks.md` not part of code diff
**Status:** resolved (orchestrator-bookkeeping; will be committed with PR-01 per CLAUDE.md I5)
**Severity:** nit
**Location:** tasks.md:24
**Description:** `git status` at review time reported `tasks.md` modified (`[ ] PR-01` → `[~] PR-01`) in addition to the new `go.mod`/`main.go`. The reviewer brief said "those are the only changed/new files" and asked to flag anything else. The change is the orchestrator's CLAUDE.md-mandated I3 bookkeeping (flip in-progress task to `[~]`), legitimate scope drift — not an executor defect.
**Fix:** Orchestrator will stage `tasks.md` together with the code in the PR-01 commit per CLAUDE.md I5 ("One PR = one commit"). Future review briefs will enumerate expected ledger churn so reviewers know what to expect.

### [PR-01-D05] Stub `_ = flag.NewFlagSet(...)` constructs and discards a flagset on every invocation; `"flag"` import is dead-loaded
**Status:** resolved (deferred to PR-10)
**Severity:** nit
**Location:** main.go:51, 57, 63 (and `"flag"` import at line 4)
**Description:** Round-2 reviewer finding. After the D01/D02/D03 fix removed `fs.Parse(args)`, the surviving `_ = flag.NewFlagSet(name, flag.ExitOnError)` line in each of `cmdGen`/`cmdCompare`/`cmdInspect` constructs an object only to discard it. The `"flag"` import is loaded solely to make this ceremony compile. Three small smells in one: constructing-to-discard, `_ =` on a constructor with no side effect (reads as "pretending to use this"), import existing only for the pretence. D3's intent — keep the architectural mechanism in place for PR-10 — is not actually preserved by `_ =` because PR-10 will rewrite these stubs anyway.
**Fix:** Deferred to PR-10. PR-10 will replace each stub's body with real flag registration (`fs.String(...)`/`fs.Bool(...)`), `fs.Parse(args)` (with appropriate error mode), and a real `args := fs.Args()` consumption — at which point the `_ =` ceremony is naturally removed and the `"flag"` import becomes load-bearing. Cost of fixing now (drop the line + remove import) is one tiny edit but adds a round-3 review trip; the deferral rationale matches CLAUDE.md "resolved (deferred …)" semantics. Constraint logged for PR-10: must remove all three `_ = flag.NewFlagSet(...)` lines and either keep the `"flag"` import (if real flags land) or drop it.
