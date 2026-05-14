package main

import (
	"fmt"
	"os"
)

// Warn is the diagnostic payload threaded through every Gen-time
// `onWarn` callback. Kind discriminates the category (so receivers
// can route, count, or dedup per type); Message is the human-readable
// detail the CLI surfaces under `--verbose`.
type Warn struct {
	Kind    WarnKind
	Message string
}

// WarnKind enumerates the diagnostic categories Gen may emit. Add a
// new value when introducing a new diagnostic site — never overload
// an existing kind to mean two things, since downstream consumers
// dispatch on this value.
type WarnKind int

const (
	// WarnSysIncl is a sysincl loader diagnostic: a `source_filter`
	// record the runtime cannot model (unsupported header, negative
	// lookahead at the wrong position, regex-compile failure). The
	// record is dropped; the scan continues without its mappings.
	WarnSysIncl WarnKind = iota

	// WarnMissingInclude is an include-resolver diagnostic: an
	// `#include` directive that found no hit in the source dir,
	// build dir, search path, OR sysincl mappings. The build still
	// proceeds — the upstream reference graph tolerates these too —
	// but the directive contributes no input edge.
	WarnMissingInclude
)

// String returns a stable lower-case label for the kind, suitable
// as a stderr-line prefix (`sysincl:`, `missing-include:`). Used by
// the default `printWarn` formatter; receivers that route programmatically
// dispatch on the Kind value directly and ignore this.
func (k WarnKind) String() string {
	switch k {
	case WarnSysIncl:
		return "sysincl"
	case WarnMissingInclude:
		return "missing-include"
	}
	return "warn"
}

// printWarn is the default `onWarn` receiver the CLI installs under
// `--verbose`: one stderr line per Warn, prefixed with the kind's
// label.
func printWarn(w Warn) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", w.Kind, w.Message)
}
