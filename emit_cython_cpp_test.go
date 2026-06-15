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
