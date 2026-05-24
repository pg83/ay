package main

import "strings"

const (
	py3ccBinSubrel    = "tools/py3cc/bin/"
	py3ccCanonicalRel = "tools/py3cc/"
)

// canonicalizePy3ccBinary maps the host walker's /bin/ output back to the canonical path.
func canonicalizePy3ccBinary(v VFS) VFS {
	if !v.IsBuild() || !strings.HasPrefix(v.Rel, py3ccBinSubrel) {
		return v
	}

	return Build(py3ccCanonicalRel + v.Rel[len(py3ccBinSubrel):])
}

const runtimePy3ModulePath = "library/python/runtime_py3"

// runtimePy3CCExtraInputs returns extra CC inputs for runtime_py3 wrappers.
func runtimePy3CCExtraInputs(modulePath, srcRel string) []VFS {
	if modulePath != runtimePy3ModulePath {
		return nil
	}

	switch srcRel {
	case "__res.cpp":
		return []VFS{
			Build("library/python/runtime_py3/__res.pyc.inc"),
			Source("library/python/runtime_py3/__res.py"),
			Source("library/python/runtime_py3/sitecustomize.py"),
		}
	case "sitecustomize.cpp":
		return []VFS{
			Build("library/python/runtime_py3/sitecustomize.pyc.inc"),
			Source("library/python/runtime_py3/__res.py"),
			Source("library/python/runtime_py3/sitecustomize.py"),
		}
	}

	return nil
}
