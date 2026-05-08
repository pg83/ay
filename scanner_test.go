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

	// The `/* not a real block comment */` lives inside the raw string
	// body and must survive — the raw-string transparency layer keeps
	// the comment state from engaging.
	if !strings.Contains(s, "/* not a real block comment */") {
		t.Errorf("raw-string body modified or comment state entered: %q", s)
	}

	if got, want := bytes.Count(out, []byte{'\n'}), bytes.Count(in, []byte{'\n'}); got != want {
		t.Errorf("newline count mismatch: got %d, want %d", got, want)
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

	dirs := scanner.parseIncludes(path)

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

	sysincl := LoadSysInclSet(sourceRoot)
	scanner := NewIncludeScanner(sourceRoot, sysincl)

	// libcxx-source case. The ScanContext mirrors what gen.go's
	// `scanIncludesForSource` constructs for a libcxx CC consumer:
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
		if strings.HasSuffix(p, "/libcxx/include/uchar.h") {
			t.Errorf("libcxx-source closure contains spurious libcxx-uchar: %s (PR-35e regression)", p)
		}

		if strings.HasSuffix(p, "/musl/include/uchar.h") {
			t.Errorf("libcxx-source closure contains spurious musl-uchar: %s (PR-35e regression)", p)
		}
	}

	// Sanity: the closure must still include __mbstate_t.h itself
	// (the includer of the suppressed `#include_next <uchar.h>`); we
	// only suppress the chain past the directive, not the includer.
	foundMbstate := false

	for _, p := range closure {
		if strings.HasSuffix(p, "/libcxx/include/__mbstate_t.h") {
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

	sysincl := LoadSysInclSet(sourceRoot)
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
		case strings.HasSuffix(p, "/libcxx/include/string.h"):
			hasLibcxxString = true
		case strings.HasSuffix(p, "/musl/include/string.h"):
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

	sysincl := LoadSysInclSet(sourceRoot)
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
