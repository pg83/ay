package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGen_ManualCompanionSourceUsesCythonCompanionCCInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(t, root, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\nPY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(t, root, "pkg/helper.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "pkg/mod.pyx", "def f():\n    return 0\n")

	g := testGen(root, "pkg")
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	args := helper.Cmds[0].CmdArgs

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
		if got := args[pythonIncludeIdx+1+i]; got != want {
			t.Fatalf("numpy include bundle mismatch at offset %d: got %q, want %q; cmd_args=%#v", i, got, want, args)
		}
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "-DPyInit_") || strings.HasPrefix(arg, "-Dinit_module_") {
			t.Fatalf("helper.cpp.o cmd_args still carry PY_REGISTER define %q: %#v", arg, args)
		}
	}
}

func TestGen_LibraryARIncludesResourceObjcopyMemberInputs(t *testing.T) {
	root := t.TempDir()

	writeTestModuleFile(t, root, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(t, root, "tools/rescompiler/bin", "rescompiler")
	writeToolProgram(t, root, "tools/rescompressor/bin", "rescompressor")

	writeTestModuleFile(t, root, "db/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nRESOURCE(data.sql key)\nEND()\n")
	writeTestModuleFile(t, root, "db/main.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(t, root, "db/data.sql", "select 1;\n")

	g := testGen(root, "db")
	regularAR := mustNodeByOutput(t, g, "$(B)/db/libdb.a")
	mustNodeByOutput(t, g, "$(B)/db/libdb.global.a")
	if findNodeByOutputPrefix(g, "$(B)/db/objcopy_") == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	for _, want := range []string{"$(S)/db/data.sql", "$(S)/build/scripts/objcopy.py"} {
		if !nodeHasInput(regularAR, want) {
			t.Fatalf("libdb.a inputs missing %q: %#v", want, regularAR.Inputs)
		}
	}
}

func writeToolProgram(t *testing.T, root, modulePath, binaryName string) {
	t.Helper()

	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "ya.make")), "PROGRAM("+binaryName+")\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(t, root, filepath.ToSlash(filepath.Join(modulePath, "main.cpp")), "int main(){return 0;}\n")
}

func writeTestModuleFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustNodeByOutput(t *testing.T, g *Graph, output string) *Node {
	t.Helper()

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0].String() == output {
			return n
		}
	}

	t.Fatalf("graph is missing output %q", output)
	return nil
}

func findNodeByOutputPrefix(g *Graph, prefix string) *Node {
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].String(), prefix) {
			return n
		}
	}

	return nil
}

func nodeHasInput(n *Node, input string) bool {
	for _, got := range n.Inputs {
		if got.String() == input {
			return true
		}
	}

	return false
}

func indexOfArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}

	return -1
}
