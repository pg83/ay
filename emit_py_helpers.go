package main

import "strings"

const (
	py3ccBinSubrel    = "tools/py3cc/bin/"
	py3ccCanonicalRel = "tools/py3cc/"
)

func canonicalizePy3ccBinary(v VFS) VFS {
	if !v.IsBuild() || !strings.HasPrefix(v.Rel(), py3ccBinSubrel) {
		return v
	}

	return Build(py3ccCanonicalRel + v.Rel()[len(py3ccBinSubrel):])
}
