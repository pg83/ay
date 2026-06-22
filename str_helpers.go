package main

import "strings"

// CmdArgs (a node's command line) is a []STR: every heterogeneous token — flag
// (ARG), path (VFS), macro name (TOK), env var (ENV) or literal — is converted
// to its backing STR via the free x.str(), so the slice needs no tagged union.
// The string form is materialized only at the sink via STR.String().

// appendArgStr converts already-interned ARG flag bundles to their STR and
// appends them — the cheap path for static flag groups (no re-interning).
func appendArgStr(dst []STR, srcs ...[]ARG) []STR {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.str())
		}
	}

	return dst
}

// appendArgGroupStr is appendArgStr for group-ARGs whose value is a space-joined
// token list (e.g. one EXTRALIBS(...) call). It splits each group back into
// individual command tokens, after the whole-group string served as the dedup key.
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

// appendInternStrs interns a genuine []string and appends each as a STR — the
// string→STR boundary for cold command tails whose tokens are not pre-interned.
func appendInternStrs(dst []STR, ss []string) []STR {
	for _, s := range ss {
		dst = append(dst, internStr(s))
	}

	return dst
}

// appendStrStrs materializes a cmd-arg []STR onto a []string — the sink-side
// boundary.
func appendStrStrs(dst []string, as []STR) []string {
	for _, a := range as {
		dst = append(dst, a.string())
	}

	return dst
}

func strStrs(as []STR) []string {
	return appendStrStrs(make([]string, 0, len(as)), as)
}

// internArgsFromSTR re-interns parsed STR tokens into the ARG namespace (flag
// tables are ARG-typed; the parser hands statements over as STR).
func internArgsFromSTR(items []STR) []ARG {
	out := make([]ARG, 0, len(items))

	for _, s := range items {
		out = append(out, internArg(s.string()))
	}

	return out
}

// strStrings converts an STR slice to its string views (each is a view into the
// intern table — no per-element allocation).
func strStrings(items []STR) []string {
	out := make([]string, 0, len(items))

	for _, s := range items {
		out = append(out, s.string())
	}

	return out
}

// STRS interns a literal token list — the test-side counterpart of the parser's
// interned argument output.
func STRS(items ...string) []STR {
	out := make([]STR, 0, len(items))

	for _, s := range items {
		out = append(out, internStr(s))
	}

	return out
}

// strPtr returns a pointer to an interned id — the *STR optional-field
// counterpart of stringPtr.
func strPtr(s STR) *STR {
	return &s
}

// strsContain reports membership of the string's intern id in an STR list — an
// unknown string cannot be a member (probe without polluting the table).
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
