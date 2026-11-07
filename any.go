package main

import (
	"path/filepath"
	"slices"
)

type ANY uint32

func (a ANY) vfs() VFS {
	if uint32(a)&1 == 0 {
		return 0
	}

	return VFS(uint32(a) >> 1)
}

func (a ANY) str() STR {
	if uint32(a)&1 != 0 {
		return 0
	}

	return STR(uint32(a) >> 1)
}

func (a ANY) string() string {
	if v := a.vfs(); v != 0 {
		return v.string()
	}

	return a.str().string()
}

func textANYs(in []ANY) []ANY {
	var out []ANY

	for i, a := range in {
		if a.vfs() != 0 {
			if out == nil {
				out = slices.Clone(in)
			}

			out[i] = a.textSTR().any()
		}
	}

	if out == nil {
		return in
	}

	return out
}

func canonicalANY(a ANY) ANY {
	if a.vfs() == 0 && vfsHasPrefix(a.str().string()) {
		return intern(a.str().string()).any()
	}

	return a
}

func canonicalANYs(in []ANY) []ANY {
	var out []ANY

	for i, a := range in {
		if a.vfs() == 0 && vfsHasPrefix(a.str().string()) {
			if out == nil {
				out = slices.Clone(in)
			}

			out[i] = intern(a.str().string()).any()
		}
	}

	if out == nil {
		return in
	}

	return out
}

func (a ANY) dirSource() VFS {
	if s := a.str(); s != 0 {
		return dirSourceOf(s)
	}

	return source(filepath.Clean(a.string()))
}

func (a ANY) peerSource() VFS {
	if s := a.str(); s != 0 {
		return s.source()
	}

	return source(a.string())
}

func (a ANY) textSTR() STR {
	if v := a.vfs(); v != 0 {
		return internStr(v.string())
	}

	return a.str()
}

func (a ANY) relOrSelf() STR {
	if v := a.vfs(); v != 0 {
		return v.rel()
	}

	return a.str()
}

func pathAny(s STR) ANY {
	if !strHasDollar(s.any()) {
		return s.any()
	}

	if v := s.vfs(); v != 0 {
		return v.any()
	}

	return s.any()
}

func (a ANY) strID() uint32 {
	return uint32(a)
}

func internAny(s string) ANY {
	if vfsHasPrefix(s) {
		return intern(s).any()
	}

	return internStr(s).any()
}
