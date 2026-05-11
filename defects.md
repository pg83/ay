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
**Fix:** Deferred to PR-10. PR-10 will replace each stub's body with real flag registration, at which point the ceremony is naturally removed. Constraint logged for PR-10: must remove all three `_ = flag.NewFlagSet(...)` lines and either keep the `"flag"` import (if real flags land) or drop it.

---

## PR-02

### [PR-02-D01] `TestFinalize_UIDsStableAcrossRuns` asserts `SelfUID == UID`, encoding an invariant the real graph violates
**Status:** resolved
**Severity:** minor
**Location:** emitter_test.go:110-111
**Description:** Test errors when `n.SelfUID != n.UID`. PR-02 brief sets SelfUID == UID as a placeholder, but in `/home/pg/monorepo/yatool_orig/g.json` 101 of 3,730 nodes have `self_uid != uid`. Test currently locks in a wrong invariant; future PR fixing SelfUID will need to rewrite the test, and a reviewer reading the test as spec gets the wrong picture.
**Fix:** Replace equality assertion with placeholder-acknowledgment comment + a `t.Logf` (not `t.Errorf`) noting the placeholder. Or add an explicit `// TODO(future-PR)` so the test screams when SelfUID gains semantics.

### [PR-02-D02] Pre-set `Node.Deps`/`Node.ForeignDeps` slices are silently accepted, hashed, and emitted without validation
**Status:** resolved
**Severity:** minor
**Location:** emitter.go:232-272
**Description:** Contract per `node.go` header is "rules use Emitter, never assemble Deps directly." But `Finalize` only resolves `DepRefs` when `len(node.DepRefs) > 0`; otherwise it preserves whatever `node.Deps` happens to contain. A misbehaving rule (or test mistake) thus poisons the Merkle hash with arbitrary strings, no error, no warning. Same for ForeignDeps.
**Fix:** At top of per-node loop in Finalize, reject pre-populated `node.Deps`/`node.ForeignDeps` with a concrete error (`finalize: node %d has pre-populated Deps; rules must use DepRefs only`).

### [PR-02-D03] No node-level dedupe — two `Emit`s with identical content produce two graph entries sharing the same UID
**Status:** resolved
**Severity:** minor
**Location:** emitter.go:298-306
**Description:** Two `Emit` calls with identical Node content produce two separate entries in `Graph.Graph` with identical UIDs. Real ymake's `graph` is logically a set keyed by UID. Comparator (PR-04) will count duplicate nodes if our serializer emits them.
**Fix:** In the final loop building `out.Graph`, skip nodes whose UID already appeared (`seen := map[string]struct{}{}`). Add a one-line test asserting `len(g.Graph) == len(distinct UIDs)` for an emitter emitting two identical leaves.

### [PR-02-D04] Comment claims Finalize re-call would "observe missing input" but it actually re-finalizes silently with stale data
**Status:** resolved
**Severity:** nit
**Location:** emitter.go:287-292
**Description:** Comment says "Drop the internal *Refs so they cannot accidentally leak past Finalize (and so a subsequent re-Finalize would observe missing input rather than silently re-resolving stale data)." Probe: Finalize twice produces identical UIDs without error because `node.Deps` is preserved post-resolution. The safety the comment claims doesn't exist.
**Fix:** Either delete the second clause of the comment, OR make Finalize actually defensive (track `finalized bool` on BufferedEmitter, return error on second call). Prefer the latter — pairs with D02.

### [PR-02-D05] `ForeignDepRefs` map with empty value-slice serializes as `foreign_deps:{key:[]}`, perturbing parent hash vs. omit-key case
**Status:** resolved
**Severity:** nit
**Location:** emitter.go:252-272
**Description:** Probe: `ForeignDepRefs: map[string][]NodeRef{"tool": {}}` produces output `"foreign_deps":{"tool":[]}`. Real g.json never shows a foreign_deps key with empty list — key is either absent or populated.
**Fix:** In foreign-deps resolution loop, skip keys with empty resolved slice. If no key survives, leave `node.ForeignDeps` nil so omitempty drops the field entirely.

### [PR-02-D06] `Result(NodeRef)` called twice with same ref produces duplicate UIDs in `Graph.Result`
**Status:** resolved
**Severity:** nit
**Location:** emitter.go:69-71, 307-309
**Description:** Calling `e.Result(a)` twice produces a 2-element `Graph.Result` with the same UID twice. Real ymake's `result` is a set, not a multiset.
**Fix:** Dedupe in Finalize after translating ids to UIDs (preserve first-seen order). Or reject in `Result()` itself with `seen map[int64]bool`.

### [PR-02-D07] Topo tie-break uses O(n²) linear scan over the queue on every pop
**Status:** resolved (deferred — premature optimization for M1; revisit when comparator runs against the full 3,730-node graph and profiling shows the hotspot)
**Severity:** nit
**Location:** emitter.go:192-211
**Description:** Topo loop scans entire pending queue every pop to find smallest-index zero-in-degree node. ~14M ops for 3,730 nodes (fine); ~10^10 for 100k+ nodes (not fine). Stdlib `container/heap` would give O(n log n).
**Fix:** Deferred to a later milestone. Constraint logged for whoever profiles M2/M4 — replace queue with `container/heap` if topo shows up in profile.

### [PR-02-D08] PR-04 (g.json reader/writer) must use `json.Encoder` with `SetEscapeHTML(false)` to match `canonicalNodeBytes` output, or hash and bytes diverge
**Status:** resolved (cross-cutting constraint logged in tasks.md; pinning test added in PR-02)
**Severity:** minor
**Location:** uid.go:46-65 (correct); PR-04 has no enforcement yet
**Description:** `canonicalNodeBytes` correctly disables HTML escaping. The eventual JSON serializer (PR-04) is not yet present, and nothing in PR-02 records that PR-04 must use the same setting. Probe: `json.Marshal(node)` (default escape on) emits `<` for `<`, while `canonicalNodeBytes` emits `<`. If PR-04 uses `json.Marshal` naively, the file written to disk will contain bytes the hash never saw.
**Fix:** Cross-cutting note added in tasks.md (D14 already covers it but PR-04 needs an explicit anchor). PR-02 fix subagent adds a pinning test asserting `json.Marshal(node)` (default) and `canonicalNodeBytes(node)` differ on HTML special chars, locking the contract in code.

### [PR-02-D09] `BufferedEmitter.Emit` and `Result` do not check `finalized` flag — silent post-Finalize mutations
**Status:** resolved (deferred — to be addressed when Emitter interface is revisited; could land as a `panic("emit on finalized emitter")` guard, no caller signature change)
**Severity:** nit
**Location:** emitter.go:62-66 (Emit), emitter.go:71-73 (Result)
**Description:** D04's fix added `finalized bool` to `BufferedEmitter` and a check at `Finalize` start. But neither `Emit` nor `Result` consults the flag. After successful Finalize, `e.Emit(...)` appends a `*Node` to `e.nodes` and `e.Result(ref)` appends to `e.results` — both succeed silently. A subsequent third `Finalize` errors out, but the user has no signal that intervening Emit/Result calls were ineffective. Sharp edge for iterative drivers that hold the emitter across passes.
**Fix:** Deferred. Constraint logged for whoever revisits the Emitter interface (M3 streaming-emitter introduction is a natural pivot point): add `panic("emit on finalized emitter")` (and symmetric for Result) at method start, with comment pointing at Finalize doc.

### [PR-02-D10] Partial-empty `ForeignDepRefs` (one key empty, another non-empty) has no test coverage
**Status:** resolved (deferred — easy 5-line test addition; logged as known coverage gap; will pick up in M2 when the comparator catches a real `foreign_deps` divergence)
**Severity:** nit
**Location:** emitter_test.go (no test); emitter.go:285-302 (untested code path)
**Description:** D05's fix has two guards: inner `if len(set) == 0 { continue }` skips a single empty-resolved key; outer `if len(resolved) > 0` decides whether to set `node.ForeignDeps`. `TestFinalize_DropsEmptyForeignDepsKey` only exercises the all-empty case. Partial case `ForeignDepRefs: {"tool": [t1], "host": []}` — should yield `{"tool": [...]}` with `host` dropped — has no test. Regression that removed the inner `continue` (kept outer) would slip past existing test.
**Fix:** Deferred. Test to add: `TestFinalize_DropsOnlyEmptyForeignDepsKey` — emit leaf t1, then node with `ForeignDepRefs: {"tool": []NodeRef{t1}, "host": []NodeRef{}}`, finalize, assert `len(aNode.ForeignDeps) == 1` and `"host"` absent. ~5 LOC.

---

## PR-07

### [PR-07-D01] Tests depend on absolute paths to a sibling repo (`/home/pg/monorepo/yatool_orig/...`) — fragile to relocation, upstream edits
**Status:** resolved
**Severity:** major
**Location:** yamake_test.go:10, yamake_test.go:73
**Description:** `TestParseArchiverYaMake` and `TestParseLibraryArchiveYaMake` hard-code `/home/pg/monorepo/yatool_orig/...` paths. Plan D11 says unit tests use synthetic inputs; only integration tests (M2+) read upstream. Anyone without `yatool_orig` checked out at exactly that path gets two failing tests; benign upstream edits silently break.
**Fix:** Inline the contents of these two `ya.make` snippets as Go string literals in the test file (12 + 14 lines, trivial). Keep structural assertions, feed `Parse` directly. Optionally gate the disk-read variant behind `t.Skip` when file missing.

### [PR-07-D02] Lone `\r` (CR) treated as in-line whitespace — Mac-classic line endings don't bump the line counter, lone CRs in stray files report wrong line numbers
**Status:** resolved
**Severity:** major
**Location:** yamake.go:149-159 (`advance`), yamake.go:161-163 (`isWhitespace`)
**Description:** `isWhitespace` accepts `\r` but `advance` only increments `l.line` on `\n`. For `\r\n` line endings the col counter is wrong. For lone `\r` (Mac-classic, or stray CRs), `PROGRAM()\rEND()` parses both stmts on `Line: 1`. Wrong-answer bug for line numbers, not a parse failure.
**Fix:** In `advance`, treat `\r` as a line terminator (`l.line++; l.col = 1`), and swallow the `\n` of a following `\r\n` pair so it doesn't double-bump. Add CRLF + lone-CR test pair.

### [PR-07-D03] Strings silently span newlines — line counter advances inside string body, missing closing quote swallows everything until next `"` somewhere downstream
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:255-275 (`readString`)
**Description:** Probe: `SET(NAME "line1\nline2")\nEND()\n` produces `SetStmt{Value: "line1\nline2"}` followed by `EndStmt{Line: 3}`. ya.make doesn't allow multiline strings; missing closing quote silently swallows until next `"` downstream, producing confusing AST instead of "unterminated string" pinned at the open quote.
**Fix:** Reject literal `\n` (and `\r`) inside `readString` with a precise `unterminated string` error pinned to the opening quote's line/col.

### [PR-07-D04] Mixed-case macro names (e.g. `Foo()`) produce hard-error "expected macro name, got word" instead of `UnknownStmt`, contradicting spec's "do NOT error on unknown macro names"
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:284-306 (`readIdentOrWord`), yamake.go:333-335 (top-level dispatch)
**Description:** Spec: "everything else → `UnknownStmt`. Do NOT error." Today, a token starting uppercase but containing lowercase / `.` / `/` is reclassified to `tokWord` and rejected at top level. Spec violation for any non-ALL_CAPS macro the parser ought to tolerate.
**Fix:** When `tokWord` (or relaxed-IDENT) is followed by `(`, accept it as an UnknownStmt name. Add tests for `lowercase_macro()` and `Mixed_Case()` → `UnknownStmt`.

### [PR-07-D05] `isWordByte` permits backtick, semicolon, pipe, ampersand, caret, angle brackets, single quote, braces beyond `${VAR}`, tilde — none appear in real ya.make atoms
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:181-195 (`isWordByte`)
**Description:** Set is too broad — typos like `` `foo `` become a "word" instead of triggering "unexpected character" diagnostics. `@` correctly fails today, but only by accident of which bytes were enumerated.
**Fix:** Trim to minimum required by today's atoms: `_ - . / + : = * ? $ % ~ , !`. Drop `` ` ; | & ^ < > [ ] ' ``. Add a test pinning the accepted/rejected boundary.

### [PR-07-D06] `# ...` inside an unquoted argument terminates the word and starts a comment, swallowing the closing `)`
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:197-214 (`skipTrivia`), yamake.go:181-195 (`isWordByte`)
**Description:** Probe: `PEERDIR(a/b#x c/d)\n` errors with `unterminated macro call "PEERDIR" (missing ')')`. Failure mode is silent; user sees missing-paren error several lines from the bad path.
**Fix:** In `skipTrivia`, only enter comment mode when `#` is at a whitespace boundary (preceding byte was whitespace, start-of-file, or `(`). Add test for `PEERDIR(a/b#x  # this IS a comment\n)`.

### [PR-07-D07] `PeerdirStmt`/`SrcsStmt`/`UnknownStmt` ship `nil` `[]string` for empty arg lists — no test pins which (nil vs `[]string{}`)
**Status:** resolved (deferred — internal AST detail with no current downstream consumer; revisit when a serializer or comparator depends on the distinction)
**Severity:** nit
**Location:** yamake.go:382-401 (`buildStmt`)
**Description:** `PEERDIR()` produces `PeerdirStmt{Paths: nil, Line: 1}`. JSON differs (`null` vs `[]`), but this AST is internal — emitter normalizes downstream.
**Fix:** Deferred. Constraint logged: when AST is JSON-serialized for any reason (debug dump, comparator input), normalize nil → `[]string{}` at the boundary.

### [PR-07-D08] Backslash-in-string semantics ambiguous between two valid readings of "treat `\"` as a literal"; no test pins what was implemented
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:255-275 (`readString`)
**Description:** Brief said "treat `\"` as a literal if you encounter it, but don't error". Two valid readings: (a) `\"` is two literal bytes (current behavior), or (b) `\"` is a literal single `"` inside the value (standard escape). Executor picked (a) without flagging. No test pins it.
**Fix:** Keep current behavior (no escape processing — simpler, easy to revisit). Add a test like `assertString("a\\b", "a\\b")` — verifying and DOCUMENTING "no escape processing" via test name and comment. Locks the decision in code.

### [PR-07-D09] EOF token position robustness — diagnostic for unterminated multi-line macro pins to macro-name token (not EOF), today correct by inspection but not by test
**Status:** resolved (deferred — verified correct by inspection, no actual bug; nit-tier coverage gap)
**Severity:** nit
**Location:** yamake.go:222-247 (`readToken`), yamake.go:139-146 (`errorf`)
**Description:** `unterminated macro` error pins to the macro name token's start (lines 358, 370, 374, 376), which is correct. But no test asserts the position for a multi-line spanning case (open paren on line 1, EOF on line 5).
**Fix:** Deferred. Optional one-line pin test could be added when convenient; no behavior change required.

### [PR-07-D10] String-adjacent-to-bare-word lexes as multiple tokens (`foo"bar"baz` → 3 args) — surprising, undocumented, no test
**Status:** resolved (deferred — defensible greedy lexing, no current real-world hit, behavior is implicit but not harmful)
**Severity:** nit
**Location:** yamake.go:222-248 (`readToken`)
**Description:** `foo"bar"baz` inside a macro arg list lexes as 3 separate tokens. Real ya.make doesn't do this; surprising relative to "args are space-separated" mental model.
**Fix:** Deferred. If reviewer of a future PR cares, add a one-line test pinning the current behavior + a code comment documenting "tokens do not require whitespace separation".


---

## PR-11

### [PR-11-D01] Missing blank lines around `if _, ok := seenNode[u]; ok { continue }` inside Finalize output loop
**Status:** resolved
**Severity:** minor
**Location:** emitter.go:357-361, :368-372
**Description:** STYLE.md mandates blank lines BEFORE and AFTER every control block (`if`/`for`/`switch`/`select`/`go`/`defer`) unless first/last in `{}`. In `Finalize`'s output construction loop, `u := uids[i]` is followed directly by `if _, ok := seenNode[u]; ok { continue }` (no blank), then closing `}` followed directly by `seenNode[u] = struct{}{}` (no blank). Same shape at `:368-372` for `seenResult`. Pre-existing PR-02 shapes that PR-11 was supposed to clean up.
**Suggested fix:** Insert blank line BEFORE each `if _, ok := ...` and AFTER its closing `}` at both sites.

### [PR-11-D02] Missing blank line before `if indeg[c] == 0 { ... }` in inner Kahn-step loop
**Status:** resolved
**Severity:** minor
**Location:** emitter.go:233-238
**Description:** Inside the inner loop body `for _, c := range children[i] { indeg[c]--; if indeg[c] == 0 { ... } }` the `if` is immediately preceded by `indeg[c]--` with no blank. The `if` is neither first nor last in the for-body so STYLE.md exemption doesn't apply.
**Suggested fix:** Insert blank line between `indeg[c]--` and `if indeg[c] == 0 { queue = append(queue, c) }`. (No blank after needed — `if` IS last in the for-body.)

### [PR-11-D03] Missing blank lines around dedupe `if`-blocks in Emit's DepRefs/ForeignDepRefs walks
**Status:** resolved
**Severity:** minor
**Location:** emitter.go:183-189, :197-205
**Description:** Inside `for i, node := range e.nodes`, the inner blocks `for _, r := range node.DepRefs { if _, ok := seen[r.id]; ok { continue }; seen[r.id] = struct{}{}; addEdge(...) }` omit blanks around the `if`. Same at the ForeignDepRefs walk (:197-205).
**Suggested fix:** Insert blank line BEFORE `if _, ok := seen[r.id]` and AFTER its closing `}` in both loops.

### [PR-11-D04] Missing blank line before `if n := len(out); n > 0 && out[n-1] == '\n'` in canonicalNodeBytes
**Status:** resolved
**Severity:** minor
**Location:** uid.go:63-68
**Description:** `out := buf.Bytes()` then a comment then `if n := len(out); ... { out = out[:n-1] }` — no blank between assignment and the if-comment block. A comment line does NOT satisfy STYLE.md's blank-line requirement; the `if` is the second statement in the function so the "first stmt" exemption doesn't apply.
**Suggested fix:** Insert blank line between `out := buf.Bytes()` and the comment preceding the `if`.

### [PR-11-D05] Missing blank line before `if absErr != nil` inside ParseFile's Try body
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:97-101
**Description:** Inside `Try(func() { ... })`, `abs, absErr := filepath.Abs(path)` is immediately followed by `if absErr != nil { ... }`. The `if` is the third statement in the closure body — neither first nor last.
**Suggested fix:** Insert blank line between `abs, absErr := filepath.Abs(path)` and `if absErr != nil { ... }`.

### [PR-11-D06] Missing blank line before `if tok.kind == tokEOF { break }` in parseInternal loop
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:491-494
**Description:** `for { tok := p.lex.next(); if tok.kind == tokEOF { break }; ... }` — `tok := p.lex.next()` directly precedes the `if` with no blank. Inconsistent with line 561-563 (same file) where the same pattern IS correctly blanked.
**Suggested fix:** Insert blank line between `tok := p.lex.next()` and `if tok.kind == tokEOF { break }`.

### [PR-11-D07] Missing blank line between `b := l.src[l.pos]` and first `if b == '"'` in readString loop
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:379-388
**Description:** Inside the `for {}` loop in `readString`, `b := l.src[l.pos]` is immediately followed by `if b == '"' { ... }`. Subsequent if-blocks within the same loop body ARE correctly blank-separated (so the missing blank is only at the top of the body).
**Suggested fix:** Insert blank line between line 379 (`b := l.src[l.pos]`) and line 380 (`if b == '"' {`).

### [PR-11-D08] Missing blank lines around `if isIdentCont(b)` / `if isWordByte(b)` arms in readIdentOrWord
**Status:** resolved
**Severity:** minor
**Location:** yamake.go:413-429
**Description:** `for l.pos < len(l.src) { b := l.src[l.pos]; if isIdentCont(b) {...continue}; if isWordByte(b) {...continue}; break }` — the `b := ...` directly precedes the first `if` (no blank) and the consecutive `if {...continue}` arms touch each other (no blank). At minimum the assignment-to-first-if blank is required by STYLE.md; blanks between the two `if` arms is judgment but reads cleaner.
**Suggested fix:** Insert blank line BEFORE first `if isIdentCont(b)`. Optional: also insert a blank between the two `if` arms (they're distinct branches, not one logical operation).

### [PR-11-D09] Missing blank lines after several `for {...}` blocks in emitter_test.go
**Status:** resolved
**Severity:** nit
**Location:** emitter_test.go:212-214, :325-327, :455-457 (and similar)
**Description:** Recurring pattern: `for _, n := range g.Graph { if n.KV["name"] == "A" { aNode = n } }` immediately followed by `if aNode == nil { ... }` with no blank between the for-block's closing `}` and the next `if`. Inconsistent with other sites in the same file where the blank IS present (e.g. line 170-172).
**Suggested fix:** Walk every `for {...}` in `emitter_test.go` and insert a blank line after the closing `}` unless the for-block is the last statement in its enclosing `{}`.

### [PR-11-D10] `errors.As` reassignment in Parse/ParseFile is logically inert (no-op against today's throw paths)
**Status:** resolved
**Severity:** nit
**Location:** yamake.go:106-115 (ParseFile), :469-481 (Parse)
**Description:** `err = exc.AsError(); var pe *ParseError; if errors.As(err, &pe) { err = pe }`. For throw paths we control, `exc.AsError()` returns `*ParseError` directly (because `throwParse` constructs `New(pe)`), so `errors.As(err, &pe)` succeeds and reassigns `err = pe` — but `err`'s dynamic type is already `*ParseError`. The reassignment is defensive against a future `fmt.Errorf("...: %w", pe)` wrap path that doesn't exist yet.
**Suggested fix:** Add a one-line comment noting the reassignment is "defensive against future fmt.Errorf wrapping" — OR delete the `var pe / if errors.As / err = pe` block as dead-code-today (preferred — it's three lines of dead code; resurrect when a wrapper actually appears).

### [PR-11-D11] `dispatch` extraction comment over-promises "contract above keeps working unchanged" while os.Exit-on-success bypasses any future Try defers
**Status:** resolved
**Severity:** minor
**Location:** main.go:31-51
**Description:** `dispatch` calls `os.Exit(cmdGen(...))` etc. Today this is harmless (stubs print + return 1; panics in cmdGen propagate up to Try correctly). But on a CLEAN exit (`os.Exit(0)`), any deferred cleanup placed by the outer Try is silently skipped. Future PR adding profile flush / log close / etc. in `main` would lose it on success exits. Comment over-promises invariance.
**Suggested fix:** Add a one-line caveat to the dispatch comment: "Note: `os.Exit` from a subcommand bypasses any defers placed by the outer Try; only panics propagate. If success-path cleanup needs to fire from Try, dispatch must return an exit code instead of calling os.Exit." No code change required.

### [PR-11-D12] `finalizeExc` test helper docstring doesn't warn against wrapping success-path tests
**Status:** resolved
**Severity:** nit
**Location:** emitter_test.go:62-72
**Description:** Helper is correctly used only in error-expecting tests; success-path tests call `Finalize(e)` directly so unexpected panics surface as test failures. Future contributor might mistakenly wrap a success-path test in `finalizeExc`, defeating the panic-catches-bugs property.
**Suggested fix:** Append to docstring: "Success-path tests should call `Finalize(e)` directly so that an unexpected panic surfaces as a test failure rather than being silently captured by Try."

---

## PR-03

### [PR-03-D01] `./yatool inspect -h` and `--help` exit 1 with cryptic "flag: help requested" instead of printing usage and exiting 0
**Status:** resolved
**Severity:** minor
**Location:** main.go:100-102 (cmdInspect's flag.Parse + missing flag.ErrHelp handling)
**Description:** `flag.ContinueOnError` returns the sentinel `flag.ErrHelp` after printing "Usage of inspect:" to stderr. Current code does `Throw(fs.Parse(args))`, so the sentinel becomes a panic carrying "flag: help requested" that main's Catch prints to stderr and the process exits 1. User explicitly asked for help; exit 1 mis-signals "you did something wrong". PR-01-D05 deferred constraint expected the subcommand body to "retain control of exit semantics" — adopted ContinueOnError but didn't actually take control. PR-10's `cmdGen`/`cmdCompare` will inherit this UX unless solved here.
**Suggested fix:** In cmdInspect, discriminate `flag.ErrHelp` (STYLE.md "Discriminate" pattern):
```go
fs.SetOutput(os.Stdout) // help is not an error → stdout
err := fs.Parse(args)
if errors.Is(err, flag.ErrHelp) {
    return 0
}
Throw(err)
```
Codify same shape for PR-10.

### [PR-03-D02] `./yatool inspect -bogus` prints unknown-flag error twice on stderr
**Status:** resolved
**Severity:** minor
**Location:** main.go:100-102 (cmdInspect)
**Description:** With `flag.ContinueOnError`, the flag package writes "flag provided but not defined: -bogus" to fs.Output() (stderr), then calls fs.Usage() ("Usage of inspect:" same stream), then returns the error. Throw panics with that error, main's Catch prints it again. Result: duplicated error line on stderr.
**Suggested fix:** `fs.SetOutput(io.Discard)` before Parse so throw path owns all diagnostics. Combine with D01: in the ErrHelp branch route usage to stdout via `fmt.Fprint(os.Stdout, ...)` or temporarily `fs.SetOutput(os.Stdout); fs.Usage()`. Add tests for both flag-help and unknown-flag paths.

---

## PR-08

### [PR-08-D01] cc_test.go has stray blank lines after opening braces, violating D18
**Status:** resolved
**Severity:** nit
**Location:** cc_test.go:42-43, 54-56, 105-107
**Description:** STYLE.md "Formatting" / D18: no blank line if statement is first inside `{}`. Three spots in cc_test.go violate: `if err != nil {\n\n\tif os.IsNotExist(err) {`, `for _, n := range g.Graph {\n\n\tif len(n.Outputs) > 0`, `for i := range wantArgs {\n\n\tif got.Cmds[0].CmdArgs[i] != wantArgs[i]`. PR-11 enforced this style on existing tree; PR-08 reintroduces 3 regressions.
**Suggested fix:** Delete the blank line immediately after each `{` opening so the nested statement is the first line of the block. Three single-line deletions.

### [PR-08-D02] env map literal shared between Cmds[0].Env and top-level Env (aliasing footgun)
**Status:** resolved (deferred — latent footgun, no current bug; addressing-or-deferring decision: defer, document)
**Severity:** nit
**Location:** cc.go:82-94
**Description:** Single `map[string]string` literal assigned to BOTH `node.Cmds[0].Env` and `node.Env`. Maps are reference types — any future code mutating one mutates the other. EmitCC is single-shot so latent today. Reference's two fields are identical maps, so semantically the alias is a faithful representation.
**Fix:** Deferred. Add to PR-08 closing notes a constraint: "future PRs that add post-emit Node mutation must clone Cmds[i].Env and Env before mutating either. EmitCC currently aliases them; comment noting this is left in cc.go for future maintainers."

### [PR-08-D03] EmitCC unconditionally applies no-libc bundle even for inputs that aren't NO_LIBC modules
**Status:** resolved (deferred — TODO already in code; M2 work)
**Severity:** nit
**Location:** cc.go:73-75 (TODO at cc.go:44-48), flags.go:158-160
**Description:** Brief explicitly accepts that EmitCC need only be byte-exact for `build/cow/on/lib.c`. Function hardwires no-libc bundle (twice) + CATBOOST_OPENSOURCE define unconditionally; calling EmitCC for a non-NO_LIBC module produces mismatched flags. TODO present in code.
**Fix:** Deferred. M2 work. When next CC leaf without NO_UTIL/NO_LIBC/NO_RUNTIME lands, gate `noLibcUndebugBlock` and `catboostOpenSourceDefine` on a module-flavor flag. Logged.

---

## PR-09

### [PR-09-D01] Archive-naming convention docstring overstates generality — `replace("/", "-")` only matches reference for ≤3-component module_dirs
**Status:** resolved
**Severity:** minor
**Location:** ar.go:5-9 (docstring), ar.go:23 (impl)
**Description:** `EmitAR` derives archive name as `"lib" + strings.ReplaceAll(moduleDir, "/", "-") + ".a"`. Works for `build/cow/on` (M1 leaf). Inspection of 48 AR nodes in g.json shows real ymake convention is NOT a uniform path-to-dash transform: `contrib/libs/...` drops leading `contrib`; `library/...` drops leading `library`; `util` becomes `libyutil.a` (special `y` prefix). Docstring claims this is "yatool's convention", misleading for PR-10+.
**Suggested fix:** Tighten docstring: scope to M1 leaf only, add `// TODO(M2)` pointing at future generic naming function. No code change needed for PR-09 byte-exactness.

### [PR-09-D02] `cmd_args` and `inputs` preserve caller order rather than sorting `.o` paths — multi-source modules will not be byte-exact
**Status:** resolved (deferred to M2 multi-source PR)
**Severity:** minor
**Location:** ar.go:48, :52-54, docstring:18-21
**Description:** Multi-source AR nodes in g.json (e.g. `contrib/libs/base64/avx2` with 2 .o inputs) emit cmd_args trailing .o paths AND inputs in alphabetical order. EmitAR currently does `append(cmdArgs, objPaths...)` and `append(inputs, objPaths...)` — caller order preserved. Single-source M1 leaf passes; multi-source will silently fail byte-exact. Docstring overpromises generality.
**Fix:** Deferred to M2 multi-source PR. When implementing multi-source, sort `objPaths` (and `objRefs` in lockstep) before composition. PR-09 docstring will be tightened (D01 same edit) to scope correctly.

### [PR-09-D03] EmitAR does not validate `len(objRefs) == len(objPaths)`
**Status:** resolved
**Severity:** minor
**Location:** ar.go:22-92
**Description:** Brief: "caller is responsible for keeping the two slices in step." Today's only caller passes len==1 for both, but PR-10's `gen` driver wiring CC outputs to AR could easily slip silently — adding a .o to objPaths without matching objRef yields an AR node whose inputs reference a .o not in deps. Violates throw-style discipline (STYLE.md): such precondition violations should `ThrowFmt`.
**Suggested fix:** At top of EmitAR: `if len(objRefs) != len(objPaths) { ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths)) }`. Cheap, throw-style, protects PR-10's wiring.

### [PR-09-D04] Hardcoded environment-specific Python path `/ix/realm/pg/bin/python3` has no TODO marker
**Status:** resolved
**Severity:** minor
**Location:** ar.go:36
**Description:** `cmd_args[0]` hardcoded to reference graph's value `/ix/realm/pg/bin/python3` — clearly user/host-specific. Byte-exactness requires this exact string today. No TODO flags this as future templating point — anyone extending EmitAR to a different host will hit confusing byte-mismatch.
**Suggested fix:** Add comment immediately above the python3 string: `// TODO(portability): python3 path captured from reference build; future work must template from TargetCfg or detect from $PATH.` No code change for byte-exactness.

### [PR-09-D05] Test omits any assertion on the deps relationship (DepRefs count)
**Status:** resolved
**Severity:** nit
**Location:** ar_test.go:83-167
**Description:** Test bypasses `Finalize` (correct — UID would diverge) and doesn't compare Deps UID-for-UID. But it ALSO never asserts `len(got.DepRefs) == len(ref.Deps)` — reference has 1 dep; executor passes 1 objRef; nothing pins this 1:1. Regression where EmitAR forgot to populate DepRefs (or populated twice) would slip past byte-exact test.
**Suggested fix:** Add `if len(got.DepRefs) != len(ref.Deps) { t.Errorf("DepRefs len = %d, want %d (ref Deps count)", len(got.DepRefs), len(ref.Deps)) }` after existing assertions.

### [PR-09-D06] `cmdEnv` and `topEnv` are duplicate map literals — sharing-vs-duplication intent undocumented
**Status:** resolved (deferred — current duplication is intentionally safe; comment-only improvement)
**Severity:** nit
**Location:** ar.go:27-30, :56-59
**Description:** Two separate map[string]string literals with IDENTICAL key-value contents (`ARCADIA_ROOT_DISTBUILD`, `DYLD_LIBRARY_PATH`). Sharing one map would be a future bug if either gets mutated downstream. Duplication isn't documented as deliberate; future "DRY it up" maintainer might re-introduce shared-map bug.
**Fix:** Deferred. Optional one-line comment above the first literal could note "Built as separate literals (not a shared variable) so downstream mutation of one map can't leak into the other." No behavioral change.

### [PR-03-D03] Test name `TestCmdInspect_UnknownFlag_PanicsWithSingleErrorMessage` overstates what the test asserts
**Status:** resolved (deferred — nit-tier; honest behaviour pinned manually in verification transcript)
**Severity:** nit
**Location:** gjson_test.go:208-220
**Description:** Test name + comment promise a "single error message" / "no duplicate output" guarantee, but the body only asserts `strings.Contains(exc.Error(), "flag provided but not defined")`. The exception payload contains the message exactly once REGARDLESS of whether stderr was double-written; the duplicate (D02) manifests on stderr. So the test would still pass if D02 regressed (e.g. `SetOutput(io.Discard)` removed). The actual single-message guarantee is verified only by the manual `./yatool inspect -bogus 2>&1` probe in the verification transcript, not by automated test.
**Fix:** Deferred. Two paths to harden: (a) rename the test to honestly describe what it checks (`TestCmdInspect_UnknownFlag_ThrowsExceptionContainingFlagError`) and trim the "no duplicate" language, or (b) shell out to the built binary with `os/exec`, capture combined output, assert `strings.Count(out, "flag provided but not defined") == 1`. Path (b) is the correct hardening but adds a build-step dependency to test runs. Both deferred to a future test-infrastructure PR. Constraint logged: D02's regression-guard is currently manual.

### [PR-08-D04] D02 fix comment lines exceed repo's ~78-char wrap width
**Status:** resolved (deferred — cosmetic; reflow when next touching cc.go)
**Severity:** nit
**Location:** cc.go:82-85
**Description:** The 4-line D02 comment wraps at 87/83/82 chars; every other comment in cc.go and across the project wraps at <=78. Visually inconsistent inside the same comment block (the pre-existing "carries the same env map" comment above wraps at ~64 chars). Mechanically harmless.
**Fix:** Deferred. Re-wrap to ~72 chars when next editing cc.go (e.g. when D03's no-libc gating lands in M2). One-time mechanical reflow.

---

## PR-04

### [PR-04-D01] blank-line-after-`{` violations across compare_topology.go (recurrence of PR-11 D01-D08 pattern)
**Status:** resolved
**Severity:** minor
**Location:** compare_topology.go:89-90, 183-184, 189-190, 212-213, 246-247, 271-272
**Description:** STYLE.md exception "no blank line if block is first statement inside `{}`" violated in 6 spots: `for {` <blank> `if/switch/comment` followed by code. PR-11 D01-D08 spent eight defect IDs eradicating exactly this pattern; reintroducing is regression of established convention.
**Suggested fix:** Drop the blank line immediately after each `{` in the six locations. Re-run `gofmt -w` + verify.

### [PR-04-D02] Dead defensive `if maxLevel >= 0` after a negative-level reject above
**Status:** resolved
**Severity:** nit
**Location:** compare.go:56-66
**Description:** L56-58 throws when `maxLevel < 0`, so L62's `if maxLevel >= 0` guard always evaluates true. Dead defensive code; future reader may assume L0 might be skipped under some maxLevel value (it cannot).
**Suggested fix:** Drop `if maxLevel >= 0 { ... }` wrapper, call `compareTopology(want, got)` unconditionally.

### [PR-04-D03] Renumbered-UID test passes vacuously when `rename` is the identity
**Status:** resolved
**Severity:** minor
**Location:** compare_topology_test.go:43-82
**Description:** TestCompareL0_RenumberedUIDsStillMatch is the only test pinning UID-rename invariance (the comparator's headline property). Builds two identical 3-node graphs, renames every UID/SelfUID/Deps/Result via `rename(uid) = "X-" + uid`, asserts L0==1.0. But never asserts UIDs ACTUALLY differ between want and got after rename — if a future refactor turned `rename` into identity (or short-circuited), test would still pass for wrong reason. Real-graph self-match cannot catch this because both want/got are the same `*Graph` pointer.
**Suggested fix:** Add setup-sanity assertion before `Compare`: `if want.Graph[0].UID == got.Graph[0].UID { t.Fatalf("test setup broken: rename did not change UIDs") }`. Optionally also assert `got.Graph[0].UID == "X-"+want.Graph[0].UID`.

### [PR-04-D04] Nil-graph diagnostic prints inverted booleans (`want=false` for nil)
**Status:** resolved
**Severity:** nit
**Location:** compare.go:52-54
**Description:** `ThrowFmt("compare: nil graph (want=%v got=%v)", want != nil, got != nil)` prints `want=false got=true` when want==nil, got!=nil. Reader has to mentally invert. Confusing diagnostic precisely when someone is debugging.
**Suggested fix:** Replace with two single-fact throws: `if want == nil { ThrowFmt("compare: want graph is nil") }; if got == nil { ThrowFmt("compare: got graph is nil") }`.

### [PR-04-D05] Missing blank line before `for` populating byUID — inconsistent with project convention
**Status:** resolved
**Severity:** nit
**Location:** compare_topology.go:88-89
**Description:** `byUID := make(...)` directly followed by `for _, node := range g.Graph {` without blank. STYLE.md mandates blank before `for` (not first/last in `{}`). "Logical grouping" exception covers consecutive one-liners; a `for` loop is not a one-liner. Other sites in same project (compare.go:131-133, main.go:195-196) DO have the blank — local inconsistency.
**Suggested fix:** Insert blank line between L88 (`byUID := ...`) and L89 (`for _, node := range g.Graph {`).

### [PR-04-D06] Blank line between `{` and nested `for` in renumbered-UID test
**Status:** resolved (deferred — same pattern as D01, single blank line; sweep in next compare-test edit)
**Severity:** nit
**Location:** compare_topology_test.go:65-67
**Description:** Inside outer `for _, n := range got.Graph` loop, `for k, vals := range n.ForeignDeps {` is followed by blank line then nested `for i, d := range vals {`. Same class as D01 but in test file (D01 swept production only). One missed line.
**Fix:** Deferred. Trivial one-line deletion when next editing compare_topology_test.go (e.g. PR-05's L1+L2 work will likely touch the file).

---

## PR-05

### [PR-05-D01] `indexByUID` docstring claims throw on duplicate UIDs but body silently overwrites
**Status:** resolved
**Severity:** minor
**Location:** compare_props.go:225-235
**Description:** Doc says "Throws on duplicate UIDs (same defensive posture as compare_topology.go's byUID build)" but body has no duplicate check — `out[n.UID] = n` overwrites silently. In production unreachable (compareTopology checks first), but bypassable through any direct call (future test, debugger, --strict mode). PR-04 set defensive duplicate-check as project standard.
**Suggested fix:** Add the throw to match docstring: `if existing, dup := out[n.UID]; dup { ThrowFmt("compare: graph has duplicate UID %q", n.UID) }`. (Preferred over docstring weakening.)

### [PR-05-D02] `--level` flag usage string lies — still says "PR-04 implements L0 only" after PR-05 added L1+L2
**Status:** resolved
**Severity:** minor
**Location:** main.go:107
**Description:** `level := fs.Int("level", 3, "highest comparator level to run (0=topology; PR-04 implements L0 only)")` — parenthetical is stale. PR-05 implemented L1+L2; user sees this in `compare --help`. The longer `printCompareUsage` block IS up-to-date — only the short flag-default text drifted.
**Suggested fix:** Replace parenthetical with `"highest comparator level to run (0=topology, 1=props/outputs, 2=inputs/tags/reqs; 3+ reserved for PR-06)"`.

### [PR-05-D03] L1Note/L2Note phrasing "X of Y pairs match" misrepresents Y when graphs have unpaired nodes
**Status:** resolved
**Severity:** minor
**Location:** compare_props.go:151, 178
**Description:** Note format: `"%d of %d pairs match"` where `denom = max(len(want.Graph), len(got.Graph))` (total node count, NOT pair count). For PR-05 self-match (3730/3730) wording incidentally correct; latent bug triggers when graphs actually differ (PR-10's vertical slice would print "L3=100%-of-2-nodes" — exact case). Reader can't distinguish "230 mismatched pairs" from "100 mismatched + 130 unpaired".
**Suggested fix:** Combine with D04 — thread `wantOnly`/`gotOnly` from `pairByOutput` into `CompareReport`, then format note as `"%d matched / %d pairs / %d unpaired-want / %d unpaired-got"` or similar. Make the denominator's meaning explicit.

### [PR-05-D04] `wantOnly`/`gotOnly` returned by `pairByOutput` are computed and discarded
**Status:** resolved
**Severity:** minor
**Location:** compare.go:86 (caller), compare_props.go:57-97 (callee)
**Description:** Caller `pairs, _, _ := pairByOutput(want, got)` discards both unpaired lists. Callee spends two append loops + two sorts to produce them — pure waste. Test `TestPairByOutput_UnmatchedReportedSeparately` exercises an API nothing else consumes.
**Suggested fix:** Thread `wantOnly`/`gotOnly` into `CompareReport` (new fields `WantOnly []string`, `GotOnly []string` or counts). cmdCompare can include them in output. Fixes D03 in same stroke.

### [PR-05-D05] Comment claims inputs alphabetically ordered in g.json — false (7 nodes break it)
**Status:** resolved
**Severity:** nit
**Location:** compare_props.go:206-208
**Description:** `l2Match` docstring: "Inputs are alphabetically ordered in g.json and tags are preserved in declaration order." Empirical: 7 of 3730 nodes have inputs in non-alphabetical order (e.g. nodes mixing `$(BUILD_ROOT)/...` and `$(SOURCE_ROOT)/...` paths with `vcs_info.py` slotted mid-list). Outputs and tags ARE alphabetical. Order-sensitive comparison still correct, but rationale stated wrong — future PR-08+/PR-10 emitter author will trust docstring instead of inspecting data.
**Suggested fix:** Replace with: `"Inputs and tags are emitted in a ymake-deterministic order (mostly alphabetical for inputs, with some ymake-internal grouping; tags follow declaration order). Order-sensitive comparison is correct because the yatool emitter must reproduce that same order — a permutation indicates emitter drift, not comparator noise."`

### [PR-05-D06] `compareL1` docstring claims note summarises "unpaired nodes on either side" but note doesn't
**Status:** resolved
**Severity:** nit
**Location:** compare_props.go:128-129
**Description:** Doc says "Returns the percentage and a one-line note summarising matched pairs and unpaired nodes on either side." Actual: only `"%d of %d pairs match"`. Doc lies. Same issue inherited by `compareL2`.
**Suggested fix:** Same edit as D03/D04 — actually include unpaired counts in note; docstring becomes accurate.

### [PR-05-D07] CompareReport.L1Note inline-comment example shows pre-D03 format
**Status:** resolved (deferred — pure doc drift; fix when next editing compare.go)
**Severity:** nit
**Location:** compare.go:43
**Description:** L1Note field comment reads `// human-readable summary, e.g. "3729 of 3730 pairs match"` — pre-D03 format. Misleads reader inspecting struct definition. L2Note line 45 just says "human-readable summary" so it's accurate.
**Fix:** Deferred. Replace example with `// human-readable summary, e.g. "3729 matched / 3730 pairs / 0 unpaired-want / 1 unpaired-got"`. Sweep when next editing compare.go (likely PR-06's L3 addition).

### [PR-05-D08] L1Note/L2Note format-pinning tests weakened from "2 of 2" to bare "matched"
**Status:** resolved (deferred — nit-tier honesty regression; fix when next editing compare_props_test.go)
**Severity:** nit
**Location:** compare_props_test.go:130-136
**Description:** Round-1 pinned `strings.Contains(r.L1Note, "2 of 2")` (numerator + denominator). Round-2 D03/D04/D06 fix swapped to `strings.Contains(r.L1Note, "matched")` — only checks the literal word. Regression to `"matched"` with no digits or `"0 matched / 0 pairs / ..."` after counter regression would still pass. Defensible-but-loose substring.
**Fix:** Deferred. Replace with `strings.Contains(r.L1Note, "2 matched")` + `strings.Contains(r.L2Note, "2 matched")` — restores count-pinning, still stable across denom-formula changes.

---

## PR-06

### [PR-06-D01] L3 test for per-cmd Env equality is missing
**Status:** resolved
**Severity:** minor
**Location:** compare_cmd_test.go:75-102
**Description:** `l3Match` checks 3 things: (a) per-cmd CmdArgs slice-equal, (b) per-cmd Env map-equal, (c) top-level node Env map-equal. Tests cover (a) and (c). Branch (b) — `reflect.DeepEqual(want.Cmds[i].Env, got.Cmds[i].Env)` — has no isolating test. Future refactor accidentally dropping per-cmd Env check would not be caught: real-graph self-match symmetric, no synthetic test isolates this branch.
**Suggested fix:** Add `TestCompareL3_DifferentCmdEnvDropsBelow1` mirroring TestCompareL3_DifferentEnvDropsBelow1 but mutating `got.Graph[0].Cmds[0].Env["X"] = "y"` instead of top-level Env. Same L0/L1/L2 == 1.0, L3 < 1.0 assertions.

### [PR-06-D02] cloneNode in compare_props_test.go shallow-copies Cmd internals — CmdArgs slice header reuse + Env map aliased
**Status:** resolved
**Severity:** minor
**Location:** compare_props_test.go:84-103 (cloneNode), specifically L87 `cp.Cmds = append([]Cmd{}, n.Cmds...)`
**Description:** `cloneNode` deep-copies Node's slices/maps but `Cmds` gets `append([]Cmd{}, n.Cmds...)` — only allocates new backing array of `Cmd` values. Each `Cmd` value still contains `CmdArgs []string` (slice header → original backing array) and `Env map[string]string` (reference → original map). Current PR-06 tests sidestep by reassigning Cmds wholesale. But D01's natural fix (mutate `got.Graph[0].Cmds[0].Env["X"] = "y"` in place) would trigger the alias and silently mutate `want` too — false positive, hides the bug L3 was supposed to detect.
**Suggested fix:** In cloneNode after `cp.Cmds = append(...)`, deep-copy each element's CmdArgs and Env: `for i := range cp.Cmds { cp.Cmds[i].CmdArgs = append([]string{}, n.Cmds[i].CmdArgs...); cp.Cmds[i].Env = copyStringMap(n.Cmds[i].Env) }`. WHY-comment: "Cmd contains slice + map reference fields; shallow copy would alias them across want/got and silently mask L3 mismatches."

### [PR-06-D03] L3 nil-vs-empty Env map false-negative risk vs. emitter output (forward-looking, blocks PR-10)
**Status:** resolved
**Severity:** minor
**Location:** compare_cmd.go:80, :85
**Description:** `reflect.DeepEqual(nil, map[string]string{})` returns FALSE. PR-06 real-graph self-match (L3 = 100.00%) doesn't exercise this — both sides go through identical `json.Unmarshal`. But L3's purpose is comparing JSON-decoded reference vs emitter-built graph (PR-10 vertical slice). Today's emitters (cc.go, ar.go) populate Env with non-nil literals so safe. But: any future emitter setting `Env: nil` for a no-env cmd would silently fail L3 against `"env": {}` decoded as `map[string]string{}`. Comparator would report "every cmd's env differs", obscuring real diff. CmdArgs already safe via `stringSliceEqual` helper.
**Suggested fix:** Add `stringMapEqual(a, b map[string]string) bool` in compare_cmd.go treating nil and empty as equal: `if len(a) != len(b) { return false }; for k, v := range a { if b[k] != v { return false } }; return true`. Use in `l3Match` for both Env checks (per-cmd at L80, top-level at L85). Discipline parallel to `stringSliceEqual`. Add a simple unit test: `stringMapEqual(nil, map[string]string{}) == true`.

### [PR-06-D04] stringMapEqual treats two same-size maps with different keys as equal when differing value is empty string (REGRESSION from D03 fix)
**Status:** resolved
**Severity:** minor
**Location:** compare_cmd.go:98-110
**Description:** Round-2 D03 fix replaced `reflect.DeepEqual` with `stringMapEqual` for nil/empty handling. New helper uses `b[k] != v`, which returns zero value `""` for keys not present in `b`. When `len(a) == len(b)` but with different keys whose values are empty strings, `"" != ""` evaluates false → function wrongly returns true. Probe: `stringMapEqual({"x": ""}, {"y": ""})` → true (should be false). `reflect.DeepEqual` did NOT have this bug. Real-graph self-match 100% doesn't catch it (symmetric). Latent regression that would mask Env diffs in PR-10's emitter-vs-reference comparison.
**Suggested fix:** Use two-value map index: `for k, v := range a { if v2, ok := b[k]; !ok || v2 != v { return false } }`. Add test case to TestStringMapEqual_NilVsEmpty (or rename): `if stringMapEqual({"x": ""}, {"y": ""}) { t.Error("...") }`.

### [PR-06-D05] Missing blank line before `// ...` comment + `for` block in cloneNode
**Status:** resolved
**Severity:** nit
**Location:** compare_props_test.go:92-96
**Description:** D02 fix added comment + for-loop directly after `cp.Cmds = append(...)` with no blank line. STYLE.md mandates blank before `for`.
**Suggested fix:** Insert blank line between `cp.Cmds = append(...)` and the `// Cmd contains slice + map reference fields...` comment.

---

## PR-10

### [PR-10-D01] printGenUsage Usage line has double-space before `[--source-root`
**Status:** resolved
**Severity:** nit
**Location:** main.go:132
**Description:** First line of `printGenUsage`: `Usage: yatool gen --target <module-dir> --out <path|->  [--source-root <path>]` — two spaces between `>` and `[`.
**Suggested fix:** Collapse to single space.

### [PR-10-D02] cmdGen has no automated tests for flag handling
**Status:** resolved
**Severity:** minor
**Location:** main.go:98-129; companion test slot expected as new tests in gen_test.go or main_test.go
**Description:** TestCmdInspect_HelpFlag/UnknownFlag exist; no equivalent TestCmdGen_*. Future regression that re-introduces ExitOnError or drops SetOutput(io.Discard) would silently slip past CI. Smoke checks confirm runtime behavior but only as long as someone re-runs them.
**Suggested fix:** Add `TestCmdGen_HelpFlag_PrintsUsageAndExits0`, `TestCmdGen_UnknownFlag_PanicsWithSingleErrorMessage`, `TestCmdGen_MissingTargetThrows`, `TestCmdGen_MissingOutThrows`. Mirror the cmdInspect test shapes.

### [PR-10-D03] CC output-path formula duplicated between cc.go:50 and gen.go:89 (REFACTOR)
**Status:** resolved (deferred to early M2 — scope-creep into PR-08 territory)
**Severity:** minor
**Location:** cc.go:50, gen.go:89
**Description:** Both compose `"$(BUILD_ROOT)/" + moduleDir + "/" + filepath.Base(srcRel) + ".o"`. Documented "MUST stay in sync" comment in gen.go is a known anti-pattern.
**Fix:** Deferred. Refactor EmitCC's signature to `(NodeRef, string)` returning `(emit.Emit(node), outputPath)`, update gen.go to consume both. PR-10's fix-subagent scope explicitly excludes cc.go modifications; the refactor lands in an early-M2 cleanup PR. Constraint logged for whoever picks it up: EmitCC has exactly one caller (gen.go) and one direct test (cc_test.go) — refactor is small.

### [PR-10-D04] writeGraph drops f.Close() error
**Status:** resolved
**Severity:** nit
**Location:** main.go:154
**Description:** `defer f.Close()` ignores error. JSON encoder doesn't buffer, but Close errors on NFS / FUSE / full-disk-flushed-at-close are still possible. Project idiom otherwise uses Throw for IO.
**Suggested fix:** Replace with `defer func() { Throw(f.Close()) }()` so Close error propagates to outer Catch.

### [PR-10-D05] Diagnostics ordering: empty-SRCS check fires before PEERDIR-present check
**Status:** resolved
**Severity:** nit
**Location:** gen.go:72-78
**Description:** For hypothetical PEERDIR-only LIBRARY (rare), user gets "PR-10 requires at least one source" instead of more accurate "PR-10 does not support PEERDIR yet". Real-world impact low; no PEERDIR-only LIBRARYs found in first 200 ya.make scanned.
**Suggested fix:** Swap order — PEERDIR-present check before SRCS-empty check. Clearer rejection message for the rare case.

### [PR-10-D06] sourceRoot constant in gen_test.go duplicates referenceGraphPath's prefix
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:19, gjson_test.go:13
**Description:** `gen_test.go:19` hardcodes `const sourceRoot = "/home/pg/monorepo/yatool_orig"`; `gjson_test.go:13` defines `const referenceGraphPath = "/home/pg/monorepo/yatool_orig/g.json"`. Same path, manual edit. Future move of the snapshot would miss one.
**Suggested fix:** `sourceRoot := filepath.Dir(referenceGraphPath)` instead of hardcoding.

---

## PR-12

### [PR-12-D01] EmitCC's path formula uses filepath.Base(srcRel), stripping subdir layout that real ymake preserves with `_/` infix
**Status:** resolved
**Severity:** minor
**Location:** cc.go:23, cc.go:40, cc.go:51
**Description:** Reference graph has 3071 of 3571 CC nodes with subdir SRCS (e.g. `module_dir = "contrib/libs/cxxsupp/libcxx"`, `srcRel = "src/algorithm.cpp"` → `$(BUILD_ROOT)/contrib/libs/cxxsupp/libcxx/_/src/algorithm.cpp.o`). PR-10's formula `$(BUILD_ROOT)/<moduleDir>/<basename(srcRel)>.o` strips both the `_/` infix AND the subdir component. PR-12 inherits and enshrines as contract. M1 lib.c (no subdir) is the only test today — Wave 2 will land first multi-source module and silently break.
**Suggested fix:** Change EmitCC formula to: if `srcRel` contains `/`, use `$(BUILD_ROOT)/<moduleDir>/_/<srcRel>.o`; else `$(BUILD_ROOT)/<moduleDir>/<srcRel>.o`. Verify against reference for both flat and nested cases. Update docstrings.

### [PR-12-D02] genCtx.memo keyed by raw targetDir without normalization
**Status:** resolved
**Severity:** minor
**Location:** gen.go:77, :162, :173, :227
**Description:** `ctx.memo[targetDir]` and `ctx.walking[targetDir]` use caller-provided string verbatim. `filepath.Join` normalizes `./thelib` → `thelib` for path resolution, but memo key doesn't — two PEERDIR entries `./thelib` and `thelib` would emit twice. Cycle detector also misses self-cycles via `PEERDIR(./a)` from `a/ya.make`.
**Suggested fix:** `targetDir = filepath.Clean(targetDir)` at top of genModule, before memo lookup. Reject empty/leading-`./`/trailing-`/` explicitly OR rely on Clean to canonicalize.

### [PR-12-D03] Multi-module ya.make rejection and zero-module rejection lack test coverage
**Status:** resolved
**Severity:** nit
**Location:** gen.go:186, :210; gen_test.go (no test)
**Description:** Throws `gen: %s declares multiple modules` and `gen: %s has no module declaration` are concrete failure modes with no test coverage. Future refactor could silently drop them.
**Suggested fix:** Add `TestGen_RejectsMultipleModules` (synthetic ya.make with `LIBRARY()...PROGRAM()...END()`) and `TestGen_RejectsZeroModule` (only `SET(X y)` + `END`).

### [PR-12-D04] describeStmt is dead code
**Status:** resolved
**Severity:** nit
**Location:** gen.go:204-205, :271-294
**Description:** Switch in genModule has explicit cases for every concrete Stmt type. `default:` arm calls `describeStmt(s)` which is itself a switch on the same types with `<unknown-stmt>` fallback unreachable. 24 lines of code-and-comment defensive against impossible scenarios. CLAUDE.md "no error handling for impossible scenarios."
**Suggested fix:** Delete describeStmt entirely. Replace `default:` arm with `ThrowFmt("gen: PR-12: unhandled Stmt type %T", s)`.

### [PR-12-D05] AR node for parent-with-peers leaks peer archives into both inputs and cmd_args
**Status:** resolved (deferred to PR-15 — peer-paths-in-AR-cmd_args is structurally wrong but harmless for UID derivation; PR-15 reworks ar.go signature with explicit peerLibs separation)
**Severity:** minor
**Location:** gen.go:248-253; ar.go:43-58
**Description:** When a module has PEERDIRs, gen.go threads peerArchivePaths through `arDepPaths` into EmitAR. EmitAR appends them verbatim to cmd_args (after archive output) AND inputs. Real ymake AR doesn't recursively bundle other `.a`s — peer archives are LD inputs, not AR inputs. Synthetic test only checks Deps presence (not cmd_args/inputs shape), so structural error is undetected.
**Fix:** Deferred to PR-15. PR-15 introduces real archive-naming convention + multi-source sort + GLOBAL_SRCS support — natural place to refactor EmitAR signature with separate `peerLibs []NodeRef` parameter that flows into DepRefs only (NOT cmd_args/inputs). Until then, peer archives live in DepRefs (correct for UID) AND ar.cmd_args/inputs (incorrect but harmless). Constraint logged for PR-15: refactor EmitAR signature; current ar_test.go's M1 byte-exact test still pins lib.c case so regression is detectable.

### [PR-12-D06] Synthetic test asserts only deps presence, not declaration-order R14 invariant
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:191-207
**Description:** rootAR.Deps stuffed into a `depSet`, only checks set-membership. Finalize sorts Deps alphabetically (D14) so set-membership is the only thing that survives. Test cannot regress on R14 (declaration-order peer visit) because there's only ONE peerdir in synthetic. A `sort.Strings(peerdirs)` regression in genModule would slip past.
**Suggested fix:** Add a third synthetic module so mainprog peers `[zlib, alib]` in non-alphabetical declaration order. Assert AR nodes' module_dir order in g.Graph follows declaration order (zlib AR before alib AR).

### [PR-12-D07] STYLE.md violation: missing blank line before `if` in new EmitCC output-path tests
**Status:** resolved
**Severity:** nit
**Location:** cc_test.go:160-161, :169-170
**Description:** STYLE.md "Blank lines around control blocks" requires blank before `if` (exception: first stmt). Both new tests have `want := "..."` directly followed by `if outPath != want { ... }` with no blank. Same anti-pattern as PR-04-D01/D06.
**Suggested fix:** Insert blank line between `want := ...` and `if outPath != want {` in both `TestEmitCC_OutputPath_NestedSrc` and `TestEmitCC_OutputPath_FlatSrc`.

### [PR-12-D08] STYLE.md violation: missing blank line before `for` in TestGen_PeerdirDeclarationOrder_Preserved
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:401-402
**Description:** `var zlibIdx, alibIdx int = -1, -1` directly followed by `for i, n := range g.Graph { ... }` — no blank.
**Suggested fix:** Insert blank line between L401 and L402.

### [PR-12-D09] D06 fix asserts node count, not declaration order — R14 regression still slips past
**Status:** resolved (mitigated; full R14 pin requires exposing BufferedEmitter pre-Finalize or recording emit-order on Node — defer to future PR)
**Severity:** nit
**Location:** gen_test.go:373-430
**Description:** D06 said "Assert AR nodes' module_dir order in g.Graph follows declaration order." Implemented test only checks both AR nodes exist + `len(g.Graph) == 6`. Test's NOTE comment acknowledges Finalize's UID-tie-break topo sort makes strict order fragile. `sort.Strings(peerdirs)` regression in genModule would still emit 6 nodes and pass.
**Fix:** Mitigated. Count-check still catches "drops a peerdir entirely" or "fails to walk" regressions. Strict R14 pin requires either (a) exposing BufferedEmitter pre-Finalize via Gen returning a tuple, or (b) recording an emit-sequence field on Node — both architectural changes. Deferred to a future test-infra PR. Mirror of D05 deferral pattern.

---

## PR-13

### [PR-13-D01] INCLUDE cycle causes unbounded recursion / stack overflow
**Status:** resolved
**Severity:** major
**Location:** yamake.go:1044-1077 (expandInclude)
**Description:** No visited-set tracking. Self-referential `INCLUDE(a.inc)` in `a.inc`, or any cycle, causes infinite recursion through ParseFile → Parse → parseInternal → parseStmts → parseMacroInto → expandInclude → ParseFile, exhausting goroutine stack. Reproduced: probe writing `a.inc` containing `INCLUDE(a.inc)` doesn't terminate within 4s.
**Suggested fix:** Thread `includeStack map[string]bool` through parser. Compute absolute path of include target, check membership, throw `*ParseError` pinned at INCLUDE site with "INCLUDE cycle: a → b → a" diagnostic. Add `TestParseInclude_RejectsCycle` covering self-cycle and a→b→a.

### [PR-13-D02] IncludeStmt type defined but never constructed (dead code)
**Status:** resolved (deferred — type retained for symmetry; PR-20 may construct if needed)
**Severity:** minor
**Location:** yamake.go:73-85, :141
**Description:** Type defined with stmtMarker but no production code constructs it. expandInclude returns `append(into, included.Stmts...)` without wrapping inclusion marker. Brief said "TYPE defined but DROPPED from result Stmts", which is what was implemented. Phantom type that next reviewer will re-investigate.
**Fix:** Deferred. Type retained per brief; if PR-20 needs it for source-tracking, wire then. Otherwise drop in future cleanup PR.

### [PR-13-D03] ADDINCL/CFLAGS/LDFLAGS/GLOBAL_SRCS accept zero arguments silently
**Status:** resolved (deferred — permissive parsing is consistent with parser philosophy; PR-20 will validate at semantic level)
**Severity:** minor
**Location:** yamake.go:764-771
**Description:** `ADDINCL()`, `CFLAGS()`, `LDFLAGS()`, `GLOBAL_SRCS()` parse cleanly into empty-slice typed Stmts. Asymmetric vs JOIN_SRCS/SRCDIR which throw on zero args.
**Fix:** Deferred. Parser stays permissive; PR-20's gen.go integration will validate semantically (e.g. zero-arg ADDINCL is no-op).

### [PR-13-D04] Lowercase identifier in IF cond accepted, defers to EvalCond runtime throw
**Status:** resolved (deferred — design consistency; isIdentShapedName is shared classifier across two contexts with different conventions)
**Severity:** nit
**Location:** yamake.go:1015-1025
**Description:** `IF (foo_lower)` parses successfully because `isIdentShapedName` accepts any letter/underscore start. Error surfaces at EvalCond runtime ("unknown IF identifier"). Convention is uppercase IF idents.
**Fix:** Deferred. isIdentShapedName intentionally permissive. EvalCond's hard error is the correct place to surface.

### [PR-13-D05] parseInternal discards parseStmts terminator with `_`
**Status:** resolved (deferred — minor refactor; cosmetic)
**Severity:** nit
**Location:** yamake.go:599
**Description:** `mf.Stmts, _ = p.parseStmts(termTopLevel)` discards endTok. STYLE.md "Explicit over implicit" calls out blank-discard for required values. parseStmts returns at EOF so endTok is unused at top level.
**Fix:** Deferred. Optional refactor: split into parseTopLevelStmts (single return) and parseIfBodyStmts (tuple).

### [PR-13-D06] Unbalanced extra `)` after IF cond produces misleading "expected macro name" error
**Status:** resolved (deferred — UX-only; real ya.make sources don't trip)
**Severity:** nit
**Location:** yamake.go:870-895
**Description:** `IF (FOO))\nENDIF()` — extra `)` becomes someone else's problem; surfaces as "expected macro name, got ')'".
**Fix:** Deferred. UX-only.

---

## PR-14

### [PR-14-D01] inferModuleFlavor prefix match misclassifies `contrib/libs/musl_extra` as musl flavor
**Status:** resolved
**Severity:** major
**Location:** cc.go:64
**Description:** Reference graph contains `contrib/libs/musl_extra` (110 args, no `-Wno-everything`, no `-D_musl_=1`, no `-nostdinc`). `strings.HasPrefix(targetDir, "contrib/libs/musl")` matches both `contrib/libs/musl` AND `contrib/libs/musl_extra`. PR-20 recursive walk will silently emit IsMusl bundle for musl_extra, producing byte-mismatch.
**Suggested fix:** Tighten predicate: `if targetDir == "contrib/libs/musl" || strings.HasPrefix(targetDir, "contrib/libs/musl/")`. Add regression test `TestInferModuleFlavor_MuslExtraIsNotMusl`.

### [PR-14-D02] doc comment "8 musl-specific -I paths" undercounts
**Status:** resolved (deferred — cosmetic doc count)
**Severity:** minor
**Location:** cc.go:101, flags.go:221
**Description:** Doc claims 8 musl-specific paths; actual is 6 musl-specific (10 total entries, 4 shared with ccIncludes).
**Fix:** Deferred. Cosmetic doc fix in next cc.go edit.

### [PR-14-D03] ModuleFlavor.IsCpp/NoPlatform/NoCompilerWarnings declared but not consulted
**Status:** resolved (deferred to PR-20 — PR-14 placeholder envelope; PR-20 wires them)
**Severity:** minor
**Location:** cc.go:35-38, :131-186
**Description:** Three of nine ModuleFlavor fields are dead-on-arrival. PR-20 is the consumer.
**Fix:** Deferred to PR-20. Field stubs retained; PR-20 wires `NoCompilerWarnings`/`NoPlatform`/`IsCpp` into bundle composition.

### [PR-14-D04] EmitCCFlavor silently produces non-byte-exact node for unknown moduleDirs
**Status:** resolved (deferred — known limitation; PR-20 supplies real flavor)
**Severity:** minor
**Location:** cc.go:73, :131
**Description:** `inferModuleFlavor("unknown/path")` returns `ModuleFlavor{}` (all-false). EmitCCFlavor emits 56-arg bundle matching no reference. Silent garbage.
**Fix:** Deferred. PR-20's macro-driven flavor inference replaces inferModuleFlavor; until then, `Gen` only calls EmitCC for build/cow/on (M1) so the unknown-path path is unreachable in practice.

### [PR-14-D05] Musl byte-exact test omits ref-side sanity checks present in M1 test
**Status:** resolved (deferred — extract `assertReferenceCCInvariants` helper in next cc_test edit)
**Severity:** nit
**Location:** cc_test.go:193-258 vs :89-171
**Description:** New musl test omits the three sanity-check blocks present in M1 test (host_platform/foreign_deps/deps).
**Fix:** Deferred. Extract `assertReferenceCCInvariants` shared helper.

---

## PR-15

### [PR-15-D01] cmd_args .o sort divergence: PR-15 sorts cmd_args[10:] but reference uses declaration order (16 of 48 AR nodes affected)
**Status:** resolved
**Severity:** major
**Location:** ar.go:55-77 (sortObjsLockstep), :103-104 (cmd_args composition); ar_test.go:408-416 (TestEmitAR_TcmallocGlobal_ByteExact masks divergence)
**Description:** sortObjsLockstep sorts both inputs AND cmd_args[10:]. Reference inputs ARE sorted (matches), but reference cmd_args[10:] is in DECLARATION order for 16 of 48 AR nodes (util, library/cpp/archive, contrib/libs/cxxsupp/*, contrib/libs/musl, tcmalloc/no_percpu_cache global). PR-22 byte-exact at L3 will silently fail for those modules. TestEmitAR_TcmallocGlobal_ByteExact compares cmd_args as sorted set — actively masks the divergence.
**Suggested fix:** Sort `inputs` only. Pass cmd_args .o paths through in caller-supplied (declaration) order. gen.go feeds in source declaration order which matches reference. Update tests: split TestEmitAR_ObjPathsSorted into TestEmitAR_InputsSorted (asserts inputs sorted) + TestEmitAR_CmdArgsPreservesDeclarationOrder (asserts cmd_args[10:] = caller order). Update TestEmitAR_TcmallocGlobal_ByteExact to compare cmd_args[10:] against refObjPaths directly (not as sorted set).

### [PR-15-D02] m2-plan.md R10 stale "24 reference AR outputs" — actual is 38
**Status:** resolved (orchestrator note; fix in m2-plan.md)
**Severity:** nit
**Location:** docs/drafts/20260507-0549-m2-plan.md:27
**Description:** Plan claims 24; actual python probe shows 38 unique non-global + 1 global = 39.
**Fix:** Orchestrator updates m2-plan.md to "38 unique non-global + 1 global; see ar_test.go TestArchiveName_AllReferenceAR".

### [PR-15-D03] globalArchiveName strips trailing ".a" without verifying suffix
**Status:** resolved (deferred — defensive guard nice-to-have)
**Severity:** nit
**Location:** ar.go:47-51
**Description:** Strips 2 chars assuming `.a` suffix. Today every ArchiveName branch returns `.a`. Future shared-lib rule could break silently.
**Fix:** Deferred. Add `if !strings.HasSuffix(base, ".a") { ThrowFmt(...) }` in next ar.go edit.

### [PR-15-D04] archivePathOf in gen.go duplicates EmitAR's internal archive-path construction
**Status:** resolved (deferred — extract ArchivePath helper in next ar.go edit)
**Severity:** nit
**Location:** gen.go:108-110, ar.go:214, :246
**Description:** Three sites construct identical `$(BUILD_ROOT)/<moduleDir>/<ArchiveName(...)>` prefix. Format-prefix change requires editing all three.
**Fix:** Deferred. Extract `ArchivePath(moduleDir) string` helper in ar.go.

### [PR-15-D05] EmitARGlobal exercised against only 1 reference (tcmalloc) — branches untested
**Status:** resolved (deferred — purely additive coverage; mechanical test addition)
**Severity:** nit
**Location:** ar_test.go:282-431
**Description:** Only one reference global archive exists. Other globalArchiveName branches (util, library/cpp/...) untested.
**Fix:** Deferred. Add TestGlobalArchiveName_AllBranches table-driven test in next ar_test edit.

### [PR-15-D06] Comment "fixed prefix of 9 elements" reads ambiguously vs 10-element literal
**Status:** resolved (deferred — cosmetic comment rewording)
**Severity:** nit
**Location:** ar.go:103-118
**Description:** Comment counts 9 + archive path; literal has 10 strings (9 fixed + archivePath embedded). Cognitive friction.
**Fix:** Deferred. Reword to "10 elements before .o section (indices 0-9)".

---

## PR-16

NO DEFECTS. Clean. Reviewer verified 94-arg byte-exact for chkstk.S, M1 regression preserved (2/2 at L1/L2/L3), STYLE compliant.

---

## PR-17

### [PR-17-D01] tasks.md D20/Q4 + m2-plan.md misidentify which UID belongs to ragel6 vs dangling external
**Status:** resolved (orchestrator-work; tasks.md D20 already corrected, m2-plan.md to be updated)
**Severity:** major
**Location:** tasks.md:75 D20, :90 Q4 (FIXED 2026-05-07); docs/drafts/20260507-0549-m2-plan.md:19, :37, :108 (still stale)
**Description:** `OsvsEu9xnOqLNXi3INZ9CQ` is the YASM LD UID (used by 25 AS nodes' foreign_deps.tool); `XO1d8CLk3qDKv0XQTlDKmQ` is the host ragel6 LD UID (used by R6 node, dangling-external from M2's target-only view). Plan had labels swapped; tasks.md D20 corrected post-PR-17 escalation; m2-plan.md still has stale labels.
**Fix:** Orchestrator updated tasks.md D20 to clarify UID ownership. m2-plan.md sections §3 R2, §4 D20, §7 Q4 receive same correction.

### [PR-17-D02] R6 stub strategy diverges from D20/Q4 plan (incompatible with Finalize's pre-populated ForeignDeps rejection)
**Status:** resolved
**Severity:** minor
**Location:** r6.go:53-67 (stub Node emission), :23 (unused ragel6HostUID const); emitter.go:115-122 (Finalize's no-pre-populated-ForeignDeps rule)
**Description:** D20/Q4 spec "Preserves L0/L1/L2 fingerprint pairing" requires R6 node post-Finalize foreign_deps.tool to carry `XO1d8CLk3qDKv0XQTlDKmQ` byte-exact. Implementation works around Finalize's no-pre-populated-ForeignDeps rule by emitting a stub Node. Post-Finalize the R6 node has `foreign_deps.tool = ["<merkle-of-stub>"]` ≠ reference. PR-20 wiring will surface as unpaired R6 + extra phantom node.
**Suggested fix:** Option 1 (chosen by orchestrator): RELAX Finalize's pre-populated-ForeignDeps rejection — allow rules to pre-populate `node.ForeignDeps` directly with literal UID strings (for stub-host scenarios). Keep the pre-populated `Deps` rejection (Deps must always go through DepRefs). Update emitter.go to drop the ForeignDeps-pre-populated check while keeping the Deps check. Rewrite EmitR6 to set `node.ForeignDeps = map[string][]string{"tool": {"XO1d8CLk3qDKv0XQTlDKmQ"}}` directly. Drop the stub Node emission. Update r6_test to assert reference UID byte-exact.

### [PR-17-D03] R6 stub Node has hardcoded "default-linux-x86_64" magic literal with no source-of-truth comment
**Status:** resolved (will-be-removed by D02 fix)
**Severity:** nit
**Location:** r6.go:59
**Description:** Stub Node carries `Platform: "default-linux-x86_64"` literal. If stub survives in PR-19/PR-20 output, becomes maintenance puzzle.
**Fix:** Resolved by D02 fix (stub Node emission removed entirely; ragel6HostUID const becomes the literal in foreign_deps).

### [PR-17-D04] r6_test.go asserts cardinality only, not resolved-UID-equality
**Status:** resolved (will-be-strengthened by D02 fix)
**Severity:** nit
**Location:** r6_test.go:144-164
**Description:** Test confirms cardinality (1 entry in foreign_deps.tool) but doesn't assert the reference UID byte-exact. Will pass under D02 divergence.
**Fix:** Resolved by D02 fix — test will assert `got.ForeignDeps["tool"][0] == "XO1d8CLk3qDKv0XQTlDKmQ"` directly.

---

## PR-18

NO DEFECTS. Clean. Panic guards correctly placed at top of Emit/Result; tests use defer recover with substring assertion; M1 regression preserved.

---

## PR-23 (PIVOT)

### [PR-23-D01] EmitAS signature deviates from D33 with `(includes []string, yasmLD NodeRef, hasYasm bool)` triple instead of `yasmLD NodeRef`
**Status:** resolved
**Severity:** minor
**Location:** as.go:48
**Description:** D33 specs `EmitAS(instance, srcRel, yasmLD, emit) (NodeRef, string)`. PR-23 ships extra `includes []string` (inherited from PR-16) AND `hasYasm bool` (sentinel because `NodeRef{id:0}` is ambiguous with first-emitted ref). Bool sentinel is a real concern — propagates to PR-25.
**Suggested fix:** Replace `yasmLD NodeRef, hasYasm bool` with `yasmLD *NodeRef` (nil = no yasm). Cleaner; readable at call sites. Update tests.

### [PR-23-D02] Stale doc reference to undefined identifier `noYasmRef`
**Status:** resolved
**Severity:** nit
**Location:** as.go:42
**Description:** Doc comment references `noYasmRef` symbol that doesn't exist — earlier draft. Misleads readers.
**Suggested fix:** Replace with `(NodeRef{}, false)` or after D01 fix, with `(nil)`.

### [PR-23-D03] FlagSet.Extra type changed from `[]string` to `string` but module.go header comment still says `[]string`
**Status:** resolved
**Severity:** nit
**Location:** module.go:25-29
**Description:** Header comment claims `Extra []string`. Actual field at line 86 is `string` (sort-joined). Field-level comment correct; header stale.
**Suggested fix:** Update header to "Its `Extra` field is a `\n`-joined sorted concatenation (string, not []string, because slice fields disqualify a struct from being a map key per D34)".

### [PR-23-D04] `peerArchivePaths` collected but never read in genModule
**Status:** resolved
**Severity:** nit
**Location:** gen.go:211, :222
**Description:** `peerArchivePaths` allocated and appended but never consumed (EmitAR signature only takes `peerArchiveRefs`). Dead code.
**Suggested fix:** Delete lines 211 and 222 outright. PR-24/PR-25 can re-add when LD wiring needs it.

### [PR-23-D05] OnReady doc lacks per-ref-channel note for M3 StreamingEmitter implementer
**Status:** resolved
**Severity:** nit
**Location:** emitter.go:50-54, :89-91
**Description:** Doc says BufferedEmitter no-op but doesn't tell M3 author that StreamingEmitter MUST switch to per-NodeRef channels.
**Suggested fix:** Add to interface comment: "BufferedEmitter returns one shared channel that closes at Finalize for any input ref. StreamingEmitter (M3) MUST close a per-ref channel as each node's deps resolve — the shared-channel shortcut is buffered-only."

---

## PR-24

### [PR-24-D01] AS emitter does not set Cwd despite reference graph carrying cwd: $(BUILD_ROOT) on 58/83 AS nodes
**Status:** resolved
**Severity:** minor
**Location:** as.go:96-102; as_test.go:119
**Description:** PR-24 added Cmd.Cwd field. Reference graph has `cwd: $(BUILD_ROOT)` on 58/83 AS nodes. AS emitter never sets it. Pre-PR-24 the divergence was hidden because Cwd field didn't exist. Now detectable; PR-25's wider reference comparison will fail at L3 unless fixed.
**Suggested fix:** Set `Cwd: "$(BUILD_ROOT)"` in EmitAS. Pin in `TestEmitAS_CxxsuppBuiltinsChkstk_ByteExact` via explicit `if got.Cmds[0].Cwd != ref.Cmds[0].Cwd` assertion.

### [PR-24-D02] PROGRAM-as-peer rejection diagnostic uncovered by tests
**Status:** resolved
**Severity:** minor
**Location:** gen.go:242-244
**Description:** Throw `gen: %s peers PROGRAM module %s; only LIBRARY peers are linkable (PR-24 limitation)` triggers when PROGRAM encountered as PEERDIR. No test exercises this branch.
**Suggested fix:** Add `TestGen_RejectsProgramAsPeer` modeled on `TestGen_PeerdirCycle_Throws`. Synthetic: `peerprog` is PROGRAM, `caller` is PROGRAM with PEERDIR(peerprog). Assert exception with substring "peers PROGRAM module".

### [PR-24-D03] EmitLD length-mismatch checks for peerLD/plugin/global slices have no test coverage
**Status:** resolved
**Severity:** minor
**Location:** ld.go:98-108; ld_test.go:387-405
**Description:** EmitLD has 4 length-mismatch throws; only ccRefs/ccPaths is tested. Other 3 (peerLDRefs/peerLibPaths, pluginRefs/pluginPaths, globalRefs/globalPaths) untested.
**Suggested fix:** Extend `TestEmitLD_LengthMismatchPanics` to drive all 4 mismatch cases via table-driven sub-tests.

### [PR-24-D04] Stale comment in TestGen_PeerdirDeclarationOrder_Preserved claims "3 CC + 3 AR"
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:602-606
**Description:** Comment + error message say "3 CC + 3 AR" but PROGRAM mainprog now closes with LD, not AR. Actual: 3 CC + 2 AR + 1 LD.
**Suggested fix:** Update comment and error message to reflect the new structure.

### [PR-24-D05] Local variable `cap` shadows builtin in composeLDCmdLinkExe
**Status:** resolved
**Severity:** nit
**Location:** ld.go:285-291
**Description:** `cap := 2 + 6 + ...` shadows `cap()` builtin. Future edit needing real `cap(...)` would silently use the int.
**Suggested fix:** Rename to `argCap` or `expectedCap`.

### [PR-24-D06] EmitLD plugin/global path-prefix convention asymmetric, not enforced
**Status:** resolved (deferred to PR-25 — convention documented; PR-25's wiring will exercise both paths and reveal mistakes)
**Severity:** nit
**Location:** ld.go:69-77 docstring; :282-333 composeLDCmdLinkExe; :375-407 composeLDInputs
**Description:** pluginPaths already $(BUILD_ROOT)/-prefixed; globalPaths BUILD_ROOT-relative. Asymmetric convention documented but not enforced.
**Fix:** Deferred. PR-25's macro evaluator will be the first real consumer; if convention proves error-prone, PR-25 normalizes inputs.

---

## PR-25

### [PR-25-D01] CXXFLAGS/CONLYFLAGS treated as no-op metadata silently drops compile flags
**Status:** resolved
**Severity:** minor
**Location:** gen.go:111-143 (whitelist), :270-287 (applyUnknownStmt)
**Description:** Whitelist treats CXXFLAGS/CONLYFLAGS as no-op. Per upstream ymake.core.conf they `SET_APPEND_WITH_GLOBAL(USER_CXXFLAGS/CONLYFLAGS ...)`. base64/avx2 etc. use these. Asymmetric vs CFLAGS (typed CFlagsStmt collected into moduleData.cFlags).
**Suggested fix:** In applyUnknownStmt, add explicit cases for CXXFLAGS → `d.cxxFlags = append(d.cxxFlags, v.Args...)` and CONLYFLAGS → `d.cOnlyFlags = append(...)`. Add fields to moduleData. Consumer is PR-26.

### [PR-25-D02] SRCDIR collected but never consumed by emitOneSource
**Status:** resolved
**Severity:** minor
**Location:** gen.go:189, :244-245, :552-596
**Description:** `*SrcDirStmt` collected into moduleData.srcDir but emitOneSource always passes srcRel verbatim. SRCDIR shifts source resolution base; today `inputPath` would be wrong for SRCDIR-using modules (none in current closure). Comment-less variable disuse reads as oversight.
**Suggested fix:** Add explicit comment in gen.go documenting SRCDIR as deferred-to-PR-26 (mirror existing ADDINCL/CFLAGS comments at :548-551). Pure documentation fix.

### [PR-25-D03] Peer LIBRARY's global archive (.global.a) dropped from PROGRAM's LD globalRefs/globalPaths
**Status:** resolved
**Severity:** minor
**Location:** gen.go:84-90 (moduleEmitResult), :469-476 (EmitLD call passes nil/nil), :495-497 (EmitARGlobal return discarded)
**Description:** When LIBRARY peer declares GLOBAL_SRCS, gen emits `.global.a` via EmitARGlobal but discards return. PROGRAM's EmitLD call hardcodes `nil, nil` for globalRefs/globalPaths. Reference tools/archiver LD wires tcmalloc's .global.a via `-Wl,--whole-archive`. PR-26 widening will surface this.
**Suggested fix:** Extend moduleEmitResult with `globalRef *NodeRef, globalPath string`. Capture from EmitARGlobal return. PROGRAM aggregates peer globals (transitive — global archives propagate through PEERDIR per ymake whole-archive convention) and passes to EmitLD.

### [PR-25-D04] TestGen_ToolsArchiver_DoesNotCrash silently passes regardless of outcome
**Status:** resolved
**Severity:** minor
**Location:** gen_test.go:1004-1020
**Description:** Test wraps Gen in Try; on non-nil exception calls only t.Logf. Passes whether Gen returns successfully OR throws. Today's actual behavior is rc=0, 50 nodes — should pin THAT.
**Suggested fix:** Replace t.Logf with t.Fatalf on exception. Add `if len(g.Graph) < 50 { t.Errorf(...) }` to pin minimum coverage. Optionally pin len(g.Result) > 0.

### [PR-25-D05] pr12SupportedUnknownMacros var name stale; PR-25 owns it now
**Status:** resolved
**Severity:** nit
**Location:** gen.go:105-143, :284
**Description:** Whitelist named after PR-12; PR-25 extended membership significantly. Future grep for "PR-25 macro whitelist" misses this.
**Suggested fix:** Rename to `whitelistedMetadataMacros` (or similar). Update doc comment + error message at line 284.

### [PR-25-D06] TestEmitLD_AcceptsHostPIC asserts only platform string; misses HostPlatform/Tags wiring
**Status:** resolved
**Severity:** nit
**Location:** ld_test.go:362-387
**Description:** Test verifies guard removal but doesn't assert `HostPlatform=true` or `Tags=["tool"]` from EmitLD. Future regression that drops those would slip.
**Suggested fix:** Add `if !got.HostPlatform { t.Errorf("host_platform = false, want true") }` and `if len(got.Tags) != 1 || got.Tags[0] != "tool" { ... }`.

### [PR-25-D07] R6-generated .cpp wired through EmitCC produces SOURCE_ROOT path for a BUILD_ROOT-located file
**Status:** resolved
**Severity:** minor
**Location:** gen.go:583-590; cc.go:59
**Description:** `.rl6` source is dispatched: EmitR6 produces `$(BUILD_ROOT)/<modulePath>/_/<srcRel>.cpp`. Then PR-25 strips prefix to `_/<srcRel>.cpp`, feeds to EmitCC which composes `inputPath=$(SOURCE_ROOT)/<modulePath>/_/<srcRel>.cpp` — wrong path (file lives in BUILD_ROOT). Mirror of documented JS deferral.
**Suggested fix:** Add explicit defer comment mirroring JS gap at gen.go:444-446. PR-26 lands `EmitCCFromBuildRoot` variant. Pure documentation for now.

### [PR-25-D08] Peer GlobalPath has $(BUILD_ROOT)/ prefix violating EmitLD contract — produces double-prefixed inputs and malformed cmd_args (REGRESSION from D03 fix)
**Status:** resolved
**Severity:** major
**Location:** gen.go:524; surfaces via composeLDInputs (ld.go:405) and composeLDCmdLinkExe (ld.go:329)
**Description:** D03 fix constructed `result.GlobalPath = "$(BUILD_ROOT)/" + instance.Path + "/" + globalArchiveName(instance.Path)`. EmitLD's globalPaths param is BUILD_ROOT-RELATIVE per ld.go:74-77 (same as peerLibPaths). Result: composeLDInputs prepends $(BUILD_ROOT)/ → double-prefix `$(BUILD_ROOT)/$(BUILD_ROOT)/peerlib/lib...global.a`. composeLDCmdLinkExe emits `$(BUILD_ROOT)/...global.a` literal between --ya-start/end-command-file. TestGen_PeerGlobalArchive_ThreadsToLD missed because only checks refs/counts, not path strings.
**Suggested fix:** `gen.go:524` change to `result.GlobalPath = instance.Path + "/" + globalArchiveName(instance.Path)` (NO $(BUILD_ROOT)/ prefix). Strengthen TestGen_PeerGlobalArchive_ThreadsToLD: assert inputs contains exactly `"$(BUILD_ROOT)/peerlib/libpeerlib.global.a"` (single prefix); assert cmd_args between --ya-start-command-file and --ya-end-command-file contains `"peerlib/libpeerlib.global.a"` (no prefix).

### [PR-25-D09] Stale `pr12SupportedUnknownMacros` reference in TestGen_RejectsUnsupportedMacro doc comment
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:330
**Description:** D05 renamed to whitelistedMetadataMacros in production code; test comment missed.
**Suggested fix:** s/pr12SupportedUnknownMacros/whitelistedMetadataMacros/ in the doc comment.

### [PR-25-D10] D07's R6 comment cites wrong line range for JS gap mirror
**Status:** resolved
**Severity:** nit
**Location:** gen.go:612
**Description:** Comment says "Mirror of the JS gap documented at gen.go:444-446" but actual JS-related EmitCC code lives at gen.go:456-468. Line refs drift between drafts.
**Suggested fix:** Either correct line number OR rewrite to "Mirror of the JS gap documented earlier in this function" to survive future drift.

### [PR-25-D11] peerGlobalRefs/peerGlobalPaths missing capacity hint inconsistent with peerArchive*
**Status:** resolved
**Severity:** nit
**Location:** gen.go:414-415
**Description:** peerArchiveRefs/Paths use `make([]X, 0, len(d.peerdirs))`. peerGlobal* use `make([]X, 0)`. Style inconsistency.
**Suggested fix:** Add capacity hint to match.

### [PR-25-D12] R6 comment misattributes EmitJS scope ("this function" — actually different function)
**Status:** resolved (deferred — one-word cosmetic; "search for EmitJS" hint still works file-wide)
**Severity:** nit
**Location:** gen.go:611-614
**Description:** D10 fix rewrote stale line numbers but picked wrong scope. R6 comment is in `emitOneSource`; EmitJS is in `genModule` — different function. "search for EmitJS" still finds it via file-wide grep.
**Fix:** Deferred. Replace "this function" with "this file" or "in genModule" in next gen.go edit.

---

## PR-26

### [PR-26-D01] musl self-guard `strings.HasPrefix(instance.Path, "contrib/libs/musl")` matches musl_extra
**Status:** resolved
**Severity:** minor
**Location:** gen.go:417
**Description:** Self-cycle guard uses HasPrefix without trailing slash. Matches musl, musl/full, AND musl_extra (false positive). PR-23 fixed same bug for `inferModuleFlavor` via `path == "contrib/libs/musl" || strings.HasPrefix(path, "contrib/libs/musl/")`. Latent today (musl_extra not in current closure).
**Suggested fix:** Use exact-match-or-trailing-slash convention. Add `musl_extra_not_self` test case.

### [PR-26-D02] 5 of 8 new DefaultIfEnv bindings speculative (not consulted by PR-26 walk)
**Status:** resolved
**Severity:** minor
**Location:** macros.go:68-70, 82, 84
**Description:** Reviewer empirically verified: removing OS_CYGWIN, CYGWIN, SUN, USE_STL_SYSTEM, FUZZING does NOT break tests (those modules — util, libcxx, libcxxrt — aren't walked). Violates "concrete observed gap" rule. Comments claim modules that PR-26 doesn't walk. The 3 needed (USE_EAT_MY_DATA, ARCH_ARM6, WITH_MAPKIT) confirmed.
**Suggested fix:** Remove the 5 speculative bindings. Re-add when actual walk encounters them (D27 throw is the signal).

### [PR-26-D03] Explicit-PEERDIR-of-default produces duplicate archive references in AR/LD
**Status:** resolved
**Severity:** minor
**Location:** gen.go:543-576
**Description:** Walker prepends defaults then iterates allPeers. Module that explicitly PEERDIR(musl) without NO_LIBC duplicates musl. Memo returns same NodeRef both times — peerArchiveRefs/Paths gets twice. AR/LD see duplicate. Latent (no current closure module hits).
**Suggested fix:** De-dup via seen map before walking. Add test: explicit PEERDIR of default → exactly one archive in peer list.

### [PR-26-D04] TestGen_DefaultPeerdirs_HelperSuppression missing no_util_only and musl_extra cases
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:1254-1352
**Description:** 9-case table covers most combinations but missing: (a) `no_util_only` — NoUtil alone suppresses nothing (per docstring); regression invisible. (b) `musl_extra_not_self` paired with D01.
**Suggested fix:** Add both cases.

### [PR-26-D05] peerYaMakeExists silently elides defaults on any os.Stat error (not just NotExist)
**Status:** resolved
**Severity:** nit
**Location:** gen.go:457-461, :558-560
**Description:** Returns false for any err. Conflates "synthetic test missing stub" with "production tree corrupted". Operator has no signal at gen time.
**Suggested fix:** Discriminate: only ENOENT silences; other errors throw via `errors.Is(err, fs.ErrNotExist)`.

### [PR-26-D06] effectiveNoPlatform docstring example misleading
**Status:** resolved
**Severity:** nit
**Location:** gen.go:437-442
**Description:** Comment says build/cow/on demos this pattern via "ya.make never types NO_PLATFORM but reference has zero peer deps". But build/cow/on gets the triple from inferFlagsFromPath HEURISTIC (module.go:161-165), NOT from ya.make declarations. Conflates path-heuristic source with macro-derived source.
**Suggested fix:** Reword to clarify the source is the path heuristic; macro-driven examples await a future closure module.

---

## PR-27

### [PR-27-D01] tools/archiver coverage 1696 falls short of brief's ≥2500-node target
**Status:** resolved (deferred to PR-28+; structural — dual-platform emission gap dominates)
**Severity:** major
**Location:** tasks.md:57 (PR-27 spec); gen.go:169-190 (Gen entry walks target only)
**Description:** PR-27 stated goal: bring coverage 1541 → ~3500+. Achieved: 1696 (delta +155, 4.2% of gap). Of 3730 reference nodes: walker emits 1696 target (vs ref 1933 target → 87.7% target coverage), zero host (vs ref 1797 host → 0%). 15 reference module_dirs entirely missing: asmlib, asmglibc, jemalloc, mimalloc, tcmalloc/{malloc_extension,no_percpu_cache}, abseil-cpp, ragel6 host, yasm host, malloc/{mimalloc,tcmalloc}, musl/full, musl/include, musl_extra. Dual-platform gap dominates (~48% of total ref nodes).
**Root cause:** Two distinct gaps. (a) Walker emits `instance.Target` only; no general dual-platform pass. (b) Allocator/asm libraries reached only via ALLOCATOR_IMPL/EXTRALIBS/RECURSE which PR-27 keeps as metadata-only.
**Fix:** Deferred to PR-28+ as dedicated dual-platform-emission PR. PR-27 closes against partially-met spec target with explicit shortfall logged; the +155 delta from libcxx/util walks is the in-scope contribution. ALLOCATOR_IMPL routing left to a separate gap-closer.

### [PR-27-D02] Cycle handler relaxed throw → silent stub-return without diagnostic
**Status:** resolved
**Severity:** major
**Location:** gen.go:611-613
**Description:** PR-26 raised on PEERDIR cycle; PR-27 silently returns `&moduleEmitResult{headerOnly: true}` with no log. Empirically tolerates exactly 1 cycle today (`util` re-entered via `zlib`'s default-peer set). Safe at L0–L3 (all 48 reference AR nodes have zero peer-archive inputs, verified). Risk: a future genuine cycle (not implicit-default-induced) gets silently skipped, producing wrong graph with no diagnostic.
**Root cause:** `zlib`-style modules with `NO_RUNTIME()` only (not `NO_UTIL`) get `util` as implicit default; `util` reaches `zlib` via its own closure. Runtime-ancestor exclusion at gen.go:471 covers `util` but not `util`'s descendants peering `zlib`.
**Fix:** Add stderr diagnostic in the cycle handler so a real cycle in production is visible. Counter on genCtx so test can assert "tolerated zero / N cycles". Keep the relaxation but make it observable.

### [PR-27-D03] Try-wrapped ragel6 recursion silently swallows ALL exceptions, not just parse-gap exceptions
**Status:** resolved
**Severity:** minor
**Location:** gen.go:958-967
**Description:** `Try(func() { ragelResult := genModule(ctx, ragelInstance); ragelLDRef = ragelResult.LDRef })` catches every exception type, not just *ParseError from the documented INCLUDE-substitution gap. Regression in host-instance walking for any other reason silently produces zero ragelLDRef and L3 byte-exact divergence on every .rl6 module instead of failing loudly.
**Root cause:** Coarse-grained Try catches the wrong shape.
**Fix:** Narrow the Try to *ParseError only via `errors.As`; re-throw any other exception type to preserve loud-fail discipline.

### [PR-27-D04] defaultPeerdirsFor per-path self-suppression checks are dead code given runtimeAncestorPaths early-exit
**Status:** resolved
**Severity:** nit
**Location:** gen.go:484, 490, 496, 506, 510, 514, 520
**Description:** Runtime-ancestor early-exit at gen.go:471 returns nil for any path in runtimeAncestorPaths, which already covers musl/cxxsupp/libunwind/malloc/api/util. Every per-path self-cycle guard below (e.g. `instance.Path != "contrib/libs/musl"`) is unreachable. Adds visual noise; risk of drift (line 510 has only `!=` not the prefix-match shape used at 484/506/520).
**Fix:** Add a compile-time invariant comment at gen.go:471 documenting that every path enumerated below must appear in `runtimeAncestorPaths` and the per-path checks are redundant defense-in-depth, OR remove the per-path checks. Defense-in-depth is the safer choice — keep checks, add the invariant comment.

### [PR-27-D05] DefaultIfEnv comment lists "SANITIZER" as a bool binding, but it is not present in the bools map
**Status:** resolved
**Severity:** nit
**Location:** macros.go:257
**Description:** Doc comment advertises `bool-typed FUZZING / EXPORT_CMAKE / NO_CXX_RTTI / NO_CXX_EXCEPTIONS / USE_ARCADIA_COMPILER_RUNTIME / SANITIZER / PROVIDE_*` but bools map at lines 261-303 contains no entry named `SANITIZER`. No closure ya.make currently uses `IF (SANITIZER)`, so functional impact is zero, but a future maintainer reading the comment would believe the binding exists.
**Fix:** Remove `SANITIZER /` from the comment list at macros.go:257. Closure does not currently need the binding; add it on demand if a real `IF (SANITIZER)` shows up.

### [PR-27-D06] Negative integer literals fail to lex as tokInt; degrade to tokWord and break int comparisons
**Status:** resolved (deferred — closure does not currently use `IF (X < -N)`; documented as ExprInt invariant)
**Severity:** nit
**Location:** yamake.go:512-513 (readToken digit dispatch), yamake.go:638-657 (readNumberOrWord), yamake.go:189-193 (ExprInt comment)
**Description:** `IF (X < -1)` lexes `-1` as `tokWord("-1")` because `-` is in `isWordByte`; `parseAtom` then throws "unexpected word "-1" in IF condition". Not exercised by current closure.
**Fix:** Document explicitly in the ExprInt comment at yamake.go:189-193 that integer literals are unsigned and negative integers are unsupported. Defer the lexer extension until a real closure ya.make needs it.

---

## PR-28

### [PR-28-D01] R6 cmd_args invokes a ragel6 binary at a path that does not match our own host LD output
**Status:** resolved
**Severity:** major
**Location:** r6.go:35; gen.go:1171,1177-1179; ld.go:119-120
**Description:** EmitR6 hardcodes `$(BUILD_ROOT)/contrib/tools/ragel6/ragel6` but our host LD output is `$(BUILD_ROOT)/contrib/tools/ragel6/bin/bin` (D03 path correction moved instance to /bin; EmitLD derives binary name from lastPathComponent, not PROGRAM(name)). A real builder consuming this graph would invoke a non-existent binary.
**Root cause:** Two compounding gaps. (a) D03 placed the host instance at /bin to dodge parent's INCLUDE; (b) EmitLD/EmitR6 don't honor PROGRAM(<name>) — binary name is path-derived.
**Suggested fix:** Thread `ModuleStmt.Args[0]` (program name) into EmitLD's binaryName composition, and have EmitR6 derive the ragel6 invocation path from the consumed host LD output (e.g. accept the path alongside the ref, or compose from `<host LD's outputs[0]>`). Add regression test: R6 cmd_args[0] == host ragel6 LD outputs[0].

### [PR-28-D02] Host ragel6 closure emits module_dir=contrib/tools/ragel6/bin instead of contrib/tools/ragel6 (10 nodes wrong)
**Status:** resolved
**Severity:** major
**Location:** gen.go:1171,1177-1179; gen.go:386-389 (collectStmts SRCDIR collection); gen.go emitOneSource (SRCDIR consumption gap)
**Description:** All 10 host ragel6 nodes (1 LD + 9 CC) emit module_dir=contrib/tools/ragel6/bin; reference uses contrib/tools/ragel6 because /bin/ya.make declares SRCDIR(contrib/tools/ragel6) which relocates SRCS to parent. Our walker collects srcDir into moduleData but never consumes it in emitOneSource. Comparator pairs by (module_dir, output) so the 10 nodes pair-fail.
**Root cause:** SRCDIR collected at gen.go:386-389 with explicit comment marking it a documented PR-26 deferral. PR-28 walks /bin (which uses SRCDIR) so the gap surfaces.
**Fix:** emitOneSource now takes `srcDir string` parameter; constructs `srcInstance` with rebased Path when srcDir != "". Plain SRCS (CC, AS, R6) rebase to <sourceRoot>/<srcDir>. JOIN_SRCS branch was missed in initial fix; closed by D11. LD/AR remain at instance.Path (semantic difference: binary lives where declared even if sources are elsewhere).

### [PR-28-D03] drainHostToolWalks is dead code; requiredHostTools machinery never fires
**Status:** resolved
**Severity:** minor
**Location:** gen.go:243,261-287,289-312; trigger sites at gen.go:1145,1173
**Description:** Reviewer commented out the drainHostToolWalks call at gen.go:243 and got a SHA-256 byte-identical 18,778,347-byte graph. Both trigger sites eagerly call genModule(...) synchronously to obtain the host LDRef they need to wire DepRefs/ForeignDepRefs. By the time the post-target-walk drain runs, memo already has every requested host instance and pendingHostTools returns empty. The "demand-driven" two-phase backbone is purely scaffolding.
**Root cause:** Plan implied two-phase model (record demand, post-walk drain). Executor wired both phases AND eager recursion (because trigger sites need LDRef value back inline). Once eager wiring landed, drain became unreachable.
**Fix:** Removed drainHostToolWalks, requiredHostTools, hostToolPrograms, pendingHostTools and related plumbing. Eager-recursion model documented at genCtx and Gen() doc comments.

### [PR-28-D04] DISABLE_INSTRUCTION_SETS=false binding is empirically dead with misleading comment
**Status:** resolved
**Severity:** minor
**Location:** macros.go:303
**Description:** Reviewer removed binding entirely and got SHA-256 byte-identical archiver.json. util/charset is the only consumer; on target axis ARCH_X86_64=false short-circuits the AND; on host axis util is gated off via PR-28's util-target-only gate. Comment "M2 default = enabled" implies binding fires under M2; it does not.
**Root cause:** Speculative addition during host-walk bring-up; gate landed later and made it unreachable but binding stayed.
**Fix:** Removed binding entirely. Throw-on-miss philosophy preserved; new vars surface via the unhandled-IF-throw signal.

### [PR-28-D05] ALLOCATOR(SYSTEM) silently drops unconditional library/cpp/malloc/system PEERDIR; comment incorrectly claims MUSL gating
**Status:** resolved
**Severity:** minor
**Location:** gen.go:476-478, 448-451
**Description:** Upstream `build/ymake.core.conf` lines 1038-1040: `when ($ALLOCATOR == "SYSTEM") { PEERDIR+=library/cpp/malloc/system }` — outside the select($ALLOCATOR) block, NOT MUSL-gated. Our table maps `"SYSTEM": nil` and the comment claims SYSTEM is "MUSL-gated, never fires under M2". Misreading. No M2 closure types ALLOCATOR(SYSTEM) so currently inert; non-M2 closure using SYSTEM would silently lose the peer.
**Fix:** `"SYSTEM": []string{"library/cpp/malloc/system"}` with comment citing ymake.core.conf:1038-1040 and clarifying that the MUSL gate at lines 954-958 applies to the select($ALLOCATOR) block, NOT to the SYSTEM when-clause.

### [PR-28-D06] yasm host-PROGRAM walk and asmlib host AS+yasm wiring are end-to-end dead in M2 closure
**Status:** resolved (deferred — wiring kept as forward-scaffolding for follow-up PR; closure path documented inline)
**Severity:** minor
**Location:** gen.go:1142-1154 (.S/asmlib trigger); gen_test.go:1280-1349 (TestGen_HostWalk_AsmlibYasmWired)
**Description:** Reference has 27 host asmlib nodes (25 AS) and 8 host yasm nodes; we emit 0 of each. ragel6/bin host walk doesn't transitively reach asmlib because we peer `contrib/libs/musl` not `contrib/libs/musl/full` (the only path that pulls asmlib via ARCH_X86_64-gated PEERDIR at musl/full/ya.make:17-23). The `.S` yasm-trigger code is exercised only by synthetic test, never by M2 closure.
**Root cause:** Our `defaultPeerdirsFor` doesn't implement upstream's `_BUILTIN_PEERDIR` rule "MUSL=yes && !MUSL_LITE → PEERDIR+=contrib/libs/musl/full" (ymake.core.conf:1238-1245).
**Fix:** Comment added at .S yasm-trigger site documenting the deferred musl/full closure path. Synthetic test + wiring remain as forward-scaffolding for the follow-up PR. PR-28 Completed entry calls out the deferral.

### [PR-28-D07] TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts host floor 1500 too loose (current 1582)
**Status:** resolved
**Severity:** minor
**Location:** gen_test.go:1190-1206
**Description:** Test asserts hostNodes >= 1500 against current 1582 (reference 1797). Future regression dropping ~80 host nodes (e.g. accidental libcxx-host pruning) would still pass.
**Fix:** Floor tightened to `>= 1582` (current emission baseline). Future PR closing the 215-node gap will raise floor + update Completed entry.

### [PR-28-D08] util gate uses Flags.PIC as host/target-axis discriminator; correct discriminator is instance.Target
**Status:** resolved
**Severity:** minor
**Location:** gen.go:719
**Description:** Predicate `!instance.Flags.PIC` repurposes a flag-level signal as platform-axis check. Works under M2 because WithHost flips PIC=true and target modules don't currently need PIC=true; breaks if a target shared library legitimately needs PIC. Correct predicate is `instance.Target == ctx.cfg.Target.ID`.
**Fix:** Predicate changed to `instance.Target == targetPlatformID` with nil-safe fallback to `DefaultLinuxConfig.Target.ID` for unit tests; defaultPeerdirsFor signature now takes `*genCtx`. Comment rewritten positively per brief.

### [PR-28-D09] Synthetic R6 test does not pin cmd_args[0] ↔ host LD outputs[0] consistency
**Status:** resolved
**Severity:** nit
**Location:** gen_test.go:944-1033 (TestGen_HostToolRecursion_R6)
**Description:** Test only checks counts and deps↔ldUID linkage; does not verify R6 cmd_args[0] equals host LD outputs[0]. Adding the assertion would have caught D01 immediately.
**Fix:** Assertion added inside TestGen_HostToolRecursion_R6 pinning `r6Node.Cmds[0].CmdArgs[0] == ldNode.Outputs[0]`.

### [PR-28-D10] PR-28 plan doc allegedly missing from worktree
**Status:** resolved (not a defect — plan doc exists in main at docs/drafts/20260507-1131-pr28-dual-platform.md but worktree was branched from main BEFORE the planning subagent wrote it)
**Severity:** nit
**Location:** docs/drafts/20260507-1131-pr28-dual-platform.md (in main checkout)
**Description:** Reviewer flagged plan doc as missing. Cause: orchestrator dispatched the PR-28 executor in worktree isolation immediately after the planner wrote the doc, but worktree creation snapshots main HEAD (which at the time did not include the plan doc commit). The plan doc exists in main; reviewer searched the worktree only.
**Fix:** No code change. Process note: future plan docs should be committed to main before spawning a worktree-isolated executor that needs them as context, OR the executor brief should inline the plan doc text rather than reference its path.

### [PR-28-D11] D02's SRCDIR threading misses JOIN_SRCS / EmitJS / downstream EmitCC and the LD itself
**Status:** resolved
**Severity:** minor
**Location:** gen.go:888-899 (JOIN_SRCS path), gen.go:932-941 (LD/LDOutputPath), js.go:28-89
**Description:** D02's docstring claims "module_dir + inputs reflect SRCDIR-relocated source root" but JS-derived nodes were NOT rebased: 7 ragel6 JS nodes + 7 downstream JS-derived CC nodes still carried `module_dir = contrib/tools/ragel6/bin`, and JS inputs were bare `$(SOURCE_ROOT)/cdcodegen.cpp` (preexisting EmitJS bug compounded).
**Root cause:** EmitJS called with unmodified `instance` and bypassed emitOneSource. EmitJS itself prepended `$(SOURCE_ROOT)/` to bare source names without resolving against any module path.
**Fix:** JOIN_SRCS branch now constructs `srcInstance` (with srcDir applied) and passes it to both EmitJS and the downstream EmitCC. EmitJS path composition fixed: cmd_args use `<instance.Path>/<src>`, inputs use `$(SOURCE_ROOT)/<instance.Path>/<src>`. js_test.go updated to use bare module-relative names (the correct convention matching real ya.make parser output). Lift: L1 88.47% → 88.66%, L2 86.46% → 86.89%.

### [PR-28-D12] D02 acceptance criterion "regression test pins SRCDIR-rewritten module_dir AND inputs" not satisfied
**Status:** resolved
**Severity:** minor
**Location:** gen_test.go (no test added in round 2)
**Description:** Brief D02 acceptance criterion #9 demanded a regression test pinning the SRCDIR-rewritten module_dir AND inputs. None was added in the initial fix. Coverage (`TestGen_ToolsArchiver_DualPlatform_HostAndTargetCounts`) only checked node counts at the `>= 1582` floor.
**Fix:** TestGen_SrcDirRebasesSourceResolution added with three subtests: SRCDIR with SRCS, no-SRCDIR baseline, SRCDIR with JOIN_SRCS. Each pins both module_dir and inputs.

### [PR-28-D13] Stale comment in collectStmts contradicts D02
**Status:** resolved
**Severity:** nit
**Location:** gen.go:303-307 (SrcDirStmt case)
**Description:** Comment said `"PR-25 collects but does NOT thread this into emitOneSource — tools/archiver closure has no SRCDIR usages so the gap is invisible today. PR-26 wires it through."` Outdated: emitOneSource now threads srcDir, and ragel6/bin uses SRCDIR within tools/archiver closure.
**Fix:** Comment replaced with current-state description: SRCDIR shifts source resolution; PR-28-D02 threads srcDir into emitOneSource; PR-28-D11 closed JOIN_SRCS gap; LD/AR remain at instance.Path (intentional — binary lives where declared even if sources are elsewhere).

---

## PR-29

### [PR-29-D01] CXXFLAGS GLOBAL modifier leaks as literal cmd_arg
**Status:** resolved
**Severity:** major
**Location:** gen.go:350-353 (applyUnknownStmt CXXFLAGS/CONLYFLAGS branches); gen.go:881-885 (collected via cxxFlags into ModuleCCInputs); cc.go:276 (appendCxxStdAndOwn)
**Description:** applyUnknownStmt appends `v.Args...` directly into `d.cxxFlags`/`d.cOnlyFlags` without using `splitGlobalModifier`. When ya.make declares `CXXFLAGS(GLOBAL -nostdinc++)` (libcxx ya.make:68 IF (CLANG) branch, taken in M2), the parser produces UnknownStmt{Name: "CXXFLAGS", Args: ["GLOBAL", "-nostdinc++"]}. Walker stores both args verbatim; appendCxxStdAndOwn appends slice to cmd_args. Result: every libcxx CC node emits literal `GLOBAL` at cmd_args[95] (116 nodes verified). Compounding: `CXXFLAGS(GLOBAL X)` per ymake semantics propagates X to peers and does NOT apply to self — injecting `-nostdinc++` into libcxx's own cmd_args is also semantically wrong (D04 PR-30 territory). Contrast: ADDINCL/CFLAGS go through typed *AddInclStmt/*CFlagsStmt whose parsers strip the modifier; CXXFLAGS/CONLYFLAGS were left untyped.
**Root cause:** PR-29-D02 plan correctly noted "GLOBAL CXXFLAGS — out of scope for D02" but implementation forgot to FILTER GLOBAL out. Pipeline silently includes GLOBAL-prefixed args because applyUnknownStmt has no awareness "GLOBAL" is a meta-token.
**Fix:** applyUnknownStmt CXXFLAGS/CONLYFLAGS branches at gen.go:350-367 call splitGlobalModifier; when mod=="GLOBAL", break (drop both literal "GLOBAL" token AND the flags — peer-propagation routing deferred to PR-30 D04). Inline comments name PR-30 D04 as future site. Empirical: regenerated tools/archiver has zero "GLOBAL" tokens in any cmd_args (3321 nodes); libcxx CC nodes correctly drop both GLOBAL and -nostdinc++ from own cmd_args.

### [PR-29-D02] D02 test coverage bypasses parser→walker→emit path
**Status:** resolved
**Severity:** minor
**Location:** cc_test.go:508-553 (TestEmitCC_OwnCXXFlags_SlotsAfterSuppressionBlock), :559-575 (TestEmitCC_COnlyFlags_AppliesOnlyToCSources)
**Description:** Both D02 pin tests construct ModuleCCInputs.CXXFlags/COnlyFlags directly with hand-written non-GLOBAL slices. They don't exercise the parser → applyUnknownStmt → moduleData.cxxFlags → ModuleCCInputs pipeline. The GLOBAL-leak defect (D01) is invisible to them — they pass cleanly with bug present.
**Fix:** Covered by D01's regression test `TestGen_CXXFLAGS_GLOBAL_NotLeakedToOwnCmdArgs` (gen_test.go:1956-2084) — three subtests (CXXFLAGS GLOBAL drop, CONLYFLAGS GLOBAL drop, non-GLOBAL passthrough) exercising the full parser→walker→emit pipeline via real ya.make + Gen + cmd_args inspection.

### [PR-29-D03] cc.go header comment claims Generator is "wired into DepRefs" but no caller sets it
**Status:** resolved
**Severity:** nit
**Location:** cc.go:11, :57-71
**Description:** Package doc-block said ModuleCCInputs "carries... a Generator NodeRef wired into DepRefs." Field comment said "Generator: NodeRef threaded into DepRefs when IsGenerated (D07)." Both implied live integration. No caller in gen.go sets `ccIn.Generator`; gate at cc.go:202 unreachable today.
**Fix:** cc.go:11 doc-block updated to "reserved for PR-30; not wired today"; cc.go:62-63 field comment cites cc.go:196-201 as the L0-deferral rationale.

### [PR-29-D04] composeMuslCC's appendCxxStdAndOwn produces double `-Wno-everything` for any future C++ musl source
**Status:** resolved
**Severity:** minor
**Location:** cc.go:389 (composeMuslCC), cc.go:441 (composeMuslHostCC), cc.go:263-279 (appendCxxStdAndOwn)
**Description:** Musl composers hard-coded `appendCxxStdAndOwn(cmdArgs, isCxx, true, ownExtras)`. When isCxx && noCompilerWarnings both hold, helper appended `muslWarningFlags...`. But musl composers ALREADY appended muslWarningFlags... earlier. Latent: M2 closure has no musl C++ source today; surfaces once musl/full lands.
**Fix:** appendCxxStdAndOwn signature gained `injectCxxWarningBundle bool` (cc.go:270). composeTargetCC/composeHostCC pass true (preserves pre-D04 behavior); composeMuslCC/composeMuslHostCC pass false. Inline comment at musl call sites: "musl already added muslWarningFlags above; suppress duplicate injection in helper."

### [PR-29-D05] noCompilerWarnings parameter unused in composeMuslCC/composeMuslHostCC, hidden via `_ = noCompilerWarnings`
**Status:** resolved
**Severity:** nit
**Location:** cc.go:367 (composeMuslCC), cc.go:419 (composeMuslHostCC)
**Description:** Both musl composers took `noCompilerWarnings bool` and immediately discarded via `_ = noCompilerWarnings`. Body always behaved as if noCompilerWarnings=true. Parameter was decorative.
**Fix:** Parameter removed from composeMuslCC (cc.go:373) and composeMuslHostCC (cc.go:424). Dispatcher at cc.go:137-139 passes only the new short signature. No `_ = noCompilerWarnings` lines remain. Inline comment retained next to the muslWarningFlags append: "musl always uses muslWarningFlags by definition."

---

## PR-30

### [PR-30-D01] ar.go peerArchiveRefs documentation contradicts new invariant
**Status:** resolved
**Severity:** nit
**Location:** ar.go:67-72, ar.go:197-211
**Description:** ar.go's emitARNode and EmitAR docstrings still claim "peerArchiveRefs are wired as DepRefs so the AR node's UID accounts for them". Per PR-30 D05, the production caller (gen.go:1131) now passes nil unconditionally and the reference invariant is "every AR has zero AR-on-AR deps". Docstring is technically still accurate at function-signature level (passing non-nil still wires DepRefs) but readers comparing the doc against the new reference invariant will be confused.
**Suggested fix:** Append one sentence to both docstrings: "PR-30 D05: production caller passes nil; reference graph confirms zero AR-on-AR deps. Parameter retained for tests that pin the historical shape."

### [PR-30-D02] as.go top-level docstring lists only ForeignDepRefs, omits new DepRefs wiring
**Status:** resolved
**Severity:** nit
**Location:** as.go:7-10, as.go:39-42
**Description:** as.go's top-level comment and `yasmLD` parameter doc both stated "wired into ForeignDepRefs[\"tool\"]" with no mention of PR-30 D02's addition that also threads yasmLD into DepRefs.
**Fix:** Updated as.go:19-22 (file-level) and as.go:47-50 (yasmLD parameter doc) to note dual wiring (DepRefs + ForeignDepRefs["tool"]) and the L0 fingerprint rationale (asmlib 25 AS nodes).

### [PR-30-D03] No direct test pin for as.go yasm DepRefs wiring
**Status:** resolved
**Severity:** minor
**Location:** as_test.go (TestEmitAS_CxxsuppBuiltinsChkstk_ByteExact)
**Description:** Single AS byte-exact test (chkstk) passed nil yasmLD; exercised no-yasm code path. New "yasmLD non-nil → DepRefs populated" behaviour verified only indirectly via L0 numbers.
**Fix:** TestEmitAS_YasmLD_PopulatesDepRefs added at as_test.go:192-232. Exercises non-nil yasmLD via dummy emitted node; asserts both DepRefs[0]==yasmLD and ForeignDepRefs["tool"][0]==yasmLD.

### [PR-30-D04] defaultProgramPeerdirsFor in-helper musl/full self-suppression is dead code
**Status:** resolved
**Severity:** nit
**Location:** gen.go:746-752
**Description:** Block `if instance.Path != muslFullPath && !strings.HasPrefix(instance.Path, muslFullPath+"/")` was unreachable: only caller gates on `!isRuntimeAncestor(instance.Path)`, which returns true for any path under `contrib/libs/musl/`.
**Fix:** Inner guard removed at gen.go:746-751; replaced with unconditional `peers = append(peers, muslFullPath)` plus comment naming the caller's gate as the load-bearing exclusion mechanism.

### [PR-30-D05] tasks.md edited by executor; orchestrator-owned per CLAUDE.md
**Status:** resolved (orchestrator overwrites with own version at squash time)
**Severity:** nit
**Location:** tasks.md:51, tasks.md:374-394 (in worktree)
**Description:** CLAUDE.md loop discipline reserves tasks.md flips and Completed entries for orchestrator (I3 + I5). PR-30 commit includes both: status flip [~] → [x] AND a 20-line rich Completed entry. Brief flagged for review. Content quality high but process boundary crossed.
**Fix:** At squash time, orchestrator stages source-only files from worktree (excludes tasks.md); writes own Completed entry from main checkout per I5.

### [PR-30-D06] gen.go:747 caller-name in comment misnames `genModule` as `defaultPeerdirsFor`
**Status:** resolved (deferred — 1-token nit; orchestrator doesn't fix code per CLAUDE.md; logged for future cleanup PR)
**Severity:** nit
**Location:** gen.go:747
**Description:** Comment added in D04 reads `Caller (defaultPeerdirsFor in gen.go:932) gates on !isRuntimeAncestor(...)`. Actual caller is `genModule` (defined at gen.go:835); line 932 is inside genModule's body, not inside defaultPeerdirsFor. Line number correct; function name not.
**Fix:** Deferred. One-token edit (`defaultPeerdirsFor` → `genModule`); will pick up in next gen.go cleanup PR or M3 work.

---

## PR-31

### [PR-31-D01] include scanner BaseSearchPaths omits $(SOURCE_ROOT), missing all repo-rooted system-form includes
**Status:** resolved
**Severity:** major
**Location:** gen.go:1656-1700 (includeScannerBasePaths); scanner.go:380-388 (resolve fallback chain)
**Description:** CC compiler invocations include `-I$(SOURCE_ROOT)` in cmd_args, enabling `<util/folder/path.h>`, `<library/cpp/...>`, `<contrib/...>` resolution against source-tree root. Scanner's BasePaths originally only had linux-headers prefix.
**Fix:** includeScannerBasePaths prepends empty-string entry (resolves to source root) for non-musl flavours. main.cpp.o went from 17 → 1002 inputs (vs ref 1009). L2 itself stayed at 79.92% (multiset comparison requires exact match, and the residual 7-input gap from D12 dominates), but the structural input recovery is real.

### [PR-31-D02] PR brief reports a target/host shift that did not occur
**Status:** resolved (orchestrator-acknowledged; brief's "1985→1916 / 1733→1802" framing was based on a stale baseline)
**Severity:** minor
**Location:** PR-31 commit message + Completed-entry draft (only)
**Description:** Brief stated target dropped 69 (1985→1916) and host gained 69 (1733→1802). Reviewer verified by building parent commit cc3c60b: pre-PR-31 already produces target=1916, host=1802. The "1985+1733" baseline is from an earlier point in the codebase, not the immediate parent (probably PR-30 measurement against g.json). No 69-node re-platforming caused by PR-31.
**Fix:** No code change. Orchestrator's PR-31 Completed entry will use correct framing: "target/host counts unchanged from pre-PR-31 (1916/1802); the older 1985+1733 baseline cited in some PR-30 prep notes was stale (measured before later closure refinements)."

### [PR-31-D03] Relaxed JS/LD prefix tests pass silently on the actual regression mode
**Status:** resolved
**Severity:** major
**Location:** js_test.go:98-110; ld_test.go:247-249
**Description:** Both tests document a "known regression" where the emitter underproduces inputs (got=8, ref=941 in JS; got=10, ref ~1052 in LD). Their relaxed check reads `if len(ref.Inputs) < len(got.Inputs) { t.Errorf("regression?") }` — fires only when our output is LARGER than reference. Per-element loop iterates `got.Inputs` indices and breaks at `i >= len(ref.Inputs)`. Future regression dropping got to 5 inputs (still a prefix of ref) passes silently. Tests give appearance of meaningful regression check but check almost nothing.
**Suggested fix:** Hard-pin `len(got) == K` for explicit current-emitter K (8 for JS, 10 for LD), with comment "documented prefix subset; PR-32+ extends to full set". Future drops below K then fail. Or convert to `t.Skip(...)`.

### [PR-31-D04] gen.go:142-143 comment falsely claims the two scanners share a parsed-includes cache
**Status:** resolved
**Severity:** nit
**Location:** gen.go:141-149
**Description:** Comment says "scannerTarget … scannerHost … They share the same parsed-includes cache via the file-system layer". Implementation in scanner.go:92-99 (`NewIncludeScanner`) constructs a fresh `parsed map[string][]includeDirective` for each scanner — nothing shared.
**Suggested fix:** Rewrite comment: "Each scanner has its own parsed-includes cache (the OS page cache amortises rereads). Each also has its own SysInclSet because linux-musl-<arch>.yml mappings differ between platforms."

### [PR-31-D05] Dead `isPrimary` parameter in scanner.go:dfs
**Status:** resolved
**Severity:** nit
**Location:** scanner.go:199, 213, 216
**Description:** dfs's `isPrimary bool` parameter never consulted; body has `_ = isPrimary` to silence unused-parameter warning. Source-itself filtering happens in WalkClosure's post-walk loop. isPrimary is set true initial / false thereafter; nothing branches on it.
**Suggested fix:** Remove the parameter from `dfs` signature and the two call sites.

### [PR-31-D06] Dead `searchPathFound` terminal write in scanner.go:resolve
**Status:** resolved
**Severity:** nit
**Location:** scanner.go:380-390
**Description:** Inside BaseSearchPaths loop the variable is set to true at line 383 but never read after; line 390 silences the warning. Relic from earlier shape that gated sysincl on `!searchPathFound`; current code unconditionally appends sysincl matches.
**Suggested fix:** Replace the BaseSearchPaths body's `searchPathFound = true; break` with a bare `break`; delete line 390.

### [PR-31-D07] gen_test.go:2544 still loads g.json instead of sg.json (D01 incomplete)
**Status:** resolved
**Severity:** minor
**Location:** gen_test.go:2538-2549
**Description:** D01 mandated referenceGraphPath switch to sg.json across all tests. TestGen_ToolsArchiver_L0_AtLeast95's doc-comment names sg.json but body skips on `os.Stat(filepath.Join(sourceRoot, "g.json"))` and calls `LoadReference(filepath.Join(sourceRoot, "g.json"))`. Test still passes (g.json present, topology nearly identical) but skip-gate, reference, doc-comment disagree.
**Suggested fix:** Replace both literal "g.json" references with "sg.json".

### [PR-31-D08] JS-emitter include-closure regression not recorded in defects.md
**Status:** resolved (deferred — JS emitter scope expansion to PR-32+)
**Severity:** minor
**Location:** js.go::EmitJS; js_test.go:90-110 inline comment; PR-31 plan doc
**Description:** PR-31 acknowledges (commit message, plan doc, js_test.go inline comment) that JS emitter doesn't fold per-source include closure into JS-derived CC node inputs (~941 entries missing per JS-derived CC; 23 such CCs; util/all_charset.cpp.o produces 1 input vs reference 1176). CLAUDE.md requires every reviewer-acknowledged gap to land as a structured defects.md entry.
**Suggested fix:** This entry IS the defects.md record (now). Mark `resolved (deferred — js.go scope expansion in PR-32+)` after the round-2 fix lands.

### [PR-31-D09] LD-emitter peer-archive + member-input passthrough regression not recorded in defects.md
**Status:** resolved (deferred — LD emitter scope expansion to PR-32+)
**Severity:** minor
**Location:** ld.go::composeLDInputs; ld_test.go:236-249 inline comment
**Description:** Same pattern as D08. ld.go::composeLDInputs doesn't fold peer-archive paths or per-member-CC include closures into LD inputs; relaxed test (itself defective per D03) acknowledges deferral.
**Suggested fix:** This entry IS the defects.md record. Mark `resolved (deferred — ld.go scope expansion in PR-32+)` after round-2 fix lands.

### [PR-31-D10] sysincl_test.go contains dead `var _ = fmt.Sprintf` / `var _ = strings.Contains` declarations
**Status:** resolved
**Severity:** nit
**Location:** sysincl_test.go:222-225
**Description:** Comment reads "debug helper used during development; kept as a non-test func to avoid pulling fmt unconditionally if go vet flags it" but body is two `var _ = …` placeholders existing only to consume imports.
**Suggested fix:** Remove sysincl_test.go:223-225 plus any imports of fmt/strings that have no other consumer in the file.

### [PR-31-D11] AR member-input aggregation produces near-correct but consistently-incomplete inputs (35 of 38 AR nodes diverge)
**Status:** resolved (deferred — downstream of D12; auto-resolves when PR-32 musl refactor + ARCH-IF evaluator close the upstream CC input gap)
**Severity:** minor
**Location:** ar.go::EmitAR memberInputs aggregation; gen.go:1107-1118 walker accumulator
**Description:** Of 38 archiver-target AR nodes, only 3 have exact input-set match. Shape is correct; residual divergence is downstream of incomplete CC member resolutions (D12). Round-2 D01 fix (empty-prefix BasePaths) lifted main.cpp.o from 17→1002 inputs but residual 7-input-per-node musl-arch gap remained, blocking AR exact match too.
**Fix:** Deferred to PR-32. Once PR-32's flag-driven musl PEERDIR mechanism + narrow ARCH-IF evaluator close D12, AR aggregation will become exact (no AR-side code change needed; AR already correctly aggregates whatever CC inputs it gets).

### [PR-31-D12] Non-musl CC nodes missing 4 musl-arch -I flags; explains L2 stagnation at 79.92%
**Status:** resolved (deferred — to PR-32 musl refactor; implementing here would bake more musl-hardcoding before the architectural refactor)
**Severity:** major
**Location:** gen.go (TARGET module construction; missing auto-PEERDIR seeding mirroring ymake.core.conf:781); contrib/libs/musl/include/ya.make handling missing
**Description:** Every non-musl TARGET CC node in reference graph emits 4 -I flags: `-I$(SOURCE_ROOT)/contrib/libs/musl/{arch/aarch64, arch/generic, include, extra}`. Our gen emits ZERO. Reproduced: 0 of 112 paired non-musl .cpp.o nodes have byte-exact cmd_args; 1287 of 18031 missing inputs are musl/linux-headers-namespaced. **This is the load-bearing reason L2 sits at 79.92%** — the matching input headers (bits/fcntl.h, sys/select.h, sched.h, alloca.h, strings.h, etc.) cannot be resolved without corresponding -I paths in scanner BasePaths. Estimated lift if fixed: ~110 of 112 non-musl .cpp.o pairs gain L2-exact + L3-exact, with knock-on lifts for dependent LDs. L2 ceiling 90%+ if combined with closing JS-joined-source scan gap (D08).
**Root cause (CORRECTED 2026-05-07 by PR-32 planner):** Originally diagnosed as two-part (missing auto-PEERDIR + IF-blind walker). The "IF-blind walker" claim was WRONG — gen.go:344-351 already calls EvalCond correctly, and ARCH_AARCH64/ARCH_X86_64/etc. are bound in DefaultIfEnv + flipped per platform in buildIfEnv. The single real root cause is `build/ymake.core.conf:781 when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include; CFLAGS+=-D_musl_ }` not being mirrored — auto-PEERDIRs every TARGET module on linux-musl to `contrib/libs/musl/include` AND adds a `-D_musl_` consumer-side CFLAG. Once musl/include is reached as an implicit peer, its existing IF(ARCH_AARCH64) ADDINCL(GLOBAL ...) blocks evaluate correctly.
**Fix:** Deferred to **PR-32 (musl refactor)**. The orchestrator's rationale: implementing the auto-PEERDIR here would add MORE musl-hardcoding right before the dedicated PR-32 refactor that establishes the flag-driven mechanism per user's directive. PR-32 handles via flag-driven `cliDefines["MUSL"]=="yes"` gate. No ARCH-IF evaluator work needed.

### [PR-31-D13] Spurious -I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/src on non-musl CC nodes consuming libcxx
**Status:** resolved
**Severity:** minor
**Location:** gen.go (peer-GLOBAL ADDINCL collection — walkPeersForGlobalAddIncl / effectiveAddInclGlobal at gen.go:1066-1088)
**Description:** main.cpp.o emitted `-I$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/src` as the 4th -I flag. Reference has this only on libcxx's OWN nodes (module-own ADDINCL), NOT consumers (only GLOBAL propagates). Caused byte-mismatch on every non-musl CC consuming libcxx (~110 nodes).
**Root cause:** Case A — collector leak in ADDINCL per-path GLOBAL handling. AddInclStmt struct used statement-level `Modifier` string; splitGlobalModifier stripped leading `GLOBAL` token and treated ALL remaining args as global. For libcxx: `ADDINCL(GLOBAL .../include  .../src)` — `src` ended up in addInclGlobal too, propagating to all consumers.
**Fix:** Replaced `AddInclStmt.{Modifier, Paths}` with `{GlobalPaths, OwnPaths}`. New `splitAddInclPaths` applies per-path GLOBAL prefix semantics: a path token immediately following the literal `GLOBAL` keyword goes to GlobalPaths; all others to OwnPaths. gen.go routes GlobalPaths → d.addInclGlobal, OwnPaths → d.addIncl. Verified empirically: zero non-libcxx CC nodes emit `-I libcxx/src`; libffi (mixed GLOBAL+bare paths) and linux-headers (multiple GLOBAL prefixes) both handled correctly.

---

## PR-32

### [PR-32-D01] Executor's "single outlier `-D_musl_=1`" claim understates scope by ~89×
**Status:** resolved (orchestrator-amended — Completed entry will state correct 89-instance scope)
**Severity:** minor
**Location:** docs/drafts/20260507-2137-pr32-musl-refactor.md (executor's review summary), gen.go:1244-1262 (musl-self GLOBAL CFLAGS suppression site)
**Description:** Executor reported "single outlier `-D_musl_=1` in tools/archiver/main.cpp.o slot 60". Empirical: 89 missed `-D_musl_=1` occurrences in CC nodes (yasm 79 host CC + ragel6 9 host CC + archiver 1 target CC). Origin is yasm/ragel6 ya.makes' own `IF (MUSL) CFLAGS(-D_musl_=1)` branch, NOT the peer-propagation suppression site. Operationally L2/L3 ceiling territory, not a PR-32 regression — but the audit characterisation is imprecise.
**Fix:** PR-32 Completed entry uses correct empirical 89-instance scope.

### [PR-32-D02] `--define MUSL=no` produces internally inconsistent graph
**Status:** resolved
**Severity:** minor
**Location:** gen.go:720-728 (NoLibc-only gate on `contrib/libs/musl` peerdir), gen.go:792 (cliMuslOn-gated `contrib/libs/musl/include`)
**Description:** When `--define MUSL=no`, `defaultPeerdirsFor` still adds `contrib/libs/musl` (the source-bearing peer; PR-26 default) because that gate keys only on `instance.Flags.NoLibc` and `noPlatform`, not on `cliMuslOn(ctx)`. Meanwhile `contrib/libs/musl/include` auto-PEERDIR (line 792) IS gated on `cliMuslOn` so it disappears, and `defaultPeerCFlags` returns nil. Result: musl source library still walked but no peer-propagation. Today moot (M2 only supports MUSL=yes), but the `--define` flag is partially load-bearing.
**Fix:** gen.go:720 gate extended with `&& cliMuslOn(ctx)` so MUSL=no produces musl-free graph (665 nodes vs MUSL=yes 3718 nodes; verified). cliMuslOn returns true when ctx==nil (preserves direct-test-call back-compat) or `cliDefines["MUSL"] == "yes"`.

### [PR-32-D03] `_artifacts/` is untracked; not in `.gitignore`
**Status:** resolved
**Severity:** nit
**Location:** .gitignore (does not list _artifacts/ or debug/)
**Description:** PR-32 sessions produced 600MB+ of artifacts under `_artifacts/`. `git status` lists as untracked; future `git add -A` slip would commit ~600MB of binary JSON.
**Fix:** `.gitignore` extended with `/_artifacts/` and `/debug/` entries.

---

## PR-33

### [PR-33-A_clean] Reviewer A (correctness lens) returned NO DEFECTS
**Status:** resolved (no action needed)
**Severity:** —
**Description:** Numbers reproduce; cycle invariant preserved; bucket helper edge cases verified (own/peer empty/non-empty/overlap); test inversions honest (TestGen_CXXFLAGS_GLOBAL_LandsOnOwnCmdArgs is correct rename matching D02 semantic). 10/10 sample-trace of newly-byte-exact pairs are genuinely byte-exact. M1 byte-exact preserved.

### [PR-33-C01] util-module PEERDIR ordering: libcxx/libcxxrt -I emitted AFTER musl, not BEFORE (16 util .o nodes + 9 ragel6)
**Status:** open
**Severity:** major
**Location:** gen.go (PEERDIR walk for util — exact site under investigation)
**Description:** For util/_/digest/city.cpp.o aarch64 (and 15 other util .o + 9 ragel6 transitively): reference emits include order [linux-headers, libcxx/include, libcxxrt/include, musl/arch, musl/include, ...]; ours emits [linux-headers, musl/arch, musl/include, ..., libcxx/include LAST, libcxxrt/include]. Slot count + surrounding flags byte-exact except the 2-slot reorder. PR-33's util-libcxx peering work introduced libcxx/libcxxrt as peers but inserted them at the TAIL of the include list rather than between linux-headers and musl/arch.
**Suggested fix:** Order util's libcxx/libcxxrt PEERDIRs BEFORE musl in include emission. Likely peer-list ordering issue in PEERDIR walk where library-class peers should precede toolchain-injected peers (musl is libc toolchain; libcxx is user library wrapping it).

### [PR-33-C02] yasm libyasm missing `-D_musl_=1` macro (30 yasm libyasm + 11 yasm modules)
**Status:** open
**Severity:** major
**Location:** yasm-specific CFLAGS injection (likely contrib/tools/yasm/libyasm/ya.make IF (MUSL) handling)
**Description:** Reference yasm/_/libyasm/assocdat.c.pic.o x86_64 emits `-D_musl_=1` at slot 52 after YASM-specific defines. Ours skips this slot; everything from slot 52 onward shifts by one. 30 yasm libyasm + 6 yasm modules + 5 yasm objfmts/elf nodes (~41 total) regress at L3.
**Suggested fix:** Inspect contrib/tools/yasm/libyasm/ya.make (or equivalent) for per-module CFLAGS or BUILDWITH_MUSL conditional setting `-D_musl_=1`, route through emitter. Independent of libcxxrt-doubled-flag pattern (PR-32-D01 89-instance overlap likely covers part of this).

### [PR-33-C03] sysincl uchar.h over-fan-out — DOMINATES L2 ceiling (251 libcxx/abseil/libcxxabi-parts consumers; +2 spurious inputs each)
**Status:** resolved (deferred — PR-34 sysincl per-record source-vs-includer routing)
**Severity:** major
**Location:** sysincl.go Lookup path; scanner.go:411-412 includerRel keying
**Description:** stl-to-libcxx.yml uchar.h record has source_filter `^(?!(contrib/libs/musl|contrib/tools/yasm)).*|^contrib/libs/musl/tests` meant to reject musl callers. Our keying uses immediate-includer path; non-musl source reaching uchar.h via yasm-related includer chain triggers libcxx-uchar mapping. Closing alone would push L2 83.94% → ~90.7%.
**Fix:** Deferred. PR-34's per-record source-class bucket gating addresses this.

### [PR-33-C04] F1 (libcxxrt own non-GLOBAL CXXFLAGS doubled) under-counted at 8; actual ~29 (14 libcxxrt + 6 libcxxabi-parts + 9 ragel6)
**Status:** resolved (count correction; underlying mechanism is real, fix needed)
**Severity:** minor
**Location:** cc.go own-CXXFLAGS emission — single emission where reference emits twice for libcxxrt-class own flags
**Description:** Reference doubles `-nostdinc++` flanking catboost-redux for libcxxrt/libcxxabi-parts/ragel6 own non-GLOBAL CXXFLAGS. Ours emits once. Note: PR-33 D02 ALREADY does this correctly for own + peer GLOBAL CXXFLAGS (libcxx, abseil byte-exact); fails only for OWN NON-GLOBAL CXXFLAGS in libcxxrt-class modules.
**Suggested fix:** Investigate why libcxx/abseil work (own GLOBAL + peer GLOBAL via bucket-twice) but libcxxrt own NON-GLOBAL doesn't. Likely a missing GLOBAL-flag-routing for OWN-module non-GLOBAL CXXFLAGS in those classes — or upstream uses a different mechanism (e.g. duplicate cmd_args via a SECOND emit pass we don't replicate).

### [PR-33-C05] F5 mislabeled — tcmalloc duplicate `-I tcmalloc` is a successful PR-33 match, not a defect
**Status:** resolved (no action — record-keeping correction only)
**Severity:** nit
**Description:** PR-32 emitted `-I tcmalloc` once; PR-33 emits twice (slots 9 + 19) matching reference exactly. 36 tcmalloc/no_percpu_cache nodes went L3-mismatch → L3-match in PR-33. `_BUILTIN_PEERDIR` self-walk hypothesis empirically confirmed.

### [PR-33-B01] AddIncl GlobalPaths-first reorder breaks intra-stmt declaration order for stmts interleaving GLOBAL and non-GLOBAL paths
**Status:** resolved (deferred — M5 hardening; libcxx empirically declares GLOBAL first; latent until a future module declares interleaved)
**Severity:** minor
**Location:** gen.go:377-379
**Description:** `d.addIncl = append(d.addIncl, v.GlobalPaths...); d.addIncl = append(d.addIncl, v.OwnPaths...)` per stmt collapses GlobalPaths-then-OwnPaths regardless of declaration order. For `ADDINCL(src GLOBAL include)` (non-GLOBAL before GLOBAL) the collapse emits `[include, src]` REORDERED. libcxx happens to declare GLOBAL first so visible behavior is correct.
**Fix:** Deferred. Future M5 hardening: extend AddInclStmt to carry single ordered `[]struct{Path, Global}` slice; reproduce both fields in true source order.

### [PR-33-B02] Duplicate musl-self gate for own GLOBAL CFLAGS in genModule (~17 lines redundant)
**Status:** resolved (deferred — M5 hardening)
**Severity:** minor
**Location:** gen.go:1252-1262 vs gen.go:1316-1333
**Description:** genModule computes LibcMusl-gated own-GLOBAL CFLAGS triple TWICE. Lines 1252-1262 produce `ownCFlagsGlobal`/etc. (used for moduleEmitResult); lines 1316-1333 produce `*Self` variants with IDENTICAL gate (used for ModuleCCInputs.Own*Global). Drift risk if gate evolves.
**Fix:** Deferred. Future cleanup: drop second computation, reuse first.

### [PR-33-B03] Asymmetric data shape for ADDINCL vs CFLAGS GLOBAL handling in ModuleCCInputs
**Status:** resolved (deferred — M5 hardening; structural rot signal but functional)
**Severity:** minor
**Location:** cc.go:79-166 (ModuleCCInputs); gen.go:377-379 vs gen.go:380-401
**Description:** ADDINCL globals merged INTO d.addIncl (mixed bag, no OwnAddInclGlobal field); CFLAGS keep OwnCFlagsGlobal/PeerCFlagsGlobal split. Future readers must internalise asymmetry.
**Fix:** Deferred. Pick one shape uniformly; document rationale or unify.

### [PR-33-B04] composeTargetCC/composeHostCC now take 11 positional parameters; PR-29 struct-refactor regression
**Status:** resolved (deferred — M5 hardening)
**Severity:** minor
**Location:** cc.go:660 (composeTargetCC), cc.go:726 (composeHostCC)
**Description:** PR-29 reshaped EmitCC to ModuleCCInputs struct to avoid positional growth. Composers below have grown 6→8→11 params over PR-31/32/33. Same refactor pattern should apply.
**Fix:** Deferred. Introduce `composerInputs` struct bundling per-source-language and per-module args.

### [PR-33-B05] composeOwnAndPeerGlobalBucket capacity over-allocates by one branch
**Status:** resolved (deferred — nit; functionally correct)
**Severity:** nit
**Location:** cc.go:603-606
**Description:** Capacity counts BOTH C++ axis lengths AND C-only axis lengths, but only one is emitted per isCxx. Functionally correct (cap is hint).
**Fix:** Deferred. Compute cap conditionally on isCxx.

### [PR-33-B06] composePeerExtras called for C++ branch but result discarded
**Status:** resolved (deferred — nit; trivially wasted CPU)
**Severity:** nit
**Location:** cc.go:248, cc.go:687-698
**Description:** peerExtras computed unconditionally, only used in !isCxx branch.
**Fix:** Deferred. Move call inside C-branch or document the discard.

### [PR-33-B07] TestIsRuntimeAncestor_LiteralOnly omits 1 literal + includes a non-module artifact path
**Status:** resolved (deferred — test coverage gap; does not affect production behavior)
**Severity:** nit
**Location:** gen_test.go:2687-2700, 2708-2715
**Description:** Test enumerates 12 of 13 runtimeAncestorPaths entries (missing `contrib/libs/linuxvdso/original`); subtree slice includes `util/datetime/parser.rl6.cpp.o` (build artifact path, not module path).
**Fix:** Deferred. Add missing literal; replace artifact with real subtree module.

### [PR-33-B08] "bucket" terminology not anchored to file's existing vocabulary
**Status:** resolved (deferred — nit; cosmetic)
**Severity:** nit
**Location:** cc.go:572-632 (composeOwnAndPeerGlobalBucket)
**Description:** "bucket" coined locally; rest of cc.go uses "extras"/"block"/"set"/"slot". Future readers must map.
**Fix:** Deferred. Rename to composeOwnAndPeerGlobals; or add one-line glossary in helper docstring.

### [PR-33-A2_01] C01 hoist is reorder-only; runtime ancestors with no PEERDIRs (library/cpp/malloc/api) get NO libcxx/libcxxrt at all
**Status:** open
**Severity:** major
**Location:** gen.go:1313-1343 (hoist gate); affects library/cpp/malloc/api
**Description:** C01 doc claims "model libcxx/libcxxrt as direct GLOBAL peers for runtime ancestors". Implementation only REORDERS entries already in peerAddInclGlobal; never INJECTS when missing. For util this works because user-PEERDIRs (util/charset, zlib, etc.) pull libcxx/libcxxrt into the slice via Phase 2; for library/cpp/malloc/api (runtime ancestor with NO_UTIL + zero PEERDIRs), nothing puts libcxx/libcxxrt into the slice and the hoist has nothing to hoist. Reference malloc.cpp.o has libcxx/include + libcxxrt/include at slots 11-12; ours has musl/arch instead (libcxx/libcxxrt entirely absent). 2 L3 mismatches (.o + .pic.o) in M2; structural concern that the discriminator's premise is operationally false.
**Suggested fix:** Either (a) augment defaultPeerdirsFor so runtime ancestors also receive libcxx/include + libcxxrt/include as implicit GLOBAL header-only contributions; or (b) change the hoist to INJECT-then-reorder. (a) more honest model; (b) smaller patch.

### [PR-33-A2_02 / C2_01] AS-emitter hardcoded to aarch64 toolchain; 62 x86_64 AS nodes have wrong cmd_args
**Status:** open
**Severity:** major
**Location:** as.go:74-79 (EmitAS prologue uses targetTriple + archFlag constants directly); flags.go:63-70 (constants are hardcoded `aarch64-linux-gnu` / `armv8-a`)
**Description:** 62 AS nodes in our output have platform=`default-linux-x86_64` but ALL 62 have `--target=aarch64-linux-gnu` and `-march=armv8-a`. Reference: zero such mismatches (37 x86 AS nodes correctly use `--target=x86_64-linux-gnu`). Sample (musl `ceill.s.o`): 23-arg cmd_args delta (want 109, got 86). Plus EmitAS doesn't inject muslExtraDefines for musl-self assembly (43 nodes total). **Dominates remaining 117 L3 misses (56 of 117 are kind=AS); closing lifts L3 95.60% → ~97.10%.**
**Suggested fix:** EmitAS must branch on `instance.Target` (or host-vs-target flag), choose hostTriple + no -march + muslCcIncludesX8664-style includes for host. Pattern in cc.go:253-262 (composeMuslHostCC). Plus musl-self branch injecting muslExtraDefines per `instance.Flags.LibcMusl`.

### [PR-33-A2_03] C01 doc-comment overstates: "16 util siblings" — actually 5 util own-CC in M2 closure
**Status:** resolved (deferred — nit doc accuracy)
**Severity:** nit
**Location:** gen.go:1326-1329
**Description:** Comment says "util's own CC nodes (util/_/digest/city.cpp.o + 15 siblings)". Reference has only 5 util own-CC nodes; 4 match, 1 (datetime/parser.rl6.cpp.o) diverges for separate reason.
**Fix:** Deferred. Replace "+ 15 siblings" with "+ 4 siblings" or drop count.

### [PR-33-A2_04] C01 doc claim "rescues util from libcxx-at-tail to libcxx-at-front" empirically incorrect for util's matching CC nodes
**Status:** resolved (deferred — minor doc accuracy)
**Severity:** minor
**Location:** gen.go:1318-1330
**Description:** util's 4 matching own-CC nodes had libcxx/libcxxrt at FRONT in PR-33 round-1 baseline already (via util's user-PEERDIRs as Phase 2 contributors). C01 hoist is empirically a NO-OP for those. What it actually rescues is more subtle (libcxxabi/libunwind ordering when those slots are present).
**Fix:** Deferred. Replace example with actual modules whose L3 fingerprint flipped due to hoist; or remove the named-example clause.

### [PR-33-A2_05] tools/archiver/main.cpp.o has tcmalloc misordered ahead of zlib/double-conversion/libc_compat
**Status:** resolved (deferred — pre-existing peer-walk ordering issue, separate PR)
**Severity:** minor
**Location:** gen.go (genModule peer-walk Phase 1+2)
**Description:** Reference archiver/main.cpp.o slots [17-21] = `zlib/include, double-conversion, libc_compat/.../readpassphrase, tcmalloc, abseil-cpp`. Got = `tcmalloc, zlib/include, double-conversion, libc_compat, abseil-cpp`. tcmalloc misordered to slot 17 instead of 20. archiver is NOT runtime-ancestor (C01 doesn't fire) and IS PROGRAM (C02 only added -D_musl_=1). Pre-existing peer-walk Phase 1+2 ordering issue; 1 L3 mismatch in M2 closure.
**Fix:** Deferred. Trace tcmalloc's entry path; reference puts it in user-PEERDIR-tail region.

### [PR-33-A2_06 / C2_03] ragel6 host PIC duplicated linux-headers pair (slots 18-19) + mimalloc/ragel5 ordering
**Status:** open
**Severity:** minor
**Location:** gen.go peer-walk dedup logic for ragel6/bin host walks
**Description:** ragel6/all_cd.cpp.pic.o reference slots [11-18]: libcxx/include, libcxxrt/include, musl/{arch/x86_64,arch/generic,include,extra}, mimalloc/include, ragel5/aapl. Got: same through musl, then ragel5/aapl, then DUPLICATE linux-headers + linux-headers/_nf, then mimalloc/include. Two issues: (a) mimalloc-vs-ragel5/aapl ordering reversed; (b) linux-headers re-emitted at slots 18-19 (over-emit by us). Pushes -D_musl_=1 from slot 51 to 53 (offset +2). 9 ragel6 host-PIC CC nodes affected.
**Suggested fix:** Locate duplicate-include emission site for host PIC ragel6 (likely peer-ADDINCL accumulator without dedup against bundle's already-emitted linux-headers). Standard fix: dedup `-I` flags against already-emitted set.

### [PR-33-A2_07] Header-only walker lacks C01 hoist gate; assumption unenforced
**Status:** resolved (deferred — minor; M2-empirical-safe)
**Severity:** minor
**Location:** gen.go:1774-1782
**Description:** walkPeersForGlobalAddIncl note says "header-only LIBRARYs (musl/include, etc.) keep natural Phase 1+2 order — none of M2-closure header-only modules are runtime ancestors". True today; but unenforced empirical assumption. Future closure pulling header-only runtime ancestor through header-only walker silently fails discriminator.
**Fix:** Deferred. Either guard panics on assumption violation, or apply hoist unconditionally in header-only walker too (symmetric).

### [PR-33-A2_08] C02 effectiveNoPlatform gate not exercised by M2 closure
**Status:** resolved (deferred — nit; empirically unverifiable in M2)
**Severity:** nit
**Location:** gen.go:1464
**Description:** C02 gate `!effectiveNoPlatform(instance.Flags)` excludes PROGRAMs with NO_PLATFORM. No PROGRAM in M2 archiver closure has effectiveNoPlatform=true. Cannot falsify from M2 evidence.
**Fix:** Deferred. Document as empirical boundary in C02 comment; verify against M3+ reference when it arrives.

### [PR-33-C2_02] Round-2 lift accounting was estimates not measurements (real: C01=19 util, C02=79 yasm, ragel6=0; sum exactly 98)
**Status:** resolved (correction noted; no code action)
**Severity:** minor
**Description:** Brief said "C01 lifted ~25 (util+ragel6); C02 lifted ~41 (yasm); discrepancy 32 unaccounted". Empirical: C01 lifted 19 util ONLY (ragel6 untouched); C02 lifted 79 yasm; sum exactly 98, no discrepancy. ZERO match→miss regressions.
**Fix:** Documented in PR-33 Completed entry with correct numbers.

### [PR-33-C2_04] PR-33-C04 round-1 hypothesis "libcxxrt own non-GLOBAL CXXFLAGS doubled" is empirically wrong — actual is missing `-nostdinc++` (1-arg shortfall)
**Status:** resolved (correction; underlying L3 mismatch is real but characterization was inverted)
**Severity:** minor
**Location:** Earlier C04 ledger entry; affected: cc.go cxx-tail composition for libcxxrt
**Description:** Round-1 hypothesis: "libcxxrt own non-GLOBAL CXXFLAGS doubled in reference". Empirical: libcxxrt CC nodes have want=110 args, got=109 — 1-arg SHORTFALL, not doubling. Missing flag is `-nostdinc++` at slot 102. 14 nodes (not 29).
**Fix:** Correction noted. Underlying defect is "libcxxrt CC missing `-nostdinc++` in cxx-tail composition" — fix is to add `-nostdinc++` to libcxxrt's own-CXXFLAGS or dedicated freestanding-cxx tail block.

### [PR-33-C2_05] util/system/compiler.cpp.o unpaired — reference emits it, our walker doesn't
**Status:** resolved (deferred — pre-existing parser/walker gap, separate PR)
**Severity:** minor
**Location:** gen.go walker; tools/archiver/main.cpp → util PEERDIR closure
**Description:** Among 47 unpaired-want nodes is `$(BUILD_ROOT)/util/system/compiler.cpp.o` (default-linux-aarch64). Pre-PR-33. Knock-on: util/libyutil.a L3-divergent (got 31 inputs, want 32; AR slot 10 holds next file alphabetically rather than compiler.cpp.o). 2 of 4 remaining util L3 misses collapse to same root.
**Fix:** Deferred. Investigate why walker omits util/system/compiler.cpp from util module's source list.

### [PR-33-C2_06] util target AS uses stripped warning bundle instead of preserving module-own warning CFLAGS
**Status:** resolved (deferred — AS-emitter retrofit, separate PR)
**Severity:** minor
**Location:** as.go:86 (asWnoEverything substitution); gen.go ADDINCL/CFLAGS threading for AS-kind sources
**Description:** util/_/system/context_aarch64.S.o want has 106 cmd_args including module-own warnings (-Werror, -Wall, -Wextra, -Wno-parentheses, etc.); got has 86 with `-Wno-everything` substitution. AS emitter doesn't thread module ADDINCL/CFLAGS the way CC does. 1-node defect in M2 but cascades for AS-kind misses generally.
**Fix:** Deferred. AS-emitter retrofit to thread module-own warning CFLAGS — sized as own PR.

### [PR-33-C2_07] util R6 path mismatch: ragel6/bin/ragel6 vs ragel6/ragel6
**Status:** resolved (deferred — single-node R6 path resolution, separate PR)
**Severity:** minor
**Location:** r6.go ragel6 binary path resolution
**Description:** Reference want: `$(BUILD_ROOT)/contrib/tools/ragel6/ragel6`. Got: `$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6`. Single-node defect; util/_/datetime/parser.rl6.cpp R6 node cmd[0][0] differs.
**Fix:** Deferred. Drop `/bin/` segment from R6 binary path resolution.

---

## PR-37

### [PR-37-D01] cc.go change is uncommitted in the executor's worktree
**Status:** resolved (orchestrator committed at merge)
**Severity:** minor
**Location:** worktree `worktree-agent-a53ec33a36efdb34b` working tree (since GC'd)
**Description:** At review time, `git status` reported `cc.go` as modified-not-staged on the executor's worktree branch. The branch HEAD was 5 commits behind master, so a direct cherry-pick of the branch would not have carried the PR-37 change.
**Fix:** Orchestrator copied `cc.go` from the worktree's working tree onto the main checkout, re-ran the build/test/comparator on master, and committed PR-37 as a single commit including the ledger updates. The worktree branch is left for runtime GC.

### [PR-37-D02] composeHostCC / composeMuslCC / composeMuslHostCC retain old C-source slot ordering
**Status:** resolved (deferred to M3+ — no M2 module triggers it)
**Severity:** nit
**Location:** cc.go::composeHostCC (~L846), composeMuslCC (~L894), composeMuslHostCC (~L945)
**Description:** The trailer-slot fix was applied only to `composeTargetCC`. The three sibling composers still emit `ownExtras` via `appendCxxStdAndOwn` at the pre-`builtinMacroDateTime` slot for C sources. M2 closure does not exercise these paths for C sources with own CONLYFLAGS (reviewer audited: 1634 host-axis C-source CCs scanned, zero with late `-std=c*`/`-march`, zero L3 mismatches). The defect is latent — any M3+ module with a HOST C-source carrying CONLYFLAGS, or a musl C-source carrying its own CONLYFLAGS, will reproduce the L3-A slot bug on its axis.
**Root cause:** `composeTargetCC` patched in isolation; the four composers' trailer ordering is structurally identical but the fix wasn't propagated.
**Fix:** Deferred. When an M3+ module surfaces the trigger, propagate the same `if isCxx { appendCxxStdAndOwn } else { cOnlyExtras = ownExtras }` split + post-`perSrcCFlags`/post-`macroPrefixMapFlags` append to the affected composer. (Note: `composeMuslCC`/`composeMuslHostCC` lack a `perSrcCFlags` slot today; trailer position there is just after `macroPrefixMapFlags`.)

### [PR-37-D03] No regression-pin unit test for the new trailer-slot ordering
**Status:** resolved (deferred — M1 byte-exact pin and full archiver L3 comparison cover regression)
**Severity:** nit
**Location:** cc_test.go (no new test)
**Description:** Existing `TestEmitCC_COnlyFlags_AppliesOnlyToCSources` asserts only PRESENCE of CONLYFLAGS, not SLOT POSITION. The M1 byte-exact pin (`build/cow/on/lib.c.o`) does not exercise the new trailer because that module has zero CONLYFLAGS. A future refactor in `composeTargetCC` that shuffles the `cOnlyExtras` slot would not be caught by unit tests — only by re-running the full `tools/archiver` L3 comparator.
**Fix:** Deferred. When a future PR next touches `composeTargetCC` slot ordering, add an assertion like `idx("-std=c11") > idx("-fmacro-prefix-map=$(TOOL_ROOT)/=")` and `idx("-std=c11") < idx("$(SOURCE_ROOT)/lib.c")` to pin the trailer slot semantically.

### [PR-37-D04] Style and comment-quality nits in the patched block
**Status:** resolved (deferred — cosmetic, low payoff)
**Severity:** nit
**Location:** cc.go L754-790 (the patched block)
**Description:** Three minor issues:
(a) Missing blank line before the new `if isCxx {` block (STYLE.md "blank line before `if`"; borderline because the new `var cOnlyExtras []string` is logically part of the same operation).
(b) `appendCxxStdAndOwn(cmdArgs, true, noCompilerWarnings, true, ownExtras)` hardcodes the first `true` literal; using the variable `isCxx` (which is guaranteed `true` inside the guarded branch) would keep the C++ branch a textual no-op vs master, easier to audit. Hardcoding `true` is gratuitous.
(c) The second new comment ("PR-37: C-source CONLYFLAGS trail after macroPrefixMapFlags...") substantially duplicates the L755-760 comment block; STYLE.md "minimal new comments" suggests trimming.
**Fix:** Deferred (cosmetic). Apply when next editing `composeTargetCC`.

---

## PR-39

### [PR-36-D01] sameDirAbs skips normalisePath on the empty-incDir branch
**Status:** resolved (deferred — latent edge case unreachable from M2 closure)
**Severity:** nit
**Location:** `scanner.go:1141-1149` (the `sameDirAbs` construction inside the PR-36 gate)
**Description:** When the includer lives at the source root (`incDir == ""`), the gate sets `sameDirAbs = s.sourceRootSlash + d.target` *without* calling `normalisePath`. By contrast, `resolveSearchPath`'s same-dir candidate (scanner.go:1420-1432) always routes through `addPath`, which calls `normalisePath(rel)` before constructing the absolute path. If a root-level source emits `#include "./foo.h"` or `#include "../bar.h"`, `resolveSearchPath` produces `<root>/foo.h` / `<root>/bar.h`, but the gate's `sameDirAbs` produces `<root>/./foo.h` / `<root>/../bar.h`. The equality test then yields `false`, the gate fails to fire, and a multi-target sysincl bypass triggers spuriously. The cluster fix doesn't hit this case (libcxxabi-parts / libunwind sources have non-empty `incDir`), so the M2 closure is unaffected.
**Suggested fix:** Mirror `resolveSearchPath`'s normalisation in both branches: `candidate := d.target; if incDir != "" { candidate = incDir + "/" + d.target }; sameDirAbs := s.sourceRootSlash + normalisePath(candidate)`.

### [PR-36-D02] hasMultiTarget across union of two single-target halves not detected
**Status:** resolved (deferred to follow-up cleanup PR — structural fragility, not active in M2)
**Severity:** minor
**Location:** `scanner.go:1252-1279` (`sysinclLookup`) + `sysincl.go:290-339` / `sysincl.go:360-411` (per-half multi-target detection)
**Description:** `hasMultiTarget` is computed *per-record* (record flag ∧ header has ≥ 2 paths in that record's Mappings). The two halves' results then OR together: `hasMultiTarget = srcMT || incMT`. This misses the case where (a) one source-keyed record contributes exactly one path for `target`, (b) one includer-keyed record also contributes exactly one *different* path for `target`, and (c) the union therefore has 2 paths but neither half reports `multiTarget=true`. The gate would suppress the sysincl layer even though, by the multi-target spirit of the PR-36 invariant, both alternates should contribute. M2 isn't exhibiting this case (L2=99.79% matches expected); the brief's L2-B example happens to be safe because the includer-keyed record is itself multi-target. Adding a new single-target source-keyed YAML record that pairs with an existing single-target includer-keyed record would silently mis-gate.
**Suggested fix:** Compute `hasMultiTarget` from the size of the *unioned* result rather than from per-record flags. After the dedup loop, set `hasMultiTarget = len(out) >= 2` (and pass that through to the caller). Drop the per-record `HasMultiTarget` flag and the per-target re-check inside `LookupSourceKeyed`/`LookupIncluderKeyed`; `len(out) >= 2` after dedup is a strictly weaker invariant that captures the same intent without the cross-record blind spot.

### [PR-36-D03] PR-36 invariant block-comment understates the path-vs-tier discriminator
**Status:** resolved (deferred — comment cleanup, low payoff)
**Severity:** nit
**Location:** `scanner.go:1123-1139`
**Description:** Comment says "if the file was found via a SEARCH-PATH TIER OTHER THAN same-dir (i.e. OwnAddIncl / PeerAddIncl / BaseSearch) AND the sysincl result is multi-target, the gate is bypassed." This is true for the *current implementation*, but the *operational* discriminator is `searchOut[0] == sameDirAbs`, which only inspects the resolved PATH — not which tier resolved it. If OwnAddIncl happens to point at the same directory as the includer (e.g. `OwnAddIncl=["src"]` for an includer also under `src/`), and the same-dir file doesn't exist but the OwnAddIncl path does, `searchOut[0]` equals `sameDirAbs` and the gate fires *even though the resolution came from a tier the comment lists as bypassable*. Functionally fine (path-shape equivalence is what matters); the comment will mislead the next maintainer.
**Suggested fix:** Reword the comment to describe the actual discriminator: "If the resolved path equals `incDir/d.target`, regardless of which search tier produced it, the gate fires; otherwise (path differs from same-dir candidate) the multi-target case bypasses."

---

## PR-39

### [PR-39-D01] PR-32 D02 "SOLE remaining musl-path-prefix dispatch" comment is now stale
**Status:** resolved (deferred — refresh comment when next editing the block)
**Severity:** nit
**Location:** `module.go:176-181`
**Description:** The comment block above the musl-path-prefix branch in `inferFlagsFromPath` asserts that this test is "the SOLE remaining musl-path-prefix dispatch in the codebase — every other call site reads `Flags.LibcMusl` instead." PR-39 introduces a second musl-path-equality dispatch at `gen.go:1984` (the `-D_musl_=1` re-injection for `contrib/libs/musl/full`). The "SOLE remaining" claim is therefore false and risks misleading future readers / subagents who use this comment as a roadmap of where the M5 removal touches.
**Root cause:** PR-39 added a sibling dispatch at `gen.go:1984` and a new "PR-39:" comment block in `module.go` (below the PR-32 D02 comment), but did NOT update the surviving "SOLE remaining" phrase in the older PR-32 D02 block.
**Fix:** Deferred per CLAUDE.md "Do not edit the code yourself, even for trivial fixes". When a future PR next touches this comment block, replace "is the SOLE remaining musl-path-prefix dispatch in the codebase" with a short list of remaining sites, e.g. "is one of two musl-path dispatches remaining (the other is the `-D_musl_=1` injection for `contrib/libs/musl/full` at `gen.go:1984`); both are M5+-removable together."
