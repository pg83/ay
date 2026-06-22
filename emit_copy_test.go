package main

import (
	"reflect"
	"testing"
)

// TestGen_TextCopyResolvesIncludesInConsumerContext reproduces the cross-module
// COPY_FILE(TEXT) contamination: two sibling libraries each TEXT-copy the same
// shared template header into their own $(B) staging, each exporting it via
// GLOBAL ADDINCL and staging its own dep.h. The template angle-includes <dep.h>.
//
// Because the TEXT-copy *source* node is parsed-and-resolved once and cached by
// absID (IncludeScanner.childrenCache), the first module to reach it fixes
// <dep.h>'s resolution for BOTH consumers — leaking the peer's staging copy.
//
// Each module's COPY output must instead resolve the template's includes in ITS
// OWN module context, so a never sees $(B)/b/dep.h and vice-versa.
// TestGen_CrossModuleTextCopySourceTracked verifies that when a CC node in
// module "consumer" includes a header produced by COPY_FILE(TEXT) in a
// *different* module "owner", the original .txt source is tracked as a leaf
// input of the CC node — upstream still records the owning module's .txt file
// as a real compiler input.
func TestGen_CrossModuleTextCopySourceTracked(t *testing.T) {
	files := map[string]string{}

	// src/tmpl.h.txt lives at source root; owner/ TEXT-copies it.
	writeTestModuleFile(files, "src/tmpl.h.txt", "#pragma once\n// template\n")

	// owner: TEXT-copies the source-root file into $(B)/owner/sub/tmpl.h, then
	// exports that directory globally so consumers can #include <sub/tmpl.h>.
	writeTestModuleFile(files, "owner/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/${MODDIR})\n"+
			"COPY_FILE(TEXT src/tmpl.h.txt ${BINDIR}/sub/tmpl.h)\nEND()\n")

	// consumer: PEERDIRs owner and includes the generated header.
	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(owner)\nSRCS(consumer.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/consumer.cpp",
		"#include <sub/tmpl.h>\nint f(){return 0;}\n")

	writeTestModuleFile(files, "root/ya.make",
		"PROGRAM()\nPEERDIR(consumer)\nSRCS(m.cpp)\nEND()\n")
	writeTestModuleFile(files, "root/m.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "root")

	consumerCC := mustNodeByOutput(t, g, "$(B)/consumer/consumer.cpp.o")

	// The .h.txt source must appear as a leaf input.
	if !nodeHasInput(consumerCC, "$(S)/src/tmpl.h.txt") {
		t.Errorf("consumer.cpp.o missing cross-module TEXT copy source $(S)/src/tmpl.h.txt: %v",
			vfsStringsT3(consumerCC.flatInputs()))
	}
}

// TestGen_Py23LibraryCopyFileCarriesPy3Tag reproduces the missing module_tag on
// COPY_FILE nodes owned by a PY23_LIBRARY. Upstream attributes the submodule's
// MODULE_TAG (py3) to every node it owns, including the .py copy node. A plain
// LIBRARY copy must NOT gain any tag.
func TestGen_Py23LibraryCopyFileCarriesPy3Tag(t *testing.T) {
	files := map[string]string{}
	mkdirWrite := func(rel, body string) { files[rel] = body }

	mkdirWrite("pylib/ya.make", `PY23_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
COPY_FILE(src/keys.py keys.py)
END()
`)
	mkdirWrite("src/keys.py", "KEY = 1\n")

	mkdirWrite("plain/ya.make", `LIBRARY()
COPY_FILE(src/plain.txt plain.txt)
END()
`)
	mkdirWrite("src/plain.txt", "data\n")

	fs := newMemFS(files)

	g := testGen(fs, "pylib")
	cp := findGraphNodeByOutputs(t, g, "$(B)/pylib/keys.py")
	if got := cp.TargetProperties.ModuleTag; got != tagPy3 {
		t.Fatalf("PY23_LIBRARY COPY_FILE module_tag = %q, want py3", got.string())
	}

	gp := testGen(fs, "plain")
	plainCP := findGraphNodeByOutputs(t, gp, "$(B)/plain/plain.txt")
	if got := plainCP.TargetProperties.ModuleTag; got != 0 {
		t.Fatalf("plain LIBRARY COPY_FILE module_tag = %q, want empty", got.string())
	}
}

// TestGen_CopyOfGeneratedPySrcCarriesProducerClosure reproduces the cross-module
// generated-source-closure residual: a child PY3_LIBRARY RUN_PROGRAM produces
// generated_consts.py (OUT_NOAUTO); a parent PY3_LIBRARY COPY_FILEs that build
// output into its own generated_consts.py and PY_SRCSes it. Upstream's flat
// input model lists the producer's transitive $(S) closure on (1) the CP action
// copying the generated file and (2) the py3cc bytecode action compiling the
// copied file — the latter additionally folds the CP's own copy-tool scripts.
// Neither is carried until COPY_FILE propagates the ProducerSourceClosure.
func TestGen_CopyOfGeneratedPySrcCarriesProducerClosure(t *testing.T) {
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

	// Child: RUN_PROGRAM produces generated_consts.py from gen.h's closure.
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

	// Parent: COPY_FILE the child's build output and PY_SRCS it.
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

	// (1) The CP action carries the producer's transitive $(S) closure.
	cp := mustNodeByOutput(t, g, "$(B)/mod/generated_consts.py")
	for _, want := range []string{"$(S)/mod/gen/gen.h", "$(S)/other/other.h"} {
		if !nodeHasInput(cp, want) {
			t.Errorf("CP generated_consts.py missing producer closure %q: %v", want, vfsStringsT3(cp.flatInputs()))
		}
	}

	// (2) The bytecode action carries the same closure PLUS the CP's own
	// copy-tool scripts.
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

// TestGen_PlainSourceCopyKeepsNoProducerClosure is the control: an ordinary
// COPY_FILE of a $(S) source (not a registered generated output) registers no
// ProducerSourceClosure, so its dst carries only the copy-tool scripts and the
// source.
func TestGen_PlainSourceCopyKeepsNoProducerClosure(t *testing.T) {
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
	// A plain source copy never resolves a generated producer, so the unrelated
	// other/other.h and any producer closure stay absent.
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
	// Input order is irrelevant to self_uid (normalization sorts inputs); assert
	// membership. The WITH_CONTEXT source's #includes resolve in the dst's own
	// context (raw directives spliced onto the per-module dst), and the $(S)
	// source is re-attached as a leaf, so dep.h and original.cpp may appear in
	// either order.
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
	// Input order is irrelevant to self_uid (normalization sorts inputs); assert
	// membership. The WITH_CONTEXT source's #includes resolve in the dst's own
	// context (raw directives spliced onto the per-module dst), and the $(S)
	// source is re-attached as a leaf, so dep.h and original.cpp may appear in
	// either order.
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
	// AUTO COPY materializes both the $(S) source and the $(B) copy; upstream
	// lists both, so the source rides as a closure leaf of the dst.
	if !slicesContains(vfsStringsT3(cc.flatInputs()), "$(S)/mod/original.cpp") {
		t.Fatalf("copied.cpp inputs should contain the AUTO source $(S)/mod/original.cpp: %v", vfsStringsT3(cc.flatInputs()))
	}
	// The leaf is NON-expanded: original.cpp's own #include "dep.h" must not be
	// followed, so dep.h does not leak into the dst's inputs.
	for _, in := range vfsStringsT3(cc.flatInputs()) {
		if in == "$(S)/mod/dep.h" {
			t.Fatalf("copied.cpp inputs unexpectedly contain non-expanded-leaf's include $(S)/mod/dep.h: %v", vfsStringsT3(cc.flatInputs()))
		}
	}
	if len(graphDeps(g, cc)) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(graphDeps(g, cc)))
	}
}
