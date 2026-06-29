package main

import (
	"reflect"
	"testing"
)

func TestGen_CrossModuleTextCopySourceTracked(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "src/tmpl.h.txt", "#pragma once\n// template\n")

	writeTestModuleFile(files, "owner/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/${MODDIR})\n"+
			"COPY_FILE(TEXT src/tmpl.h.txt ${BINDIR}/sub/tmpl.h)\nEND()\n")

	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(owner)\nSRCS(consumer.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/consumer.cpp",
		"#include <sub/tmpl.h>\nint f(){return 0;}\n")

	writeTestModuleFile(files, "root/ya.make",
		"PROGRAM()\nPEERDIR(consumer)\nSRCS(m.cpp)\nEND()\n")
	writeTestModuleFile(files, "root/m.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "root")

	consumerCC := mustNodeByOutput(t, g, "$(B)/consumer/consumer.cpp.o")

	if !nodeHasInput(consumerCC, "$(S)/src/tmpl.h.txt") {
		t.Errorf("consumer.cpp.o missing cross-module TEXT copy source $(S)/src/tmpl.h.txt: %v",
			vfsStringsT3(consumerCC.flatInputs()))
	}
}

func TestGen_CopyOfGeneratedPySrcCarriesSourceInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "mod/gen/bin", "gen")

	files["build/scripts/fs_tools.py"] = "import process_command_files as pcf\n"
	files["build/scripts/process_command_files.py"] = "\n"

	writeTestModuleFile(files, "other/other.h", "#pragma once\n")
	writeTestModuleFile(files, "mod/gen/gen.h", "#pragma once\n#include <other/other.h>\n")

	writeTestModuleFile(files, "mod/gen/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
RUN_PROGRAM(
    mod/gen/bin
        --save_file_path generated_consts.py
    IN_NOPARSE gen.h
    OUT_NOAUTO generated_consts.py
    CWD ${BINDIR}
)
END()
`)

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(mod/gen)
COPY_FILE(${ARCADIA_BUILD_ROOT}/mod/gen/generated_consts.py generated_consts.py)
PY_SRCS(generated_consts.py)
END()
`)

	g := testGen(newMemFS(files), "mod")

	cp := mustNodeByOutput(t, g, "$(B)/mod/generated_consts.py")

	for _, want := range []string{"$(S)/mod/gen/gen.h", "$(S)/other/other.h"} {
		if !nodeHasInput(cp, want) {
			t.Errorf("CP generated_consts.py missing producer closure %q: %v", want, vfsStringsT3(cp.flatInputs()))
		}
	}

	bc := mustNodeByOutput(t, g, "$(B)/mod/generated_consts.py.yapyc3")

	for _, want := range []string{
		"$(S)/mod/gen/gen.h",
		"$(S)/other/other.h",
		"$(S)/build/scripts/fs_tools.py",
		"$(S)/build/scripts/process_command_files.py",
	} {
		if !nodeHasInput(bc, want) {
			t.Errorf("bytecode generated_consts.py.yapyc3 missing %q: %v", want, vfsStringsT3(bc.flatInputs()))
		}
	}
}

func TestGen_PlainSourceCopyKeepsNoUnrelatedClosure(t *testing.T) {
	files := map[string]string{}

	files["build/scripts/fs_tools.py"] = "import process_command_files as pcf\n"
	files["build/scripts/process_command_files.py"] = "\n"

	writeTestModuleFile(files, "other/other.h", "#pragma once\n")
	writeTestModuleFile(files, "mod/orig.txt", "data\n")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
COPY_FILE(orig.txt copied.txt)
END()
`)

	g := testGen(newMemFS(files), "mod")

	cp := mustNodeByOutput(t, g, "$(B)/mod/copied.txt")

	if nodeHasInput(cp, "$(S)/other/other.h") {
		t.Errorf("plain source copy leaked unrelated closure: %v", vfsStringsT3(cp.flatInputs()))
	}
}

func TestGen_TextCopyResolvesIncludesInConsumerContext(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "shared/header.inc", `ADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/${MODDIR})
COPY_FILE(TEXT shared/tmpl.h.txt ${BINDIR}/tmpl.h)
COPY_FILE(TEXT shared/dep.h.txt ${BINDIR}/dep.h)
`)
	writeTestModuleFile(files, "shared/tmpl.h.txt", "#include <dep.h>\n")
	writeTestModuleFile(files, "shared/dep.h.txt", "#pragma once\n")

	writeTestModuleFile(files, "a/ya.make", "LIBRARY()\nINCLUDE(${ARCADIA_ROOT}/shared/header.inc)\nSRCS(a.cpp)\nEND()\n")
	writeTestModuleFile(files, "a/a.cpp", "#include <tmpl.h>\nint a(){return 0;}\n")
	writeTestModuleFile(files, "b/ya.make", "LIBRARY()\nINCLUDE(${ARCADIA_ROOT}/shared/header.inc)\nSRCS(b.cpp)\nEND()\n")
	writeTestModuleFile(files, "b/b.cpp", "#include <tmpl.h>\nint b(){return 0;}\n")

	writeTestModuleFile(files, "root/ya.make", "PROGRAM()\nPEERDIR(b a)\nSRCS(m.cpp)\nEND()\n")
	writeTestModuleFile(files, "root/m.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "root")

	aCC := mustNodeByOutput(t, g, "$(B)/a/a.cpp.o")
	bCC := mustNodeByOutput(t, g, "$(B)/b/b.cpp.o")

	if nodeHasInput(aCC, "$(B)/b/dep.h") {
		t.Errorf("a.cpp.o leaked peer copy $(B)/b/dep.h: %v", vfsStringsT3(aCC.flatInputs()))
	}

	if nodeHasInput(bCC, "$(B)/a/dep.h") {
		t.Errorf("b.cpp.o leaked peer copy $(B)/a/dep.h: %v", vfsStringsT3(bCC.flatInputs()))
	}

	if !nodeHasInput(aCC, "$(B)/a/dep.h") {
		t.Errorf("a.cpp.o missing own copy $(B)/a/dep.h: %v", vfsStringsT3(aCC.flatInputs()))
	}

	if !nodeHasInput(bCC, "$(B)/b/dep.h") {
		t.Errorf("b.cpp.o missing own copy $(B)/b/dep.h: %v", vfsStringsT3(bCC.flatInputs()))
	}
}

func TestGen_CopyFileWithContextAutoCompilesBuildOutput(t *testing.T) {
	files := map[string]string{}

	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(newMemFS(files), "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")

	for _, want := range []string{"$(B)/mod/copied.cpp", "$(S)/mod/original.cpp", "$(S)/mod/dep.h"} {
		if !nodeHasInput(cc, want) {
			t.Fatalf("copied.cpp inputs missing %q: %v", want, vfsStringsT3(cc.flatInputs()))
		}
	}

	if len(graphDeps(g, cc)) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(graphDeps(g, cc)))
	}
}

func TestGen_CopyFileWithContextExpandsBuildRootModdirDestination(t *testing.T) {
	files := map[string]string{}

	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    ${ARCADIA_BUILD_ROOT}/${MODDIR}/copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(newMemFS(files), "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")

	for _, want := range []string{"$(B)/mod/copied.cpp", "$(S)/mod/original.cpp", "$(S)/mod/dep.h"} {
		if !nodeHasInput(cc, want) {
			t.Fatalf("copied.cpp inputs missing %q: %v", want, vfsStringsT3(cc.flatInputs()))
		}
	}
}

func TestGen_CopyFileAutoRidesSourceAsNonExpandedLeaf(t *testing.T) {
	files := map[string]string{}

	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(newMemFS(files), "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{"$(B)/mod/copied.cpp"}

	if got := vfsStringsT3(cc.flatInputs()); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}

	if !slicesContains(vfsStringsT3(cc.flatInputs()), "$(S)/mod/original.cpp") {
		t.Fatalf("copied.cpp inputs should contain the AUTO source $(S)/mod/original.cpp: %v", vfsStringsT3(cc.flatInputs()))
	}

	for _, in := range vfsStringsT3(cc.flatInputs()) {
		if in == "$(S)/mod/dep.h" {
			t.Fatalf("copied.cpp inputs unexpectedly contain non-expanded-leaf's include $(S)/mod/dep.h: %v", vfsStringsT3(cc.flatInputs()))
		}
	}

	if len(graphDeps(g, cc)) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(graphDeps(g, cc)))
	}
}
