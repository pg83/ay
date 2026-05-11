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
