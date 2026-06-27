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

var macrosAcceptingUserFlags = map[TOK]struct{}{
	tokEnable:  {},
	tokDisable: {},

	tokExcludeTags: {},

	tokSetResourceUriFromJson: {},

	tokLicense: {},

	tokSetAppend: {},
}

var serviceArgOK BitSet

type MacroAuditState struct {
	enabled  bool
	mu       sync.Mutex
	ignored  map[string]int
	services map[string]map[string]int
	unknown  map[string]map[string]int
}

func enableMacroAudit() {
	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	macroAudit.enabled = true
}

func recordIgnoredMacro(name TOK) {
	if !macroAudit.enabled {
		return
	}

	macroAudit.mu.Lock()

	defer macroAudit.mu.Unlock()

	macroAudit.ignored[name.string()]++
}

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
