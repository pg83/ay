package main

import "strings"

var (
	// Path constants hoisted by `ay refac consts`.
	bldLibraryPythonRuntimePy3ResPycInc           = Build("library/python/runtime_py3/__res.pyc.inc")
	bldLibraryPythonRuntimePy3SitecustomizePycInc = Build("library/python/runtime_py3/sitecustomize.pyc.inc")
	libraryPythonRuntimePy3ResPy                  = Source("library/python/runtime_py3/__res.py")
	libraryPythonRuntimePy3SitecustomizePy        = Source("library/python/runtime_py3/sitecustomize.py")
)

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

const runtimePy3ModulePath = "library/python/runtime_py3"

func runtimePy3CCExtraInputs(modulePath, srcRel string) []VFS {
	if modulePath != runtimePy3ModulePath {
		return nil
	}

	switch srcRel {
	case "__res.cpp":
		return []VFS{
			bldLibraryPythonRuntimePy3ResPycInc,
			libraryPythonRuntimePy3ResPy,
			libraryPythonRuntimePy3SitecustomizePy,
		}
	case "sitecustomize.cpp":
		return []VFS{
			bldLibraryPythonRuntimePy3SitecustomizePycInc,
			libraryPythonRuntimePy3ResPy,
			libraryPythonRuntimePy3SitecustomizePy,
		}
	}

	return nil
}
