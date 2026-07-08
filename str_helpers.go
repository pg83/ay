package main

import (
	"strings"
	"unsafe"
)

func strBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

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

func appendInternStrs(dst []STR, ss []string) []STR {
	for _, s := range ss {
		dst = append(dst, internStr(s))
	}

	return dst
}

func appendStrStrs(dst []string, as []STR) []string {
	for _, a := range as {
		dst = append(dst, a.string())
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

func strStrs(as []STR) []string {
	return appendStrStrs(make([]string, 0, len(as)), as)
}

func internAnysFromSTR(items []STR) []ANY {
	out := make([]ANY, 0, len(items))

	for _, s := range items {
		out = append(out, s.any())
	}

	return out
}

func strStrings(items []STR) []string {
	out := make([]string, 0, len(items))

	for _, s := range items {
		out = append(out, s.string())
	}

	return out
}

func sTRS(items ...string) []STR {
	out := make([]STR, 0, len(items))

	for _, s := range items {
		out = append(out, internStr(s))
	}

	return out
}

func strPtr(s STR) *STR {
	return &s
}

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

func appendAnyLists(dst []ANY, srcs ...[]ANY) []ANY {
	for _, s := range srcs {
		dst = append(dst, s...)
	}

	return dst
}

func appendAnys(dst []ANY, ss []STR) []ANY {
	for _, s := range ss {
		dst = append(dst, s.any())
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
