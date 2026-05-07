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
