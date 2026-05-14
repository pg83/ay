package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripComments_BlockCommentInclude pins PR-35u's primary motivator:
// `#include <iostream>` inside a `/* ... */` block must not survive the
// strip pass. The shape mirrors the upstream
// `contrib/libs/cxxsupp/libcxx/include/__charconv/from_chars_integral.h:156-166`
// snippet that floods the M2 closure with phantom <iostream>/<format>
// when the regex picks the include up.
func TestStripComments_BlockCommentInclude(t *testing.T) {
	in := []byte(`#include <real.h>
/*
 * Code used to generate the LUT.
 * #include <cmath>
 * #include <format>
 * #include <iostream>
 */
#include <other.h>`)

	out := stripComments(append([]byte(nil), in...))

	// The pre-comment include must survive intact; the post-comment one
	// too. The body of the comment must NOT contain `#include` substrings.
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("pre-comment #include lost: %q", s)
	}

	if !strings.Contains(s, "#include <other.h>") {
		t.Errorf("post-comment #include lost: %q", s)
	}

	for _, ghost := range []string{"<cmath>", "<format>", "<iostream>"} {
		if strings.Contains(s, ghost) {
			t.Errorf("ghost include %q survived block-comment strip; output:\n%s", ghost, s)
		}
	}

	// Newline count must equal the input — per-line `^\s*#` anchoring
	// depends on line numbers staying aligned through stripped spans.
	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

// TestStripComments_LineCommentInclude pins line-comment stripping.
// `// #include <ghost>` after real code on the same line must lose
// the ghost; the real include on a separate line is unaffected.
func TestStripComments_LineCommentInclude(t *testing.T) {
	in := []byte(`#include <real.h> // and not #include <ghost.h>
int main() {} // unrelated #include <also-ghost.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("real include lost: %q", s)
	}

	if strings.Contains(s, "<ghost.h>") || strings.Contains(s, "<also-ghost.h>") {
		t.Errorf("line-comment ghost survived: %q", s)
	}
}

// TestStripComments_StringLiteralTransparent pins PR-35u's "strings
// are transparent, not stripped" rule. The bytes of a string literal
// stay unchanged so that the include directive's quoted form
// `#include "header.h"` survives intact (early prototype that stripped
// string payloads erased every quoted include in the M2 closure).
// What the string layer DOES guarantee: a `/*` inside a string body
// must NOT enter block-comment state, so following code/comments are
// scanned correctly.
func TestStripComments_StringLiteralTransparent(t *testing.T) {
	in := []byte(`#include "syscall.h"
const char *msg = "this /* is not a comment */ end";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, `#include "syscall.h"`) {
		t.Errorf("quoted #include lost: %q", s)
	}

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-string #include lost: %q", s)
	}

	// The `/* is not a comment */` is a string-literal substring.
	// It MUST appear unmodified in the output — the string-literal
	// state has to recognise the surrounding `"..."` so it does not
	// enter block-comment state on the embedded `/*`.
	if !strings.Contains(s, "/* is not a comment */") {
		t.Errorf("string-literal contents modified or block-comment state entered: %q", s)
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

// TestStripComments_StringEscapedQuote pins escape handling — `\"`
// inside a `"..."` does NOT terminate the string. A `/*` AFTER the
// apparent escape (but inside the still-open string) must not enter
// block-comment state.
func TestStripComments_StringEscapedQuote(t *testing.T) {
	in := []byte(`const char *s = "a \" /* still in string */ end";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-string #include lost: %q", s)
	}

	// The string body is preserved (transparent). The `/* still in string */`
	// must NOT have been treated as a real block comment.
	if !strings.Contains(s, "/* still in string */") {
		t.Errorf("escape-quote handling broke: string body modified or comment state entered: %q", s)
	}
}

// TestStripComments_RawStringLiteral exercises the C++11 raw-string
// form `R"delim(...)delim"`. The body of a raw string can contain
// unescaped `"` and `\`, so the regular double-quoted state machine
// would mishandle it. The body bytes stay transparent (unchanged);
// the only contract is that an unescaped `/*` or `//` inside a raw
// body does NOT enter comment state.
func TestStripComments_RawStringLiteral(t *testing.T) {
	in := []byte(`const char *s = R"py(
no escape needed: " or \ — /* not a real block comment */ done
)py";
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-raw-string #include lost: %q", s)
	}

	// Raw-string body is blanked (non-newline bytes → spaces) so a
	// fake `#include` at the start of an inner line — common in
	// protoc-style `p->Emit(R"(#include "$path$")")` codegen
	// templates — never reaches parseCIncludes. The "not a real
	// block comment" body bytes must therefore NOT survive.
	if strings.Contains(s, "not a real block comment") {
		t.Errorf("raw-string body bytes survived; expected blanked: %q", s)
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
	}
}

// TestStripComments_RawStringFakeIncludeBlanked pins the protoc-template
// motivator: a `#include "X"` at the start of a line INSIDE a raw
// string body must be blanked so parseCIncludes doesn't pick it up.
func TestStripComments_RawStringFakeIncludeBlanked(t *testing.T) {
	in := []byte(`p->Emit(R"(
#include "$path$"
)");
#include <real.h>
`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	if !strings.Contains(s, "#include <real.h>") {
		t.Errorf("post-raw-string #include lost: %q", s)
	}

	if strings.Contains(s, `#include "$path$"`) {
		t.Errorf("raw-string fake #include survived blanking: %q", s)
	}
}

// TestStripComments_QuotedIncludeSurvives is the hard pin against the
// regression that motivated this test in the first place: the very
// common shape `#include "syscall.h"` must round-trip the strip pass
// unchanged. parseIncludes' regex relies on the closing `"` being
// present at the captured target span.
func TestStripComments_QuotedIncludeSurvives(t *testing.T) {
	in := []byte("#include <sys/sendfile.h>\n#include \"syscall.h\"\n")

	out := stripComments(append([]byte(nil), in...))

	if string(out) != string(in) {
		t.Errorf("quoted include altered:\n got: %q\nwant: %q", out, in)
	}
}

// TestStripComments_NestedBlockCommentNotAttempted documents that the
// strip pass treats `/* */` as the only block-comment shape — C/C++
// has no nested block comments by spec. A `/* outer /* inner */ rest */`
// closes at the FIRST `*/`, leaving ` rest */` outside the comment.
// This pins the simple state-machine behaviour against accidental
// drift toward GCC's `-Wcomment` "nested" warning territory.
func TestStripComments_NestedBlockCommentNotAttempted(t *testing.T) {
	in := []byte(`/* outer /* inner */ rest */`)

	out := stripComments(append([]byte(nil), in...))
	s := string(out)

	// First `*/` closes the comment. ` rest */` remains as code (the
	// trailing `*/` is unmatched but the strip pass does not flag it).
	if !strings.Contains(s, "rest") {
		t.Errorf("post-first-`*/` code lost: %q", s)
	}

	if strings.Contains(s, "outer") || strings.Contains(s, "inner") {
		t.Errorf("inside-comment text survived: %q", s)
	}
}

// TestStripComments_NoTriggers asserts the fast-path: a buffer with no
// `/`, `"`, or `'` is returned unchanged (same backing slice). This is
// performance-critical because every parseIncludes call runs the
// pre-scan; for the rare trigger-free file (mostly empty headers and
// pure preprocessor token files) the loop should never engage.
func TestStripComments_NoTriggers(t *testing.T) {
	in := []byte("#include <abc>\n#include <def>\n")
	out := stripComments(in)

	if &in[0] != &out[0] {
		t.Errorf("trigger-free fast path allocated: in=%p out=%p", &in[0], &out[0])
	}
}

// TestStripComments_DivisionOperatorNotMistaken pins discrimination
// between division (`a / b`) and the start of a comment. `/` followed
// by anything other than `/` or `*` is plain code.
func TestStripComments_DivisionOperatorNotMistaken(t *testing.T) {
	in := []byte("int x = a / b;\n#include <real.h>\n")
	out := stripComments(append([]byte(nil), in...))

	if string(out) != string(in) {
		t.Errorf("division operator misread as comment:\n got: %q\nwant: %q", out, in)
	}
}

// TestScanner_BlockCommentIncludeIgnored exercises parseIncludes
// end-to-end through stripComments. A header with a real `#include`
// plus a block-comment-buried `#include` must produce ONLY the real
// directive in the parsed list.
func TestScanner_BlockCommentIncludeIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.h")

	body := []byte(`#include <real.h>
/*
 * Code used to generate the table.
 * #include <iostream>
 * #include <format>
 */
`)

	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write fake.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})

	// PR-M3-vfs-paths: parseIncludes takes a VFS path. The test file
	// `<dir>/fake.h` becomes `$(S)/fake.h` under the scanner's
	// dir-as-sourceRoot.
	dirs := scanner.parseIncludes(Source("fake.h"))

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; directives=%+v", len(dirs), dirs)
	}

	if dirs[0].target != "real.h" {
		t.Errorf("directive target = %q, want %q", dirs[0].target, "real.h")
	}
}

// TestScanner_IncludeNextSuppressed pins the PR-35e fix: the scanner
// does NOT follow `#include_next` directives through sysincl OR the
// search path. The directive in libcxx's shadow-header pattern (e.g.
// __mbstate_t.h's `#elif __has_include_next(<uchar.h>)`) lives in an
// `#elif` branch the live preprocessor never takes when
// `_LIBCPP_HAS_MUSL_LIBC` is set; following it text-blindly drove the
// dominant L2-ceiling over-fan-out documented at PR-31-D08 / PR-33-C03.
//
// Two byte-exact checks against the production tree:
//
//   - libcxx-source CC consumer (`libcxx/src/algorithm.cpp`): closure
//     must contain neither `libcxx/include/uchar.h` nor
//     `musl/include/uchar.h`. Reference parity verified against
//     `sg.json` for the same file.
//   - JS-derived CC consumer (`util/charset/all_charset.cpp`, the
//     join-srcs output for util/charset): same uchar.h absence.
//
// Both checks skip when the production tree is not present.
func TestScanner_IncludeNextSuppressed(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	sysincl := LoadSysInclSet(sourceRoot, func(Warn) {})
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	// libcxx-source case. The ScanContext mirrors what gen.go's
	// `walkClosure` constructs for a libcxx CC consumer:
	// libcxx/include is in OwnAddIncl (the libcxx module's own
	// ADDINCL). musl/include is in BaseSearchPaths (the cc bundle's
	// implicit -I set).
	libcxxCtx := ScanContext{
		SourceRel: "contrib/libs/cxxsupp/libcxx/src/algorithm.cpp",
		OwnAddIncl: []string{
			"contrib/libs/cxxsupp/libcxx/include",
		},
		BaseSearchPaths: []string{
			"contrib/libs/musl/include",
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/linux-headers",
			"",
		},
	}

	closure := scanner.WalkClosure(libcxxCtx)

	if len(closure) == 0 {
		t.Fatalf("libcxx-source closure unexpectedly empty (source absent or include scan misconfigured)")
	}

	for _, p := range closure {
		if strings.HasSuffix(p.String(), "/libcxx/include/uchar.h") {
			t.Errorf("libcxx-source closure contains spurious libcxx-uchar: %s (PR-35e regression)", p)
		}

		if strings.HasSuffix(p.String(), "/musl/include/uchar.h") {
			t.Errorf("libcxx-source closure contains spurious musl-uchar: %s (PR-35e regression)", p)
		}
	}

	// Sanity: the closure must still include __mbstate_t.h itself
	// (the includer of the suppressed `#include_next <uchar.h>`); we
	// only suppress the chain past the directive, not the includer.
	foundMbstate := false

	for _, p := range closure {
		if strings.HasSuffix(p.String(), "/libcxx/include/__mbstate_t.h") {
			foundMbstate = true

			break
		}
	}

	if !foundMbstate {
		t.Errorf("libcxx-source closure missing __mbstate_t.h (regression beyond PR-35e scope)")
	}
}

// TestScanner_RegularIncludeStillResolvesViaSysincl pins the
// converse: a REGULAR `#include <X.h>` (not `#include_next`) must
// still resolve through the sysincl chain. cstring's regular
// `#include <string.h>` from a libcxx-source compile unit must
// produce BOTH libcxx-string.h AND musl-string.h via the
// stl-to-libcxx and libc-to-musl records respectively. PR-35e
// preserves this behaviour; the suppression only affects the
// `next: true` branch.
func TestScanner_RegularIncludeStillResolvesViaSysincl(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	sysincl := LoadSysInclSet(sourceRoot, func(Warn) {})
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	libcxxCtx := ScanContext{
		SourceRel: "contrib/libs/cxxsupp/libcxx/src/algorithm.cpp",
		OwnAddIncl: []string{
			"contrib/libs/cxxsupp/libcxx/include",
		},
		BaseSearchPaths: []string{
			"contrib/libs/musl/include",
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/linux-headers",
			"",
		},
	}

	closure := scanner.WalkClosure(libcxxCtx)

	hasLibcxxString := false
	hasMuslString := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxx/include/string.h"):
			hasLibcxxString = true
		case strings.HasSuffix(p.String(), "/musl/include/string.h"):
			hasMuslString = true
		}
	}

	if !hasLibcxxString {
		t.Errorf("libcxx-source closure missing libcxx-string.h (cstring → <string.h> sysincl chain broken)")
	}

	if !hasMuslString {
		t.Errorf("libcxx-source closure missing musl-string.h (cstring → <string.h> sysincl chain broken)")
	}
}

// TestScanner_SubgraphCacheReuse pins the PR-34r per-includer subgraph
// cache contract: running WalkClosure twice on the SAME source returns
// byte-identical closures, and the second run hits the cache for every
// header the first run computed (zero new misses for the second source).
// Also exercised via two DIFFERENT sources whose ScanContext shares the
// same OwnAddIncl/PeerAddInclSet/BaseSearchPaths AND the same source-
// keyed sysincl equivalence class — those sources must share cached
// subgraphs for every header reached via either.
//
// The cache is keyed by `(headerAbs, ctxHash, srcClassHash)`; two
// libcxx-source compiles with identical ADDINCL configuration land in
// the same equivalence class because their `activeSourceKeyed` records
// (PerSourceView) match pointer-for-pointer.
func TestScanner_SubgraphCacheReuse(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	sysincl := LoadSysInclSet(sourceRoot, func(Warn) {})
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	makeCtx := func(srcRel string) ScanContext {
		return ScanContext{
			SourceRel: srcRel,
			OwnAddIncl: []string{
				"contrib/libs/cxxsupp/libcxx/include",
			},
			BaseSearchPaths: []string{
				"contrib/libs/musl/include",
				"contrib/libs/musl/arch/aarch64",
				"contrib/libs/musl/arch/generic",
				"contrib/libs/linux-headers",
				"",
			},
		}
	}

	closure1 := scanner.WalkClosure(makeCtx("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"))

	if len(closure1) == 0 {
		t.Fatalf("first closure unexpectedly empty (source absent or scan misconfigured)")
	}

	hits1, misses1, _ := scanner.SubgraphCacheStats()

	// Second walk on a DIFFERENT source in the same equivalence class.
	// Many of the headers the first walk computed should be reused —
	// hits should grow significantly while misses grow only by the
	// new-source's own previously-unseen headers (typically a small
	// number when the two sources share most of libcxx's transitive
	// closure).
	closure2 := scanner.WalkClosure(makeCtx("contrib/libs/cxxsupp/libcxx/src/string.cpp"))

	if len(closure2) == 0 {
		t.Fatalf("second closure unexpectedly empty")
	}

	hits2, misses2, _ := scanner.SubgraphCacheStats()

	hitsDelta := hits2 - hits1
	missesDelta := misses2 - misses1

	if hitsDelta == 0 {
		t.Errorf("second-walk hits delta is 0 — cache not reused across libcxx-source compiles "+
			"(hits1=%d hits2=%d misses1=%d misses2=%d)",
			hits1, hits2, misses1, misses2)
	}

	if hitsDelta < missesDelta {
		t.Errorf("second-walk hits delta (%d) less than misses delta (%d) — "+
			"cross-source cache reuse is below 50%% (PR-34r ≥30%% gate at risk)",
			hitsDelta, missesDelta)
	}

	// Re-walk the FIRST source. Should produce the same closure, with
	// effectively zero new misses — every header it touched is now
	// cached. The walk's hit count grows; miss count stays roughly
	// constant.
	hitsBefore3, missesBefore3, _ := scanner.SubgraphCacheStats()
	closure3 := scanner.WalkClosure(makeCtx("contrib/libs/cxxsupp/libcxx/src/algorithm.cpp"))
	hits3, misses3, _ := scanner.SubgraphCacheStats()

	if len(closure3) != len(closure1) {
		t.Errorf("re-walk closure length differs: first=%d third=%d", len(closure1), len(closure3))
	}

	for i, p := range closure3 {
		if i >= len(closure1) {
			break
		}

		if p != closure1[i] {
			t.Errorf("re-walk diverges at index %d: first=%q third=%q", i, closure1[i], p)

			break
		}
	}

	missesAcrossThird := misses3 - missesBefore3

	if missesAcrossThird > 5 {
		t.Errorf("re-walk added %d new misses — cache is not durable across repeat WalkClosure on the same key "+
			"(hits delta=%d)", missesAcrossThird, hits3-hitsBefore3)
	}
}

// TestScanner_QuotedSysinclGated_LocalResolved pins the PR-35w
// gate: a quoted include `#include "foo.h"` whose local search-path
// resolution succeeded MUST NOT pick up the matching sysincl record's
// alternate path. Quoted-form is a project-local include; the upstream
// ymake scanner only consults sysincl alternates when the search-path
// resolution fails. The text-blind union appended musl/libc/libcxxrt
// alternates on top of legitimate local resolutions, producing 34
// L2-divergent pairs in the M2 closure (R3 elf.h-style + R5
// unwind.h-quoted-self subset).
//
// The synthetic tree mirrors the elf.h shape: yasm/elf.h exists and
// is the legitimate target of `#include "elf.h"` from yasm/source.cpp;
// sysincl maps `elf.h → musl/include/elf.h`. With the gate, the
// scanner returns ONLY yasm/elf.h. Without the gate the closure would
// also contain musl/include/elf.h — the exact L2-divergent
// over-emission this PR closes.
func TestScanner_QuotedSysinclGated_LocalResolved(t *testing.T) {
	dir := t.TempDir()

	mkdirs := []string{"yasm", "musl/include"}

	for _, p := range mkdirs {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	src := []byte(`#include "elf.h"
`)

	if err := os.WriteFile(filepath.Join(dir, "yasm/source.cpp"), src, 0o644); err != nil {
		t.Fatalf("write source.cpp: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "yasm/elf.h"), []byte("// local elf.h\n"), 0o644); err != nil {
		t.Fatalf("write yasm/elf.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "musl/include/elf.h"), []byte("// musl elf.h\n"), 0o644); err != nil {
		t.Fatalf("write musl/include/elf.h: %v", err)
	}

	// Hand-build a sysincl set with one record: header `elf.h` maps to
	// `musl/include/elf.h`. KeyBySource=false + nil filter so the
	// record matches every (source, includer) pair.
	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"elf.h": {"musl/include/elf.h"},
			},
		},
	}

	scanner := NewIncludeScanner(dir, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "yasm/source.cpp",
	})

	hasLocal := false
	hasMusl := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/yasm/elf.h"):
			hasLocal = true
		case strings.HasSuffix(p.String(), "/musl/include/elf.h"):
			hasMusl = true
		}
	}

	if !hasLocal {
		t.Errorf("closure missing local yasm/elf.h (search-path resolution broken): %v", closure)
	}

	if hasMusl {
		t.Errorf("closure contains spurious musl/include/elf.h — PR-35w gate failed to suppress sysincl on locally-resolved quoted include: %v", closure)
	}
}

// TestScanner_QuotedMultiTargetSysincl_OwnAddIncl pins the PR-36 fix:
// a quoted include `#include "cxxabi.h"` resolved via OwnAddIncl (not
// the same directory as the includer) MUST pick up sysincl multi-target
// alternates. This mirrors the libcxxabi-parts pattern:
//   - Source: libcxxabi-parts/src/abort_message.cpp
//   - `abort_message.h` does `#include "cxxabi.h"` (quoted)
//   - OwnAddIncl=libcxxabi/include → finds libcxxabi/include/cxxabi.h
//   - stl-to-libcxx.yml maps cxxabi.h to BOTH libcxxabi/include/cxxabi.h
//     AND libcxxrt/include/cxxabi.h (multi-target, ≥ 2 paths)
//   - Reference graph includes both — the PR-35w gate was too aggressive.
//
// The synthetic tree: `src/` holds the source and header; `libcxxabi/include/`
// holds the "local" cxxabi.h (via OwnAddIncl); `libcxxrt/include/` holds the
// sysincl alternate. The sysincl record is multi-target (2 non-empty paths).
// With the PR-36 fix, both paths appear in the closure. Without it, only
// `libcxxabi/include/cxxabi.h` would appear (gate short-circuits sysincl).
func TestScanner_QuotedMultiTargetSysincl_OwnAddIncl(t *testing.T) {
	dir := t.TempDir()

	mkdirs := []string{
		"src",
		"libcxxabi/include",
		"libcxxrt/include",
	}

	for _, p := range mkdirs {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	// src/header.h does `#include "cxxabi.h"` (quoted). Same-dir
	// `src/cxxabi.h` does NOT exist — forces OwnAddIncl resolution.
	header := []byte(`#include "cxxabi.h"
`)

	src := []byte(`#include "header.h"
`)

	if err := os.WriteFile(filepath.Join(dir, "src/header.h"), header, 0o644); err != nil {
		t.Fatalf("write header.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "src/source.cpp"), src, 0o644); err != nil {
		t.Fatalf("write source.cpp: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libcxxabi/include/cxxabi.h"), []byte("// libcxxabi cxxabi.h\n"), 0o644); err != nil {
		t.Fatalf("write libcxxabi/include/cxxabi.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/include/cxxabi.h"), []byte("// libcxxrt cxxabi.h\n"), 0o644); err != nil {
		t.Fatalf("write libcxxrt/include/cxxabi.h: %v", err)
	}

	// Multi-target sysincl: cxxabi.h maps to BOTH libcxxabi and libcxxrt.
	// HasMultiTarget must be set explicitly on hand-built records (YAML
	// loading sets it automatically via parseSysInclYAML's flushRecord).
	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			Mappings: map[string][]string{
				"cxxabi.h": {
					"libcxxabi/include/cxxabi.h",
					"libcxxrt/include/cxxabi.h",
				},
			},
		},
	}

	scanner := NewIncludeScanner(dir, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "src/source.cpp",
		OwnAddIncl: []string{"libcxxabi/include"},
	})

	hasLibcxxabi := false
	hasLibcxxrt := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxabi/include/cxxabi.h"):
			hasLibcxxabi = true
		case strings.HasSuffix(p.String(), "/libcxxrt/include/cxxabi.h"):
			hasLibcxxrt = true
		}
	}

	if !hasLibcxxabi {
		t.Errorf("closure missing libcxxabi/include/cxxabi.h (OwnAddIncl resolution broken): %v", closure)
	}

	if !hasLibcxxrt {
		t.Errorf("closure missing libcxxrt/include/cxxabi.h — PR-36 multi-target bypass failed "+
			"for OwnAddIncl-resolved quoted include: %v", closure)
	}
}

// TestScanner_QuotedSameDirStillGated pins that the PR-36 bypass does
// NOT fire when the quoted include was resolved via the SAME DIRECTORY
// as the includer. Same-dir resolution means the file is literally
// adjacent — sysincl alternates are inappropriate regardless of the
// record's target count. This guards the libcxxrt/dwarf_eh.h → unwind.h
// regression that PR-35w originally closed: `libcxxrt/unwind.h` exists
// in the same directory as `dwarf_eh.h`, so the PR-36 multi-target bypass
// must NOT fire (even though the stl-to-libcxx.yml unwind.h record is
// multi-target) and `libcxx/include/unwind.h` must NOT appear.
func TestScanner_QuotedSameDirStillGated(t *testing.T) {
	dir := t.TempDir()

	mkdirs := []string{
		"libcxxrt",
		"libcxx/include",
	}

	for _, p := range mkdirs {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	// libcxxrt/dwarf_eh.h does `#include "unwind.h"` (quoted).
	// libcxxrt/unwind.h EXISTS (same-dir) — same-dir resolution wins.
	dwarf := []byte(`#include "unwind.h"
`)
	src := []byte(`#include "dwarf_eh.h"
`)

	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/dwarf_eh.h"), dwarf, 0o644); err != nil {
		t.Fatalf("write dwarf_eh.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/source.cc"), src, 0o644); err != nil {
		t.Fatalf("write source.cc: %v", err)
	}

	// libcxxrt/unwind.h exists in the SAME DIR as dwarf_eh.h.
	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/unwind.h"), []byte("// libcxxrt unwind.h\n"), 0o644); err != nil {
		t.Fatalf("write libcxxrt/unwind.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libcxx/include/unwind.h"), []byte("// libcxx unwind.h\n"), 0o644); err != nil {
		t.Fatalf("write libcxx/include/unwind.h: %v", err)
	}

	// Multi-target sysincl: unwind.h maps to BOTH libcxx and libcxxrt.
	// HasMultiTarget must be set explicitly on hand-built records (YAML
	// loading sets it automatically via parseSysInclYAML's flushRecord).
	sysincl := SysInclSet{
		{
			Filter:         nil,
			KeyBySource:    false,
			HasMultiTarget: true,
			Mappings: map[string][]string{
				"unwind.h": {
					"libcxx/include/unwind.h",
					"libcxxrt/unwind.h",
				},
			},
		},
	}

	scanner := NewIncludeScanner(dir, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "libcxxrt/source.cc",
		OwnAddIncl: []string{"libcxxrt"},
	})

	hasLibcxxrt := false
	hasLibcxxSpurious := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxrt/unwind.h"):
			hasLibcxxrt = true
		case strings.HasSuffix(p.String(), "/libcxx/include/unwind.h"):
			hasLibcxxSpurious = true
		}
	}

	if !hasLibcxxrt {
		t.Errorf("closure missing local libcxxrt/unwind.h (same-dir resolution broken): %v", closure)
	}

	if hasLibcxxSpurious {
		t.Errorf("closure contains spurious libcxx/include/unwind.h — PR-36 same-dir gate failed "+
			"(must NOT bypass for same-dir resolved quoted includes): %v", closure)
	}
}

// TestScanner_QuotedSysinclFiresOnLocalMiss is the converse pin: a
// quoted include whose local search-path resolution FAILED must still
// fall through to sysincl. The gate is "skip sysincl when local
// resolved", not "skip sysincl entirely for quoted form" — the
// upstream ymake scanner consults sysincl alternates when local lookup
// fails, and we must preserve that behaviour or quoted-form headers
// that only exist as sysincl entries (e.g. some musl-only forms)
// would silently lose their inputs.
func TestScanner_QuotedSysinclFiresOnLocalMiss(t *testing.T) {
	dir := t.TempDir()

	mkdirs := []string{"src", "musl/include"}

	for _, p := range mkdirs {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	// `#include "absent.h"` from src/source.cpp — no local file at
	// src/absent.h. The sysincl record provides musl/include/absent.h
	// as the alternate; the gate must let it through.
	src := []byte(`#include "absent.h"
`)

	if err := os.WriteFile(filepath.Join(dir, "src/source.cpp"), src, 0o644); err != nil {
		t.Fatalf("write source.cpp: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "musl/include/absent.h"), []byte("// musl absent.h\n"), 0o644); err != nil {
		t.Fatalf("write musl/include/absent.h: %v", err)
	}

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"absent.h": {"musl/include/absent.h"},
			},
		},
	}

	scanner := NewIncludeScanner(dir, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "src/source.cpp",
	})

	hasMusl := false

	for _, p := range closure {
		if strings.HasSuffix(p.String(), "/musl/include/absent.h") {
			hasMusl = true

			break
		}
	}

	if !hasMusl {
		t.Errorf("closure missing musl/include/absent.h — gate over-suppresses sysincl when local resolution failed: %v", closure)
	}
}

// TestScanner_AngleSysinclUnaffected pins the asymmetry: an
// angle-bracket include `#include <unwind.h>` whose local search-path
// resolution succeeded must STILL pick up matching sysincl alternates.
// libcxx/libcxxrt/libunwind ship multi-target sysincl records for the
// same logical header — the reference scan unions the local and
// sysincl resolutions, and the PR-35w gate is gated on QUOTED form
// only. Using the same physical layout as the quoted-resolved test
// but flipping `< >` MUST yield BOTH paths.
func TestScanner_AngleSysinclUnaffected(t *testing.T) {
	dir := t.TempDir()

	mkdirs := []string{"libcxxrt", "libunwind/include"}

	for _, p := range mkdirs {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	// `#include <unwind.h>` from libcxxrt/source.cpp — angle bracket.
	// Local resolution succeeds via OwnAddIncl=libcxxrt; sysincl maps
	// `unwind.h` → libunwind/include/unwind.h. Both must appear.
	src := []byte(`#include <unwind.h>
`)

	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/source.cpp"), src, 0o644); err != nil {
		t.Fatalf("write source.cpp: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libcxxrt/unwind.h"), []byte("// libcxxrt unwind.h\n"), 0o644); err != nil {
		t.Fatalf("write libcxxrt/unwind.h: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "libunwind/include/unwind.h"), []byte("// libunwind unwind.h\n"), 0o644); err != nil {
		t.Fatalf("write libunwind/include/unwind.h: %v", err)
	}

	sysincl := SysInclSet{
		{
			Filter:      nil,
			KeyBySource: false,
			Mappings: map[string][]string{
				"unwind.h": {"libunwind/include/unwind.h"},
			},
		},
	}

	scanner := NewIncludeScanner(dir, sysincl)
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "libcxxrt/source.cpp",
		OwnAddIncl: []string{"libcxxrt"},
	})

	hasLocal := false
	hasLibunwind := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxrt/unwind.h"):
			hasLocal = true
		case strings.HasSuffix(p.String(), "/libunwind/include/unwind.h"):
			hasLibunwind = true
		}
	}

	if !hasLocal {
		t.Errorf("closure missing local libcxxrt/unwind.h: %v", closure)
	}

	if !hasLibunwind {
		t.Errorf("closure missing libunwind/include/unwind.h — PR-35w gate over-suppressed sysincl on angle-bracket include: %v", closure)
	}
}

// TestScanner_LibcxxrtUnwindQuoted_ProductionParity is the
// production-tree pin for the canonical R5 case the PR-35w gate
// closes. The sequence:
//   - `libcxxrt/exception.cc` does `#include "unwind.h"` (quoted).
//   - Local resolution succeeds: same-dir lookup yields
//     `libcxxrt/unwind.h` — the legitimate intended target.
//   - `libcxxrt/unwind.h` itself does
//     `#include <contrib/libs/libunwind/include/unwind.h>` (fully-
//     qualified angle bracket), so the libunwind shadow comes in via
//     the transitive scan rather than via sysincl.
//   - Pre-PR-35w, sysincl additionally appended `libcxx/include/unwind.h`
//     on top of the locally-resolved libcxxrt copy — a spurious
//     over-emission the reference scan does not produce.
//
// Reference parity check: the closure of libcxxrt/exception.cc must
// contain libcxxrt/unwind.h and libunwind/include/unwind.h, but
// NOT libcxx/include/unwind.h.
func TestScanner_LibcxxrtUnwindQuoted_ProductionParity(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	if _, err := os.Stat(filepath.Join(sourceRoot, "contrib/libs/cxxsupp/libcxxrt/exception.cc")); err != nil {
		t.Skipf("libcxxrt source not present: %v", err)
	}

	sysincl := LoadSysInclSet(sourceRoot, func(Warn) {})
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	// ScanContext mirrors the libcxxrt CC consumer's emission shape:
	// libcxxrt is its own ADDINCL root; cxx-tail picks up libunwind
	// indirectly via libcxxrt/unwind.h's fully-qualified include.
	ctx := ScanContext{
		SourceRel: "contrib/libs/cxxsupp/libcxxrt/exception.cc",
		OwnAddIncl: []string{
			"contrib/libs/cxxsupp/libcxxrt",
		},
		BaseSearchPaths: []string{
			"contrib/libs/musl/include",
			"contrib/libs/musl/arch/aarch64",
			"contrib/libs/musl/arch/generic",
			"contrib/libs/linux-headers",
			"",
		},
	}

	closure := scanner.WalkClosure(ctx)

	if len(closure) == 0 {
		t.Fatalf("libcxxrt closure unexpectedly empty")
	}

	hasLibcxxrt := false
	hasLibunwind := false
	hasLibcxxSpurious := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/libcxxrt/unwind.h"):
			hasLibcxxrt = true
		case strings.HasSuffix(p.String(), "/libunwind/include/unwind.h"):
			hasLibunwind = true
		case strings.HasSuffix(p.String(), "/libcxx/include/unwind.h"):
			hasLibcxxSpurious = true
		}
	}

	if !hasLibcxxrt {
		t.Errorf("libcxxrt closure missing local libcxxrt/unwind.h (regression beyond PR-35w scope)")
	}

	if !hasLibunwind {
		t.Errorf("libcxxrt closure missing libunwind/include/unwind.h (PR-35w over-suppressed transitive chain)")
	}

	if hasLibcxxSpurious {
		t.Errorf("libcxxrt closure contains spurious libcxx/include/unwind.h — PR-35w gate failed to close R5 over-emission")
	}
}

// TestParseYasmIncludes_LowercaseQuoted pins the basic NASM/yasm
// `%include "foo"` form against the stand-alone parser. PR-35x's
// asmlib motivator: `cachesize64.asm:1` is `%include "defs.asm"`.
func TestParseYasmIncludes_LowercaseQuoted(t *testing.T) {
	in := []byte(`%include "defs.asm"
some_label:
    mov rax, 0
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target != "defs.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target, "defs.asm")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}

	if dirs[0].next {
		t.Errorf("next = true, want false (yasm has no %%include_next)")
	}
}

// TestParseYasmIncludes_UppercaseDirective pins case-insensitive
// directive matching: NASM/yasm directives are case-insensitive, and
// asmlib's `mersenne64.asm:64` / `mother64.asm:32` / `sfmt64.asm:29`
// all use uppercase `%INCLUDE "randomah.asi"`. Without case-insensitive
// matching those three sources would still miss `randomah.asi`.
func TestParseYasmIncludes_UppercaseDirective(t *testing.T) {
	in := []byte(`%INCLUDE "randomah.asi"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target != "randomah.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target, "randomah.asi")
	}

	if dirs[0].kind != includeQuoted {
		t.Errorf("kind = %v, want includeQuoted", dirs[0].kind)
	}
}

// TestParseYasmIncludes_LineCommentIgnored asserts that yasm `;`
// line comments do not produce phantom includes. A `;` at column zero
// followed by `%include "ghost.asm"` text must not match — the
// `^\s*%include` anchor requires `%` as the first non-whitespace
// token; `;` blocks the regex from firing.
func TestParseYasmIncludes_LineCommentIgnored(t *testing.T) {
	in := []byte(`; %include "ghost.asm"
%include "real.asm"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target != "real.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target, "real.asm")
	}
}

// TestParseYasmIncludes_TrailingSemicolonComment pins that a real
// directive followed by an inline `; ...` comment still parses. The
// regex anchors on the directive head and stops at the closing `"`,
// so trailing trivia is naturally ignored. Mirrors
// `instrset64.asm:26`'s `%include "instrset64.asm"              ;
// include code for InstructionSet function`.
func TestParseYasmIncludes_TrailingSemicolonComment(t *testing.T) {
	in := []byte(`%include "instrset64.asm"              ; include code for InstructionSet function
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target != "instrset64.asm" {
		t.Errorf("target = %q, want %q", dirs[0].target, "instrset64.asm")
	}
}

// TestParseYasmIncludes_NoMatchOnCInclude is the cross-syntax pin: a
// C-style `#include` on a yasm line must NOT match the yasm parser.
// `parseYasmIncludes` is dispatched only for `.asm`/`.asi`, so a
// stray `#` that does not begin with `%` should produce no
// directive.
func TestParseYasmIncludes_NoMatchOnCInclude(t *testing.T) {
	in := []byte(`#include "foo.h"
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 0 {
		t.Errorf("got %d directives, want 0; %+v", len(dirs), dirs)
	}
}

// TestParseYasmIncludes_AngleBracketForm verifies the angle-bracket
// branch. Not observed in asmlib but supported for parity with the
// C scanner — yasm accepts `%include <foo>` for system-style search.
func TestParseYasmIncludes_AngleBracketForm(t *testing.T) {
	in := []byte(`%include <sysmacros.asi>
`)

	dirs := parseYasmIncludes(in)

	if len(dirs) != 1 {
		t.Fatalf("got %d directives, want 1; %+v", len(dirs), dirs)
	}

	if dirs[0].target != "sysmacros.asi" {
		t.Errorf("target = %q, want %q", dirs[0].target, "sysmacros.asi")
	}

	if dirs[0].kind != includeSystem {
		t.Errorf("kind = %v, want includeSystem", dirs[0].kind)
	}
}

// TestParseIncludes_DispatchByExtension pins parseIncludes' dispatch
// by file extension: a `.asm` file routes to the yasm parser; a `.h`
// file routes to the C parser. The two parsers agree on the
// `includeDirective` shape but only one fires per file. Without the
// dispatch the asmlib AS scanner missed every `%include` (PR-35t R4
// root cause).
func TestParseIncludes_DispatchByExtension(t *testing.T) {
	dir := t.TempDir()

	asmPath := filepath.Join(dir, "src.asm")
	hPath := filepath.Join(dir, "src.h")

	if err := os.WriteFile(asmPath, []byte(`%include "defs.asm"
#include "should-not-match.h"
`), 0o644); err != nil {
		t.Fatalf("write src.asm: %v", err)
	}

	if err := os.WriteFile(hPath, []byte(`#include "real.h"
%include "should-not-match.asm"
`), 0o644); err != nil {
		t.Fatalf("write src.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})

	// PR-M3-vfs-paths: parseIncludes takes a VFS path.
	asmDirs := scanner.parseIncludes(Source("src.asm"))
	hDirs := scanner.parseIncludes(Source("src.h"))

	if len(asmDirs) != 1 || asmDirs[0].target != "defs.asm" {
		t.Errorf("asm dispatch failed: got %+v, want one directive targeting defs.asm", asmDirs)
	}

	if len(hDirs) != 1 || hDirs[0].target != "real.h" {
		t.Errorf("h dispatch failed: got %+v, want one directive targeting real.h", hDirs)
	}
}

// TestParseIncludes_AsiDispatchesToYasm pins that `.asi` (yasm
// include-only file) extension also routes to the yasm parser.
// asmlib's `randomah.asi` is a `.asi` file; without `.asi` in the
// dispatch list, transitive scans through it would silently miss any
// nested `%include` it might hold.
func TestParseIncludes_AsiDispatchesToYasm(t *testing.T) {
	dir := t.TempDir()
	asiPath := filepath.Join(dir, "src.asi")

	if err := os.WriteFile(asiPath, []byte(`%include "nested.asi"
`), 0o644); err != nil {
		t.Fatalf("write src.asi: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	// PR-M3-vfs-paths: parseIncludes takes a VFS path.
	dirs := scanner.parseIncludes(Source("src.asi"))

	if len(dirs) != 1 || dirs[0].target != "nested.asi" {
		t.Errorf(".asi dispatch failed: got %+v, want one directive targeting nested.asi", dirs)
	}
}

// TestScanner_AsmlibAsmInputsParity is the production-tree pin for
// the PR-35x R4 closure. The ScanContext mirrors what
// `gen.go::walkClosure` constructs for an asmlib host AS
// node (PIC-mode, asmlibYasmModules trigger). The transitive closure
// of `contrib/libs/asmlib/sfmt64.asm` must contain BOTH
// `defs.asm` (via the file's leading `%include "defs.asm"`) and
// `randomah.asi` (via the `%INCLUDE "randomah.asi"` later in the
// file). Reference: `sg.json` 1013831-1013833.
//
// Skips when the production tree is not present.
func TestScanner_AsmlibAsmInputsParity(t *testing.T) {
	const sourceRoot = "/home/pg/monorepo/yatool_orig"

	if _, err := os.Stat(filepath.Join(sourceRoot, "contrib/libs/asmlib/sfmt64.asm")); err != nil {
		t.Skipf("asmlib source not present: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sourceRoot, "build", "sysincl")); err != nil {
		t.Skipf("sysincl tree %s not present: %v", sourceRoot, err)
	}

	sysincl := LoadSysInclSet(sourceRoot, func(Warn) {})
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	ctx := ScanContext{
		SourceRel: "contrib/libs/asmlib/sfmt64.asm",
	}

	closure := scanner.WalkClosure(ctx)

	if len(closure) == 0 {
		t.Fatalf("asmlib sfmt64.asm closure unexpectedly empty")
	}

	hasDefs := false
	hasRandomah := false

	for _, p := range closure {
		switch {
		case strings.HasSuffix(p.String(), "/asmlib/defs.asm"):
			hasDefs = true
		case strings.HasSuffix(p.String(), "/asmlib/randomah.asi"):
			hasRandomah = true
		}
	}

	if !hasDefs {
		t.Errorf("asmlib sfmt64.asm closure missing defs.asm — PR-35x yasm-include scanner regression: %v", closure)
	}

	if !hasRandomah {
		t.Errorf("asmlib sfmt64.asm closure missing randomah.asi — PR-35x case-insensitive yasm-include matching regression: %v", closure)
	}
}

// TestParseIncludes_MacroIndirectAugmentation pins the
// PR-M3-musl-self-closure behaviour: sources known to use macro-indirect
// `#include MACRO_NAME` forms get synthetic includeDirectives appended
// after the regex-extracted set. The text-blind regex parser cannot
// expand the macro; the table-driven augmenter is the surgical fix.
func TestParseIncludes_MacroIndirectAugmentation(t *testing.T) {
	dir := t.TempDir()
	rel := "contrib/libs/openssl/crypto/uid.c"
	full := filepath.Join(dir, rel)

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := []byte(`#include <openssl/crypto.h>
# include OPENSSL_UNISTD
`)

	if err := os.WriteFile(full, src, 0o644); err != nil {
		t.Fatalf("write uid.c: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	dirs := scanner.parseIncludes(Source(rel))

	var hasCrypto, hasUnistd bool

	for _, d := range dirs {
		if d.target == "openssl/crypto.h" && d.kind == includeSystem {
			hasCrypto = true
		}

		if d.target == "unistd.h" && d.kind == includeSystem {
			hasUnistd = true
		}
	}

	if !hasCrypto {
		t.Errorf("regex-parsed openssl/crypto.h missing: %+v", dirs)
	}

	if !hasUnistd {
		t.Errorf("macro-indirect unistd.h augmentation missing for openssl/crypto/uid.c: %+v", dirs)
	}
}
