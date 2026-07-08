package main

import "strings"

func appendArgGroupStr(dst []ANY, srcs ...[]ANY) []ANY {
	for _, s := range srcs {
		for _, a := range s {
			for _, tok := range strings.Fields(a.string()) {
				dst = append(dst, internStr(tok).any())
			}
		}
	}

	return dst
}

func anyStrs(as []ANY) []string {
	out := make([]string, 0, len(as))

	for _, a := range as {
		out = append(out, a.string())
	}

	return out
}

func strStrings(items []ANY) []string {
	out := make([]string, 0, len(items))

	for _, s := range items {
		out = append(out, s.string())
	}

	return out
}

func anyPtr(s ANY) *ANY {
	return &s
}

func strsContain(items []ANY, s string) bool {
	id := interned(s)

	if id == 0 {
		return false
	}

	for _, it := range items {
		if it == id.any() {
			return true
		}
	}

	return false
}

func appendAnyLists(dst []ANY, srcs ...[]ANY) []ANY {
	for _, s := range srcs {
		dst = append(dst, s...)
	}

	return dst
}

func appendAnys(dst []ANY, ss []ANY) []ANY {
	for _, s := range ss {
		dst = append(dst, s)
	}

	return dst
}

func appendInternAnys(dst []ANY, ss []string) []ANY {
	for _, s := range ss {
		dst = append(dst, internStr(s).any())
	}

	return dst
}

func strsAny(ss []STR) []ANY {
	if len(ss) == 0 {
		return nil
	}

	out := make([]ANY, len(ss))

	for i, s := range ss {
		out[i] = s.any()
	}

	return out
}

func anysOf(items ...string) []ANY {
	return internAnys(items)
}

func internAnys(ss []string) []ANY {
	if len(ss) == 0 {
		return nil
	}

	out := make([]ANY, len(ss))

	for i, s := range ss {
		out[i] = internAny(s)
	}

	return out
}
