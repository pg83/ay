package main

import (
	"reflect"
	"testing"
)

// TestGen_TextCopyResolvesIncludesInConsumerContext reproduces the cross-module
// COPY_FILE(TEXT) contamination: two sibling libraries each TEXT-copy the same
// shared template header (shared/tmpl.h.txt) into their own $(B) staging, each
// exporting that staging via GLOBAL ADDINCL, and each also stages its own
// dep.h. The template angle-includes <dep.h>.
//
// Because the TEXT-copy *source* node ($(S)/shared/tmpl.h.txt) is parsed-and-
// resolved exactly once and cached by absID (IncludeScanner.childrenCache), the
// first module to reach it fixes <dep.h>'s resolution for BOTH consumers —
// leaking the peer module's staging copy into the other's translation unit.
//
// Each module's COPY output must instead resolve the template's includes in ITS
// OWN module context (the per-module dst is the unit of resolution), so a never
// sees $(B)/b/dep.h and vice-versa.
// TestGen_CrossModuleTextCopySourceTracked verifies that when a CC node in
// module "consumer" includes a header produced by COPY_FILE(TEXT) in a
// *different* module "owner", the original .txt source is tracked as a leaf
// input of the CC node. This mirrors upstream ymake's behaviour: a TEXT-mode
// copy whose source lives in another module still records the owning module's
// .txt file as a real compiler input.
func TestGen_CrossModuleTextCopySourceTracked(t *testing.T) {
	files := map[string]string{}

	// src/tmpl.h.txt lives at arcadia root; owner/ TEXT-copies it.
	writeTestModuleFile(files, "src/tmpl.h.txt", "#pragma once\n// template\n")

	// owner: TEXT-copies the arcadia-root source into $(B)/owner/sub/tmpl.h,
	// then exports that directory globally so consumers can #include <sub/tmpl.h>.
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

	// The .h.txt source must appear as a leaf input — upstream always tracks it.
	if !nodeHasInput(consumerCC, "$(S)/src/tmpl.h.txt") {
		t.Errorf("consumer.cpp.o missing cross-module TEXT copy source $(S)/src/tmpl.h.txt: %v",
			vfsStringsT3(consumerCC.flatInputs()))
	}
}

// TestGen_Py23LibraryCopyFileCarriesPy3Tag reproduces the missing module_tag on
// COPY_FILE nodes owned by a PY23_LIBRARY. Upstream attributes the submodule's
// MODULE_TAG (py3) to every node it owns, including the .py copy node; ay drops
// it. A plain LIBRARY copy must NOT gain any tag.
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
	// membership. The WITH_CONTEXT source's #includes now resolve in the dst's
	// own context (its raw directives are spliced onto the per-module dst), and
	// the $(S) source is re-attached as a leaf input, so dep.h and original.cpp
	// may appear in either relative order.
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
	// membership. The WITH_CONTEXT source's #includes now resolve in the dst's
	// own context (its raw directives are spliced onto the per-module dst), and
	// the $(S) source is re-attached as a leaf input, so dep.h and original.cpp
	// may appear in either relative order.
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
	// AUTO COPY materializes both $(S)/mod/original.cpp and $(B)/mod/copied.cpp;
	// upstream lists both, so the source rides as a closure leaf of the dst.
	if !slicesContains(vfsStringsT3(cc.flatInputs()), "$(S)/mod/original.cpp") {
		t.Fatalf("copied.cpp inputs should contain the AUTO source $(S)/mod/original.cpp: %v", vfsStringsT3(cc.flatInputs()))
	}
	// The leaf is NON-expanded: original.cpp's own #include "dep.h" must not be
	// followed, so $(S)/mod/dep.h does not leak into the dst's inputs.
	for _, in := range vfsStringsT3(cc.flatInputs()) {
		if in == "$(S)/mod/dep.h" {
			t.Fatalf("copied.cpp inputs unexpectedly contain non-expanded-leaf's include $(S)/mod/dep.h: %v", vfsStringsT3(cc.flatInputs()))
		}
	}
	if len(graphDeps(g, cc)) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(graphDeps(g, cc)))
	}
}
