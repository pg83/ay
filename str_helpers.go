package main

// CmdArgs (a node's command line) is a []STR: every heterogeneous token — a flag
// (ARG), a path (VFS), a macro name (TOK), an env var (ENV) or a literal — is
// converted to the STR backing it via the free x.str() conversion, so the slice
// needs no tagged union. The string form is materialized only at the sink
// (graph write / UID canonicalisation) via STR.String().

// appendArgStr converts already-interned ARG flag bundles to their STR and
// appends them — the cheap path for the static flag groups in compile/link
// command lines (no re-interning).
func appendArgStr(dst []STR, srcs ...[]ARG) []STR {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.str())
		}
	}

	return dst
}

// appendInternStrs interns a genuine []string (e.g. linker-selection flag groups or
// per-node computed args) and appends each as a STR — the string→STR boundary for
// cold command tails whose tokens are not pre-interned.
func appendInternStrs(dst []STR, ss []string) []STR {
	for _, s := range ss {
		dst = append(dst, internStr(s))
	}

	return dst
}

// appendStrStrs materializes a cmd-arg []STR onto a []string — the sink-side
// boundary (graph write, canonicalisation, executor).
func appendStrStrs(dst []string, as []STR) []string {
	for _, a := range as {
		dst = append(dst, a.string())
	}

	return dst
}

func strStrs(as []STR) []string {
	return appendStrStrs(make([]string, 0, len(as)), as)
}
