package main

import "testing"

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
		t.Errorf("a.cpp.o leaked peer copy $(B)/b/dep.h: %v", vfsStringsT3(aCC.Inputs))
	}
	if nodeHasInput(bCC, "$(B)/a/dep.h") {
		t.Errorf("b.cpp.o leaked peer copy $(B)/a/dep.h: %v", vfsStringsT3(bCC.Inputs))
	}
	if !nodeHasInput(aCC, "$(B)/a/dep.h") {
		t.Errorf("a.cpp.o missing own copy $(B)/a/dep.h: %v", vfsStringsT3(aCC.Inputs))
	}
	if !nodeHasInput(bCC, "$(B)/b/dep.h") {
		t.Errorf("b.cpp.o missing own copy $(B)/b/dep.h: %v", vfsStringsT3(bCC.Inputs))
	}
}
