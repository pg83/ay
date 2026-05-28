package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

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
type macroAuditState struct {
	enabled  bool
	mu       sync.Mutex
	ignored  map[string]int            // macro name → invocation count
	services map[string]map[string]int // macro name → service arg → count
}

var macroAudit = &macroAuditState{
	ignored:  map[string]int{},
	services: map[string]map[string]int{},
}

func enableMacroAudit() {
	macroAudit.mu.Lock()
	defer macroAudit.mu.Unlock()
	macroAudit.enabled = true
}

// recordIgnoredMacro is called from applyUnknownStmt's default branch when
// the macro name is in whitelistedMetadataMacros — i.e., gen sees it but
// produces nothing from it.
func recordIgnoredMacro(name string, args []string) {
	if !macroAudit.enabled {
		return
	}
	macroAudit.mu.Lock()
	defer macroAudit.mu.Unlock()
	macroAudit.ignored[name]++
	recordServiceArgsLocked(name, args)
}

// recordHandledMacro is called from every handled branch in applyUnknownStmt
// (and the typed-stmt cases in collectStmts) so service keywords that gen
// already routes still show up in the audit — confirming that the keyword
// is part of the surface area we model.
func recordHandledMacro(name string, args []string) {
	if !macroAudit.enabled {
		return
	}
	macroAudit.mu.Lock()
	defer macroAudit.mu.Unlock()
	recordServiceArgsLocked(name, args)
}

func recordServiceArgsLocked(macroName string, args []string) {
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
		return
	}
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
