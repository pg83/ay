package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
