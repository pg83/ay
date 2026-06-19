package main

import (
	"strings"
	"testing"
)

func TestCythonImplicitFallthrough(t *testing.T) {
	tests := []struct {
		name        string
		stmt        *CythonStmt
		py23Variant bool
		want        bool
	}{
		{
			name:        "pyx in py3 library",
			stmt:        &CythonStmt{Src: "foo.pyx"},
			py23Variant: false,
			want:        true,
		},
		{
			name:        "pyx in py23 library",
			stmt:        &CythonStmt{Src: "foo.pyx"},
			py23Variant: true,
			want:        true,
		},
		{
			name:        "py source in py23 library",
			stmt:        &CythonStmt{Src: "graph.py"},
			py23Variant: true,
			want:        true,
		},
		{
			name:        "py source in py3 library",
			stmt:        &CythonStmt{Src: "graph.py"},
			py23Variant: false,
			want:        false,
		},
		{
			name:        "cmode never gets the flag",
			stmt:        &CythonStmt{Src: "foo.pyx", CMode: true},
			py23Variant: false,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cythonImplicitFallthrough(tt.stmt, tt.py23Variant)
			if got != tt.want {
				t.Fatalf("cythonImplicitFallthrough(%+v, %t) = %t, want %t", *tt.stmt, tt.py23Variant, got, tt.want)
			}
		})
	}
}

func TestGen_CythonizePyFollowsCythonCMode(t *testing.T) {
	// Upstream pybuild.py: CYTHONIZE_PY only flips a flag; a following `.py`
	// source is appended to whatever pyxs list the last CYTHON_C/CYTHON_CPP
	// directive selected. After CYTHON_C the .py is built in C mode and named
	// `<src>.py.c` (DEP variant keeps the extension), like gevent's
	// `_abstract_linkable.py.c`.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nPY_SRCS(NAMESPACE pkg CYTHON_C mod.pyx CYTHONIZE_PY helper.py)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "def f():\n    return 0\n")
	writeTestModuleFile(files, "pkg/helper.py", "def g():\n    return 1\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/helper.py.c")
	if nodeByOutput(g, "$(B)/pkg/helper.py.cpp") != nil {
		t.Fatalf("CYTHONIZE_PY .py after CYTHON_C must not emit a C++ output")
	}

	for _, a := range cy.Cmds[0].CmdArgs.flat() {
		if a.string() == "--cplus" {
			t.Fatalf("C-mode cython invocation must not pass --cplus: %#v", cy.Cmds[0].CmdArgs.flat())
		}
	}
}

func TestGen_CythonCApiHeaderEmitsCompanionHeaders(t *testing.T) {
	// Upstream _BUILDWITH_CYTHON_C_API_H uses `noext` naming and emits the
	// generated `.c` plus companion `.h` and `_api.h` outputs, like lxml's
	// etree.pyx -> etree.c + etree.h + etree_api.h.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nPY_SRCS(NAMESPACE pkg CYTHON_C_API_H etree.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/etree.pyx", "def f():\n    return 0\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/etree.c")
	if nodeByOutput(g, "$(B)/pkg/etree.pyx.c") != nil {
		t.Fatalf("CYTHON_C_API_H must use noext naming (etree.c), not etree.pyx.c")
	}

	got := make(map[string]bool)
	for _, o := range cy.Outputs {
		got[o.string()] = true
	}

	for _, want := range []string{"$(B)/pkg/etree.c", "$(B)/pkg/etree.h", "$(B)/pkg/etree_api.h"} {
		if !got[want] {
			t.Fatalf("CY node missing output %q; outputs=%v", want, cy.Outputs)
		}
	}
}

func TestGen_CythonizePyPxdSideInputClosure(t *testing.T) {
	// Upstream pybuild.py: for a CYTHONIZE_PY `.py` source whose module has a
	// sibling `<mod-as-path>.pxd`, `dep = pxd` is passed to the cython macro as
	// `${hide;input:Dep}` — a hidden input whose transitive cimport / extern-from
	// closure rides the CY node (gevent _abstract_linkable.py.c carries
	// _gevent_c_abstract_linkable.pxd + its cimported pxds + greenlet.h).
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(TOP_LEVEL CYTHON_C gevent/mod.pyx CYTHONIZE_PY gevent/helper.py=gevent._helper gevent/plain.py=gevent._plain)\nEND()\n")
	writeTestModuleFile(files, "pkg/gevent/mod.pyx", "def f():\n    return 0\n")
	writeTestModuleFile(files, "pkg/gevent/helper.py", "def g():\n    return 1\n")
	writeTestModuleFile(files, "pkg/gevent/plain.py", "def h():\n    return 2\n")
	writeTestModuleFile(files, "pkg/gevent/_helper.pxd",
		"from gevent._dep cimport thing\ncdef extern from \"gevent/api.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/gevent/_dep.pxd", "cdef int x\n")
	writeTestModuleFile(files, "pkg/gevent/api.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/gevent/helper.py.c")

	counts := map[string]int{}
	for _, in := range cy.flatInputs() {
		counts[in.string()]++
	}

	for _, want := range []string{
		"$(S)/pkg/gevent/_helper.pxd",
		"$(S)/pkg/gevent/_dep.pxd",
		"$(S)/pkg/gevent/api.h",
	} {
		switch counts[want] {
		case 0:
			t.Fatalf("CY node missing pxd-side input %q; inputs=%v", want, cy.flatInputs())
		case 1:
		default:
			t.Fatalf("CY node lists pxd-side input %q %d times, want exactly once", want, counts[want])
		}
	}

	// The generated .c's compile node carries the same pxd closure (matching the
	// .pyx case, whose compile already lists its own cimport closure).
	cc := mustNodeByOutputSuffix(t, g, "gevent/helper.py.c.o")

	ccInputs := map[string]bool{}
	for _, in := range cc.flatInputs() {
		ccInputs[in.string()] = true
	}

	for _, want := range []string{
		"$(S)/pkg/gevent/_helper.pxd",
		"$(S)/pkg/gevent/_dep.pxd",
		"$(S)/pkg/gevent/api.h",
	} {
		if !ccInputs[want] {
			t.Fatalf("generated-.c compile node missing pxd-side input %q; inputs=%v", want, cc.flatInputs())
		}
	}

	// A CYTHONIZE_PY .py with no matching pxd carries no pxd side input.
	plain := mustNodeByOutput(t, g, "$(B)/pkg/gevent/plain.py.c")
	for _, in := range plain.flatInputs() {
		if strings.HasSuffix(in.string(), ".pxd") {
			t.Fatalf("CY node for a pxd-less .py must not carry a pxd input: %q", in.string())
		}
	}
}

func TestGen_CythonPyxCarriesNoPxdDep(t *testing.T) {
	// Upstream pybuild.py: the cython macro's hidden `Dep` is `dep = path` for a
	// `.pyx` source (the source itself, already an input → dedup no-op). Only a
	// CYTHONIZE_PY `.py` source turns `dep` into `<mod-as-path>.pxd`. A `.pyx`
	// whose `<mod-as-path>.pxd` resolves but is not cimported must therefore NOT
	// carry that pxd (or its closure) as a side input — that pxd rides a `.pyx`
	// only through its own cimport scan, never through `Dep`.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(NAMESPACE pkg sub/mod.pyx=foo)\nEND()\n")
	writeTestModuleFile(files, "pkg/sub/mod.pyx", "def f():\n    return 0\n")
	// `<mod-as-path>.pxd` for mod name `foo` resolves under the module dir, but
	// the .pyx does not cimport it — upstream's `dep == path` excludes it.
	writeTestModuleFile(files, "pkg/foo.pxd", "cdef extern from \"pkg/extra.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/extra.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/sub/mod.pyx.cpp")
	for _, in := range cy.flatInputs() {
		s := in.string()
		if s == "$(S)/pkg/foo.pxd" || s == "$(S)/pkg/extra.h" {
			t.Fatalf("CY node for a .pyx must not carry a non-cimported <mod>.pxd Dep input %q; inputs=%v", s, cy.flatInputs())
		}
	}
}

func TestGen_ManualCompanionSourceUsesCythonCompanionCCInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\nPY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/helper.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "def f():\n    return 0\n")

	g := testGen(newMemFS(files), "pkg")
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	args := helper.Cmds[0].CmdArgs.flat()

	pythonIncludeIdx := indexOfArg(args, "-I$(S)/contrib/libs/python/Include")
	if pythonIncludeIdx < 0 {
		t.Fatalf("helper.cpp.o cmd_args missing python include: %#v", args)
	}

	wantNumpy := []string{
		"-I$(S)/contrib/python/numpy/include/numpy/core/include",
		"-I$(S)/contrib/python/numpy/include/numpy/core/include/numpy",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/common",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/npymath",
		"-I$(S)/contrib/python/numpy/include/numpy/distutils/include",
	}

	if pythonIncludeIdx+1+len(wantNumpy) > len(args) {
		t.Fatalf("helper.cpp.o cmd_args too short for numpy include bundle: %#v", args)
	}

	for i, want := range wantNumpy {
		if got := args[pythonIncludeIdx+1+i].string(); got != want {
			t.Fatalf("numpy include bundle mismatch at offset %d: got %q, want %q; cmd_args=%#v", i, got, want, args)
		}
	}

	for _, arg := range strStrs(args) {
		if strings.HasPrefix(arg, "-DPyInit_") || strings.HasPrefix(arg, "-Dinit_module_") {
			t.Fatalf("helper.cpp.o cmd_args still carry PY_REGISTER define %q: %#v", arg, args)
		}
	}
}
