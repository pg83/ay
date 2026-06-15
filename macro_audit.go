package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

var macroAudit = &MacroAuditState{
	ignored:  map[string]int{},
	services: map[string]map[string]int{},
	unknown:  map[string]map[string]int{},
}

// macrosAcceptingUserFlags lists handled macros whose arguments are not
// structural keywords but arbitrary user-defined flag names — ENABLE(MY_X)
// and DISABLE(MY_X) translate into env.SetBool(MY_X, …) and accept anything
// the ya.make author chose. The strict service-keyword check is suppressed
// for these so a new project-specific flag does not need to be hard-coded.
var macrosAcceptingUserFlags = map[TOK]struct{}{
	tokEnable:  {},
	tokDisable: {},
	// EXCLUDE_TAGS args are submodule tag NAMES (GO_PROTO, JAVA_PROTO, PY_PROTO,
	// …) — user data, not structural service-keywords — so they bypass the
	// strict service-keyword audit.
	tokExcludeTags: {},
	// SET_RESOURCE_URI_FROM_JSON(VarName file.json): the first arg is the
	// user-chosen destination variable name (SANDBOX_RESOURCE_URI, WITH_JDK_URI,
	// …), not a structural keyword — like ENABLE's flag name.
	tokSetResourceUriFromJson: {},
}

// serviceArgOK marks arg STRs the service-keyword check has already passed
// (either not service-shaped, or a modelled keyword) — args repeat heavily
// across ya.makes, so the per-invocation check is a bit probe. Single-writer
// (gen goroutine), like the deduper.
var serviceArgOK BitSet

// macroAudit collects two classes of ya.make traffic during gen:
//   - macro invocations whose name lands in whitelistedMetadataMacros (i.e.,
//     we accept the macro but emit nothing for it);
//   - macro argument tokens that are equal to strings.ToUpper(token) AND
//     consist of [A-Z0-9_] only — those are the service keywords that
//     upstream's ya.make grammar uses to split macro argument lists
//     (e.g. NAMESPACE, MAIN, GLOBAL, ADDINCL, ONE_LEVEL, FOR, TOP_LEVEL,
//     OUTPUT_INCLUDES, IN, OUT_NOAUTO, etc.). Audit them so we can confirm
//     we handle every meaningful split key for every macro we accept.
//
// The audit is process-global because gen runs serially per-target and
// collecting across the whole graph keeps the dump compact. Output is
// surfaced when cmdMake's --dump-ignored-macros flag is set; otherwise
// recording is a cheap nil-check.
type MacroAuditState struct {
	enabled  bool
	mu       sync.Mutex
	ignored  map[string]int            // macro name → invocation count
	services map[string]map[string]int // macro name → service arg → count
	unknown  map[string]map[string]int // macro name → unhandled service arg → count
}

func enableMacroAudit() {
	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	macroAudit.enabled = true
}

// recordIgnoredMacro is called from applyUnknownStmt's default branch when
// the macro name is in acknowledgedMacros — i.e., gen sees it but produces
// nothing from it. Service-word args of these macros are intentionally NOT
// inspected: their argument grammar belongs to upstream's macro parser, not
// ours, so listing tokens like `MIT` for LICENSE only generates noise.
func recordIgnoredMacro(name TOK) {
	if !macroAudit.enabled {
		return
	}

	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	macroAudit.ignored[name.string()]++
}

// recordHandledMacro is called for every macro whose typed branch in
// applyUnknownStmt fired. It enforces "agent must immediately support all
// macro flags": every uppercase service-keyword argument must already be
// present as a "…" literal in this package's .go sources (mined via
// macro_audit_known.go); an unknown keyword throws so the next agent run
// is forced to model it before any graph is emitted. Macros listed in
// macrosAcceptingUserFlags bypass the check. The audit buckets fill only
// when --dump-ignored-macros is on.
func recordHandledMacro(name TOK, args []STR) {
	if _, free := macrosAcceptingUserFlags[name]; !free {
		for _, aTok := range args {
			if serviceArgOK.has(uint32(aTok)) {
				continue
			}

			a := aTok.string()

			if looksLikeServiceWord(a) {
				if _, ok := knownServiceTokens()[a]; !ok {
					throwFmt("gen: macro %s received service-keyword %q that no handler models — open the upstream macro definition (yatool/build/conf, yatool/build/ymake.core.conf, yatool/build/plugins) and implement its semantics; only then drop the keyword as a \"…\" literal in the macro's handler", name.string(), a)
				}
			}

			serviceArgOK.add(uint32(aTok))
		}
	}

	if !macroAudit.enabled {
		return
	}

	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	recordServiceArgsLocked(name.string(), strStrings(args))
}

func recordServiceArgsLocked(macroName string, args []string) {
	known := knownServiceTokens()

	for _, a := range args {
		if !looksLikeServiceWord(a) {
			continue
		}

		bucket, ok := macroAudit.services[macroName]

		if !ok {
			bucket = map[string]int{}
			macroAudit.services[macroName] = bucket
		}

		bucket[a]++

		if _, modelled := known[a]; modelled {
			continue
		}

		unkBucket, ok := macroAudit.unknown[macroName]

		if !ok {
			unkBucket = map[string]int{}
			macroAudit.unknown[macroName] = unkBucket
		}

		unkBucket[a]++
	}
}

// looksLikeServiceWord reports whether a token is an uppercase keyword: at
// least one ASCII letter, only [A-Z0-9_], and equal to its own upper-case
// form (the last check is redundant with the second but kept explicit so the
// definition matches the user-facing description).
func looksLikeServiceWord(s string) bool {
	if s == "" {
		return false
	}

	hasLetter := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			return false
		}
	}

	if !hasLetter {
		return false
	}

	return s == strings.ToUpper(s)
}

// dumpMacroAudit writes the collected report to w. Called from cmdMake when
// --dump-ignored-macros was passed.
func dumpMacroAudit(w io.Writer) {
	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	if !macroAudit.enabled {
		return
	}

	fmt.Fprintln(w, "=== ya.make macros gen acknowledges but emits nothing for ===")

	if len(macroAudit.ignored) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		names := make([]string, 0, len(macroAudit.ignored))

		for n := range macroAudit.ignored {
			names = append(names, n)
		}

		sort.Strings(names)

		for _, n := range names {
			fmt.Fprintf(w, "  %-40s × %d\n", n, macroAudit.ignored[n])
		}
	}

	fmt.Fprintln(w, "=== uppercase service-keyword arguments seen per macro ===")

	if len(macroAudit.services) == 0 {
		fmt.Fprintln(w, "  (none)")
	} else {
		macros := make([]string, 0, len(macroAudit.services))

		for n := range macroAudit.services {
			macros = append(macros, n)
		}

		sort.Strings(macros)

		for _, m := range macros {
			bucket := macroAudit.services[m]
			args := make([]string, 0, len(bucket))

			for a := range bucket {
				args = append(args, a)
			}

			sort.Strings(args)
			fmt.Fprintf(w, "  %s:\n", m)

			for _, a := range args {
				fmt.Fprintf(w, "      %-30s × %d\n", a, bucket[a])
			}
		}
	}

	fmt.Fprintln(w, "=== unhandled service-keyword arguments (not present as a \"…\" literal in *.go) ===")

	if len(macroAudit.unknown) == 0 {
		fmt.Fprintln(w, "  (none)")

		return
	}

	macros := make([]string, 0, len(macroAudit.unknown))

	for n := range macroAudit.unknown {
		macros = append(macros, n)
	}

	sort.Strings(macros)

	for _, m := range macros {
		bucket := macroAudit.unknown[m]
		args := make([]string, 0, len(bucket))

		for a := range bucket {
			args = append(args, a)
		}

		sort.Strings(args)
		fmt.Fprintf(w, "  %s:\n", m)

		for _, a := range args {
			fmt.Fprintf(w, "      %-30s × %d\n", a, bucket[a])
		}
	}
}
