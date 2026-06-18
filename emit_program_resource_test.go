package main

import (
	"testing"
)

// TestEmitProgramResource_CppProgramLinksObjcopy covers the C++ PROGRAM resource
// link path (the ads/argus/proto/profile/generated/codegen class): a plain
// PROGRAM() that declares BUNDLE(dep NAME x.bundle) + RESOURCE(x.bundle dep/key)
// must emit the resource objcopy and link it as an LD member, with the objcopy
// embedding the BN build output ($(B)/prog/x.bundle), not a $(S) source
// placeholder. Upstream packs RESOURCE batches into objcopy_<hash>.o for the
// link side of every module, not only Python ones.
func TestEmitProgramResource_CppProgramLinksObjcopy(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "dep/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "dep/d.cpp", "int d(){return 0;}\n")

	writeTestModuleFile(files, "prog/ya.make", "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nBUNDLE(dep NAME x.bundle)\nRESOURCE(x.bundle dep/key)\nEND()\n")
	writeTestModuleFile(files, "prog/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "prog")

	depAR := mustNodeByOutput(t, g, "$(B)/dep/libdep.a")
	bn := mustNodeByOutput(t, g, "$(B)/prog/x.bundle")

	// (1) BN node renames the bundled module's primary output.
	if bn.KV.P != pkBN {
		t.Errorf("bundle node kv.p = %q, want BN", bn.KV.P.string())
	}
	if !nodeHasInput(bn, "$(B)/dep/libdep.a") {
		t.Errorf("BN node inputs missing $(B)/dep/libdep.a: %v", vfsStringsT3(bn.flatInputs()))
	}
	if !depsContain(graphDeps(g, bn), depAR.UID) {
		t.Errorf("graphDeps(BN) %v does not include the bundled AR uid %q", graphDeps(g, bn), depAR.UID)
	}

	// (2) the resource objcopy exists, embeds the BN build output, and depends on it.
	oc := findNodeByOutputPrefix(g, "$(B)/prog/objcopy_")
	if oc == nil {
		t.Fatal("graph is missing the prog resource objcopy node (C++ PROGRAM resource not linked)")
	}
	if !nodeHasInput(oc, "$(B)/prog/x.bundle") {
		t.Errorf("objcopy inputs missing the BN build output $(B)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if nodeHasInput(oc, "$(S)/prog/x.bundle") {
		t.Errorf("objcopy lists the nonexistent source $(S)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if !depsContain(graphDeps(g, oc), bn.UID) {
		t.Errorf("graphDeps(objcopy) %v does not include the BN uid %q", graphDeps(g, oc), bn.UID)
	}

	// (3) the PROGRAM LD node links the objcopy object and depends on it.
	ld := mustNodeByOutput(t, g, "$(B)/prog/prog")
	if !nodeHasInput(ld, oc.Outputs[0].string()) {
		t.Errorf("LD inputs missing the resource objcopy member %q: %v", oc.Outputs[0].string(), vfsStringsT3(ld.flatInputs()))
	}
	if !depsContain(graphDeps(g, ld), oc.UID) {
		t.Errorf("graphDeps(LD) %v does not include the objcopy uid %q", graphDeps(g, ld), oc.UID)
	}
}

func depsContain(deps []UID, want UID) bool {
	for _, d := range deps {
		if d == want {
			return true
		}
	}

	return false
}
