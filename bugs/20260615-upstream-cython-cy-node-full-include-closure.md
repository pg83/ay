# Upstream over-declaration: cython (CY) node carries the generated `.cpp`'s full include closure

## Summary

Upstream ymake attaches the **full transitive C/C++ include closure of the
generated translation unit** to the cython node (`kv.p=CY`, the
`cython.py … .pyx -o …pyx.cpp` action), even though `cython.py` does **not** read
any of those headers. Cython parses `.pyx`/`.pxd`/`.pxi` and embeds the
`Cython/Utility/*` templates; for a `cdef extern from "X.h"` block it emits a
literal `#include "X.h"` into the generated `.cpp` **without ever opening
`X.h`**. So the libcxx / python / numpy header closure on the CY node is a pure
cache-key dependency set, not an action-input set — an over-declaration from a
strict-incrementality standpoint (a libcxx header change needlessly re-runs
cython, whose output is independent of that header's contents).

We reproduce it for byte-exact parity (sg6: 45 CY nodes). This document records
that the closure is upstream behavior, not our model — and pins the one path
(the `output_include` python shim) where our reproduction was incomplete.

## cython does not parse `.h` (evidence)

`contrib/tools/cython/Cython/Compiler/ModuleNode.py`:

- `#include` lines for the C output are emitted verbatim, e.g.
  `h_code.putln('#include "Python.h"')` (lines 365/476), and
  `generate_includes()` (line 1066) emits `#include <omp.h>` /
  the `cdef extern` headers as text.
- `search_include_file` (line 687/695) resolves cython `include "…"` (`.pxi`)
  directives — cython-level source includes — **not** C headers.
- There is no C-header parser in `Cython/Compiler/`: `cdef extern from "X.h"`
  trusts the in-`.pxd` declarations and turns into an `#include "X.h"` string.

Operational criterion: `cython` produces the `.cpp` even if the referenced
`X.h` is absent; only the later C compile fails. So `X.h`'s contents (and its
transitive `#include`s) are not inputs of the cython action.

## What upstream attaches anyway

Reference CY node `$(B)/library/python/codecs/__codecs.pyx.py3.cpp` —
**1263 inputs**, a full compile closure:

| count | prefix |
|------:|--------|
| 837 | `contrib/libs/cxxsupp` (libcxx `<vector>`, `<string>`, `__algorithm/*`, …) |
| 138 | `contrib/tools/python3` |
| 83  | `contrib/python/numpy` |
| 75  | `contrib/tools/python` (py2 src — incl. `longintrepr.h`) |
| 38  | `contrib/tools/cython` |
| 20  | `contrib/libs/glibcasm` |
| 11  | `contrib/libs/python` |
| …   | glibc compat, `library/cpp/blockcodecs`, `util/charset`, … |

The genuine cython action inputs are the small non-header set: `cython.py`, the
`Cython/Utility/*.c/.pyx/.pxd` templates, `__codecs.pyx`, the cimported `.pxd`
(`util/generic/string.pxd`, `libcpp/string.pxd`, …), `pyconfig.h.in`. Everything
else (the 837 libcxx + python + numpy headers) is the scanned include closure.

### How the closure is assembled (two paths)

1. **cimport → `cdef extern` → header.** ymake's include scanner walks the
   `.pyx`, follows `cimport`s into `.pxd`s, treats each `cdef extern from "X.h"`
   as an include edge, and pulls `X.h`'s transitive C closure (this is the 837
   libcxx + most python headers).
2. **`output_include` declared on the cython command.** `build/conf/python.conf`:
   `PY_COMMON_CYTHON_OUTPUT_INCLUDES` lists python headers, wired into the
   cython `.CMD` via `${hide;output_include:"…"}` (`CYTHON_OUTPUT_INCLUDES` /
   `CYTHON_CPP_OUTPUT_INCLUDES`, ymake.core.conf:3364/3380). `OUTPUT_INCLUDES`
   = "output file dependencies": ymake resolves each declared header and scans
   it transitively, attaching the result to the producing (CY) node.

## The `longintrepr.h` datum (our reproduction gap)

`PY_COMMON_CYTHON_OUTPUT_INCLUDES` lists `$PY3_BASE_INCLUDE_DIR/longintrepr.h` =
`contrib/libs/python/Include/longintrepr.h`, a **shim**:

```c
#ifdef USE_PYTHON3
#error "No <longintrepr.h> in Python3"
#else
#include <contrib/tools/python/src/Include/longintrepr.h>
#endif
```

ymake's scanner does not evaluate `#ifdef`; it follows the `#else` `#include`
and adds `contrib/tools/python/src/Include/longintrepr.h` (the py2 target). That
target is reached **only** through this shim — nothing under
`contrib/tools/python/src/Include/` `#include`s it, and the `.pyx` cimport path
does not reach it.

Our CY node has 1262 inputs — identical to ref minus exactly
`$(S)/contrib/tools/python/src/Include/longintrepr.h`. Cause: we mirror the
`output_include` set as a hardcoded list (`py3CythonOutputIncludes`,
`emit_cython_cpp.go`) and add those headers to the node **bare (unscanned)**, so
the shim's `#else` `#include` is never followed. Path 1 (the `.pyx`/`.pxd`
cimport closure) we scan correctly, which is why the other 1262 match.

Node dumps captured: `debug/cy_codecs_ref.json` (1263), `debug/cy_codecs_our.json`
(1262).

## Status

Open. For byte-exact sg6 parity we must reproduce the closure, including the
`output_include`-only `longintrepr.h` target. The fix concerns how the
`output_include` headers feed the CY node (currently bare vs. ymake's scanned),
tracked separately. This document records that the broad CY-node closure itself
is an upstream over-declaration (cython reads none of those headers), not a
property our model independently needs.
