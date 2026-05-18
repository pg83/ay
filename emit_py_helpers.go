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

// pySrcsARExtraInputs returns the extra AR-input set for PY*_LIBRARY modules.
func pySrcsARExtraInputs(modulePath string, srcDir *string, pySrcs []string, generatedPySrcs map[string][]VFS, resourcePaths []string) []VFS {
	if len(pySrcs) == 0 && len(resourcePaths) == 0 {
		return nil
	}

	actualUnit := modulePath
	if srcDir != nil {
		actualUnit = *srcDir
	}

	out := make([]VFS, 0, 1+len(pySrcs)+len(resourcePaths))
	out = append(out, Source("build/scripts/objcopy.py"))

	for _, srcRel := range pySrcs {
		if generatedPySrcs[srcRel] != nil {
			out = append(out, Build(modulePath+"/"+srcRel))
		} else {
			out = append(out, Source(actualUnit+"/"+srcRel))
		}
	}

	for _, srcRel := range resourcePaths {
		out = append(out, Source(modulePath+"/"+srcRel))
	}

	return out
}

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
