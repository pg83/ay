package main

import "strings"

// py.go — emitter helpers for PY (Python) source-compilation nodes.
//
// yapyc3 shape: .py.yapyc3 / .py.<unit-pathid>.yapyc3 bytecode via tools/py3cc,
// driven by PY_SRCS() in PY3_LIBRARY / PY23_LIBRARY. cmd_args:
//   [py3cc, --slow-py3cc, slow_py3cc, <modulePath>/<srcRel>-,
//    $(S)/<path>/<src>, $(B)/<output>]
// Output suffix: flat src → .py.yapyc3; subdir src →
// .py.<pathid($S/unit)[:4]>.yapyc3.
// The show_out/objcopy RESOURCE shape is handled elsewhere.
//
// Binary canonicalization: tools/py3cc/bin LD lands at
// $(B)/tools/py3cc/bin/py3cc, but reference yapyc3 nodes invoke
// $(B)/tools/py3cc/py3cc — canonicalizePy3ccBinaryPath rewrites the /bin/
// segment. Same pattern as canonicalizeRagel6BinaryPath in r6.go.

const (
	py3ccBinSubpath   = "$(B)/tools/py3cc/bin/"
	py3ccCanonicalDir = "$(B)/tools/py3cc/"
)

// canonicalizePy3ccBinaryPath maps the host walker's
// $(B)/tools/py3cc/bin/<basename> output back to the reference-
// shaped $(B)/tools/py3cc/<basename>.  All other inputs pass
// through unchanged so the canonical-fallback codepath (which already
// supplies the correct literal) is not double-rewritten.
func canonicalizePy3ccBinaryPath(p string) string {
	if !strings.HasPrefix(p, py3ccBinSubpath) {
		return p
	}

	return py3ccCanonicalDir + p[len(py3ccBinSubpath):]
}

// runtimePy3ModulePath is the canonical path of the PY3_LIBRARY module
// whose ya.make declares ARCHIVE(NAME __res.pyc.inc DONTCOMPRESS __res.pyc)
// and ARCHIVE(NAME sitecustomize.pyc.inc DONTCOMPRESS sitecustomize.pyc).
// The two C++ sources __res.cpp and sitecustomize.cpp #include the
// generated .pyc.inc headers from $(B)/<modulePath>/; upstream
// also records the PY_SRCS-fed .py files as side inputs on each compile
// (the RUN_PROGRAM that pickles them feeds both sources to stage0pycc).
const runtimePy3ModulePath = "library/python/runtime_py3"

// runtimePy3CCExtraInputs returns extra CC inputs for __res.cpp /
// sitecustomize.cpp in library/python/runtime_py3. The runtime_py3 pipeline
// pickles __res.py/sitecustomize.py into .pyc and ARCHIVE-embeds them as
// .pyc.inc headers under $(B)/library/python/runtime_py3/; each C++ wrapper
// CC then carries the .pyc.inc and both .py sources as scanner-resolved
// inputs, even though only the .pyc.inc is textually #included. Mirroring
// this keeps the libpy3library-python-runtime_py3.{a,global.a} AR closure
// byte-shape-correct.
//
// pySrcsARExtraInputs returns the extra AR-input set for PY*_LIBRARY
// modules whose ya.make declares PY_SRCS and/or RESOURCE_FILES. Each entry
// flows through build/scripts/objcopy.py; the script and source paths land
// in the AR multiset because AR unions member-compile inputs.
//
// modulePath: canonical $(B)-relative path. srcDir: SRCDIR() (empty when
// none). pySrcs / resourcePaths: declaration-ordered macro args. Returns
// nil when nothing contributes.
func pySrcsARExtraInputs(modulePath, srcDir string, pySrcs, resourcePaths []string) []VFS {
	if len(pySrcs) == 0 && len(resourcePaths) == 0 {
		return nil
	}

	actualUnit := modulePath
	if srcDir != "" {
		actualUnit = srcDir
	}

	out := make([]VFS, 0, 1+len(pySrcs)+len(resourcePaths))
	out = append(out, Source("build/scripts/objcopy.py"))

	for _, srcRel := range pySrcs {
		out = append(out, Source(actualUnit+"/"+srcRel))
	}

	for _, srcRel := range resourcePaths {
		out = append(out, Source(modulePath+"/"+srcRel))
	}

	return out
}

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
