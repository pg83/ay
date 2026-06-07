package main

// ANY is a tagged interned reference: the low anyTagBits bits select the
// namespace (ENV/TOK/ARG/STR/VFS) and the high bits hold that namespace's id.
// It lets heterogeneous interned tokens — flags (ARG), paths (VFS), literal
// option strings (STR), … — sit uniformly in one slice (e.g. a node's CmdArgs)
// with O(1) boxing and a single String() dispatch, instead of materializing
// every element to a Go string at construction.
type ANY uint32

const (
	anyTagENV uint32 = iota
	anyTagTOK
	anyTagARG
	anyTagSTR
	anyTagVFS
)

const (
	anyTagBits = 3
	anyTagMask = 1<<anyTagBits - 1
)

func envAny(e ENV) ANY {
	return ANY(uint32(e)<<anyTagBits | anyTagENV)
}

func tokAny(t TOK) ANY {
	return ANY(uint32(t)<<anyTagBits | anyTagTOK)
}

func argAny(a ARG) ANY {
	return ANY(uint32(a)<<anyTagBits | anyTagARG)
}

func strAny(s STR) ANY {
	return ANY(uint32(s)<<anyTagBits | anyTagSTR)
}

func vfsAny(v VFS) ANY {
	return ANY(uint32(v)<<anyTagBits | anyTagVFS)
}

// stringAny interns a computed/raw string and boxes it as a STR-tagged ANY —
// the entry point for cmd args assembled from non-interned strings.
func stringAny(s string) ANY {
	return strAny(internString(s))
}

func (a ANY) String() string {
	id := uint32(a) >> anyTagBits

	switch uint32(a) & anyTagMask {
	case anyTagENV:
		return ENV(id).String()
	case anyTagTOK:
		return TOK(id).String()
	case anyTagARG:
		return ARG(id).String()
	case anyTagSTR:
		return STR(id).String()
	case anyTagVFS:
		return VFS(id).String()
	}

	ThrowFmt("ANY.String: bad tag %d", uint32(a)&anyTagMask)
	return ""
}

// appendArgAny boxes already-interned ARG flags into ANY with no re-interning —
// the cheap path for the static flag bundles in the compile/link command lines.
func appendArgAny(dst []ANY, srcs ...[]ARG) []ANY {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, argAny(a))
		}
	}

	return dst
}

// appendStringAny interns and boxes a genuine []string (e.g. platform link-tail
// fields) into ANY. For cold (per-link/per-node) command tails — the strings are
// not pre-interned, so this is the string→ANY boundary.
func appendStringAny(dst []ANY, ss []string) []ANY {
	for _, s := range ss {
		dst = append(dst, stringAny(s))
	}

	return dst
}

// appendAnyStrs materializes the boxed args onto a string slice — the sink-side
// boundary (graph write, canonicalisation, executor).
func appendAnyStrs(dst []string, as []ANY) []string {
	for _, a := range as {
		dst = append(dst, a.String())
	}

	return dst
}

func anyStrs(as []ANY) []string {
	return appendAnyStrs(make([]string, 0, len(as)), as)
}
