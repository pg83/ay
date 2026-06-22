package main

import "strings"

// A node's command line is a []STR: every token converts to its backing STR via
// x.str(), so the slice needs no tagged union.

// appendArgStr appends already-interned ARG flag bundles as STR — no re-interning.
func appendArgStr(dst []STR, srcs ...[]ARG) []STR {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.str())
		}
	}

	return dst
}

// appendArgGroupStr is appendArgStr for group-ARGs whose value is a space-joined
// token list; it splits each group back into individual command tokens.
func appendArgGroupStr(dst []STR, srcs ...[]ARG) []STR {
	for _, s := range srcs {
		for _, a := range s {
			for _, tok := range strings.Fields(a.string()) {
				dst = append(dst, internStr(tok))
			}
		}
	}

	return dst
}

// appendInternStrs interns a []string and appends each as a STR.
func appendInternStrs(dst []STR, ss []string) []STR {
	for _, s := range ss {
		dst = append(dst, internStr(s))
	}

	return dst
}

// appendStrStrs materializes a []STR onto a []string — the sink-side boundary.
func appendStrStrs(dst []string, as []STR) []string {
	for _, a := range as {
		dst = append(dst, a.string())
	}

	return dst
}

func strStrs(as []STR) []string {
	return appendStrStrs(make([]string, 0, len(as)), as)
}

// internArgsFromSTR re-interns parsed STR tokens into the ARG namespace, since
// flag tables are ARG-typed.
func internArgsFromSTR(items []STR) []ARG {
	out := make([]ARG, 0, len(items))

	for _, s := range items {
		out = append(out, internArg(s.string()))
	}

	return out
}

// strStrings converts an STR slice to its string views — no per-element allocation.
func strStrings(items []STR) []string {
	out := make([]string, 0, len(items))

	for _, s := range items {
		out = append(out, s.string())
	}

	return out
}

// STRS interns a literal token list — the test-side counterpart of the parser.
func STRS(items ...string) []STR {
	out := make([]STR, 0, len(items))

	for _, s := range items {
		out = append(out, internStr(s))
	}

	return out
}

// strPtr returns a pointer to an interned id — the *STR optional-field helper.
func strPtr(s STR) *STR {
	return &s
}

// strsContain reports membership by intern id, probing without polluting the table.
func strsContain(items []STR, s string) bool {
	id := interned(s)

	if id == 0 {
		return false
	}

	for _, it := range items {
		if it == id {
			return true
		}
	}

	return false
}
