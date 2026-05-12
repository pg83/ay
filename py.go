package main

import "strings"

// py.go — emitter helpers for PY (Python) source-compilation nodes.
//
// Two PY sub-shapes exist in the reference graph (sg2.json):
//
//  1. yapyc3 shape (536 nodes, no show_out flag):
//     Produces `.py.yapyc3` or `.py.3kp2.yapyc3` bytecode files by
//     invoking the host `tools/py3cc` binary. Driven by PY_SRCS()
//     declarations in PY3_LIBRARY / PY23_LIBRARY modules.
//     cmd_args: [py3cc_binary, --slow-py3cc, slow_py3cc_binary,
//                <modulePath>/<srcRel>-, $(SOURCE_ROOT)/<path>/<src>,
//                $(BUILD_ROOT)/<output>]
//     Output suffix: flat src (no `/`) → `.py.yapyc3`;
//                    subdir src (has `/`) → `.py.3kp2.yapyc3`.
//
//  2. show_out / objcopy shape (127 nodes, show_out=yes):
//     RESOURCE-based Python embedding; emitted by build/scripts/objcopy.py.
//     Deferred to a later PR — not handled here.
//
// Binary path canonicalization:
//   tools/py3cc/bin/ya.make declares PROGRAM(py3cc) with SRCDIR(tools/py3cc).
//   Walking tools/py3cc/bin produces an LD node at
//   $(BUILD_ROOT)/tools/py3cc/bin/py3cc, but the reference graph's yapyc3
//   nodes invoke py3cc at the canonical parent path
//   $(BUILD_ROOT)/tools/py3cc/py3cc.  canonicalizePy3ccBinaryPath rewrites
//   the /bin/ subpath back to the canonical location so cmd_args[0] matches
//   the reference byte-exact.  Same pattern as canonicalizeRagel6BinaryPath
//   in r6.go.

const (
	py3ccBinSubpath   = "$(BUILD_ROOT)/tools/py3cc/bin/"
	py3ccCanonicalDir = "$(BUILD_ROOT)/tools/py3cc/"
)

// canonicalizePy3ccBinaryPath maps the host walker's
// $(BUILD_ROOT)/tools/py3cc/bin/<basename> output back to the reference-
// shaped $(BUILD_ROOT)/tools/py3cc/<basename>.  All other inputs pass
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
// generated .pyc.inc headers from $(BUILD_ROOT)/<modulePath>/; upstream
// also records the PY_SRCS-fed .py files as side inputs on each compile
// (the RUN_PROGRAM that pickles them feeds both sources to stage0pycc).
const runtimePy3ModulePath = "library/python/runtime_py3"

// runtimePy3CCExtraInputs returns the additional CC-input set the
// reference graph attaches to CC nodes that compile __res.cpp /
// sitecustomize.cpp in the runtime_py3 module. Returns nil for any
// other (module, source) pair so the helper is scoped tightly.
//
// The runtime_py3 ya.make pipeline is:
//   - PY_SRCS(entry_points.py) → yapyc3 output
//   - RUN_PROGRAM(stage0pycc … IN __res.py sitecustomize.py …)
//     pickles __res.py and sitecustomize.py into __res.pyc /
//     sitecustomize.pyc
//   - ARCHIVE(NAME __res.pyc.inc DONTCOMPRESS __res.pyc) embeds the
//     pickle as a C array header `__res.pyc.inc` under $(BUILD_ROOT)/
//     library/python/runtime_py3/
//   - SRCS(__res.cpp sitecustomize.cpp GLOBAL runtime_reg_py3.cpp)
//     compile the C++ wrappers that #include the .pyc.inc headers
//
// As a result, each CC node carries the .pyc.inc header (BUILD_ROOT
// path) and the two PY_SRCS-input python source paths as scanner-
// resolved inputs even though only the .pyc.inc is in the textual
// include chain. Mirroring this here keeps the AR closure
// (libpy3library-python-runtime_py3.{a,global.a}) byte-shape-correct
// against the reference graph.
// pySrcsARExtraInputs returns the additional AR-input set the reference
// graph attaches to the regular .a / .global.a of every PY*_LIBRARY
// whose ya.make declares PY_SRCS and/or RESOURCE_FILES. Each PY_SRCS or
// RESOURCE_FILES entry flows into the module's objcopy emitter via
// build/scripts/objcopy.py; both the script and the source paths land
// in the AR's input multiset because the AR pulls the union of its
// member compiles' inputs.
//
// modulePath is the module's canonical $(BUILD_ROOT)-relative path.
// srcDir is the SRCDIR(...) setting (empty when none). pySrcs is the
// declaration-ordered list of PY_SRCS entries from the ya.make.
// resourcePaths is the list of RESOURCE/RESOURCE_FILES source path
// entries (non-kv-only). Returns nil when neither input contributes
// any extras.
func pySrcsARExtraInputs(modulePath, srcDir string, pySrcs, resourcePaths []string) []string {
	if len(pySrcs) == 0 && len(resourcePaths) == 0 {
		return nil
	}

	actualUnit := modulePath
	if srcDir != "" {
		actualUnit = srcDir
	}

	out := make([]string, 0, 1+len(pySrcs)+len(resourcePaths))
	out = append(out, "$(SOURCE_ROOT)/build/scripts/objcopy.py")

	for _, srcRel := range pySrcs {
		out = append(out, "$(SOURCE_ROOT)/"+actualUnit+"/"+srcRel)
	}

	for _, srcRel := range resourcePaths {
		out = append(out, "$(SOURCE_ROOT)/"+modulePath+"/"+srcRel)
	}

	return out
}

func runtimePy3CCExtraInputs(modulePath, srcRel string) []string {
	if modulePath != runtimePy3ModulePath {
		return nil
	}

	switch srcRel {
	case "__res.cpp":
		return []string{
			"$(BUILD_ROOT)/library/python/runtime_py3/__res.pyc.inc",
			"$(SOURCE_ROOT)/library/python/runtime_py3/__res.py",
			"$(SOURCE_ROOT)/library/python/runtime_py3/sitecustomize.py",
		}
	case "sitecustomize.cpp":
		return []string{
			"$(BUILD_ROOT)/library/python/runtime_py3/sitecustomize.pyc.inc",
			"$(SOURCE_ROOT)/library/python/runtime_py3/__res.py",
			"$(SOURCE_ROOT)/library/python/runtime_py3/sitecustomize.py",
		}
	}

	return nil
}
