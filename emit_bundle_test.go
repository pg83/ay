package main

import (
	"strings"
	"testing"
)

// TestEmitBundle_GeneratedFileWiresProducerDep covers the BUNDLE → RESOURCE
// consumer path: BUNDLE(<lib> NAME x.bundle) emits a BN node that renames the
// bundled module's primary output ($(B)/dep/libdep.a) into $(B)/cons/x.bundle,
// and a RESOURCE(x.bundle …) in the same module embeds the BN build output
// (not a nonexistent $(S)/cons/x.bundle source) and depends on the BN node.
func TestEmitBundle_GeneratedFileWiresProducerDep(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "dep/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "dep/d.cpp", "int d(){return 0;}\n")

	writeTestModuleFile(files, "cons/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(c.cpp)\nBUNDLE(dep NAME x.bundle)\nRESOURCE(x.bundle key)\nEND()\n")
	writeTestModuleFile(files, "cons/c.cpp", "int c(){return 0;}\n")

	g := testGen(newMemFS(files), "cons")

	depAR := mustNodeByOutput(t, g, "$(B)/dep/libdep.a")
	bn := mustNodeByOutput(t, g, "$(B)/cons/x.bundle")

	// (1) BN node identity + faithful rename of the bundled module's primary output.
	if bn.KV.P != pkBN {
		t.Errorf("bundle node kv.p = %q, want BN", bn.KV.P.string())
	}
	if !nodeHasInput(bn, "$(B)/dep/libdep.a") {
		t.Errorf("BN node inputs missing the bundled primary output $(B)/dep/libdep.a: %v", vfsStringsT3(bn.flatInputs()))
	}
	cmd := strings.Join(strStrs(bn.Cmds[0].CmdArgs.flat()), " ")
	if !strings.Contains(cmd, "fs_tools.py rename $(B)/dep/libdep.a $(B)/cons/x.bundle") {
		t.Errorf("BN cmd = %q, want a fs_tools.py rename of the bundled output into the destination", cmd)
	}
	bnDepsAR := false
	for _, d := range graphDeps(g, bn) {
		if d == depAR.UID {
			bnDepsAR = true
			break
		}
	}
	if !bnDepsAR {
		t.Errorf("graphDeps(g, BN) %v does not include the bundled AR uid %q", graphDeps(g, bn), depAR.UID)
	}

	// (2) the resource objcopy embeds the BN build output and depends on the BN node.
	oc := findNodeByOutputPrefix(g, "$(B)/cons/objcopy_")
	if oc == nil {
		t.Fatal("graph is missing the cons objcopy node")
	}
	if !nodeHasInput(oc, "$(B)/cons/x.bundle") {
		t.Errorf("objcopy inputs missing the BN build output $(B)/cons/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if nodeHasInput(oc, "$(S)/cons/x.bundle") {
		t.Errorf("objcopy still lists the nonexistent source $(S)/cons/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	ocDepsBN := false
	for _, d := range graphDeps(g, oc) {
		if d == bn.UID {
			ocDepsBN = true
			break
		}
	}
	if !ocDepsBN {
		t.Errorf("graphDeps(g, objcopy) %v does not include the BN uid %q", graphDeps(g, oc), bn.UID)
	}
}
