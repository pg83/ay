package main

import (
	"strings"
	"unsafe"
)

func strBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func appendArgStr(dst []STR, srcs ...[]ARG) []STR {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.str())
		}
	}

	return dst
}

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

func internArgsFromSTR(items []STR) []ARG {
	out := make([]ARG, 0, len(items))

	for _, s := range items {
		out = append(out, internArg(s.string()))
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
