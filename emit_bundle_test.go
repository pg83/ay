package main

import (
	"strings"
	"testing"
)

// TestEmitBundle_GeneratedFileWiresProducerDep covers the BUNDLE → RESOURCE path:
// the BN node renames the bundled primary output into $(B)/cons/x.bundle, and a
// RESOURCE embeds that BN output (not an $(S) source) and depends on the BN node.
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

	// (1) BN node identity + rename of the bundled primary output.
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

	// (2) the resource objcopy embeds the BN output and depends on the BN node.
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

// TestEmitProgramResource_BundleAttributesFsToolsToModule covers the BUNDLE
// MOVE_FILE attribution gap: a linkable module declaring BUNDLE has the fs_tools.py
// input on its link node as well as the BN producer, whose shape stays unchanged.
func TestEmitProgramResource_BundleAttributesFsToolsToModule(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "dep/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "dep/d.cpp", "int d(){return 0;}\n")

	// Seed the FS_TOOLS script closure (fs_tools.py -> process_command_files.py).
	files["build/scripts/fs_tools.py"] = "import process_command_files as pcf\n"
	files["build/scripts/process_command_files.py"] = "\n"

	// blib is a BUNDLE-declaring LIBRARY. The MOVE_FILE fs_tools.py is attributed
	// only to an LD node, not a library's .a, so its AR must stay fs_tools-free.
	writeTestModuleFile(files, "blib/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(b.cpp)\nBUNDLE(dep NAME y.bundle)\nRESOURCE(y.bundle blib/key)\nEND()\n")
	writeTestModuleFile(files, "blib/b.cpp", "int b(){return 0;}\n")

	writeTestModuleFile(files, "prog/ya.make", "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nPEERDIR(blib)\nBUNDLE(dep NAME x.bundle)\nRESOURCE(x.bundle dep/key)\nEND()\n")
	writeTestModuleFile(files, "prog/main.cpp", "int main(){return 0;}\n")

	// Internal contour: emitCopy is false for a plain PROGRAM, so fs_tools.py
	// reaches the link node only through the BUNDLE MOVE_FILE under test.
	g := testGenInternal(newMemFS(files), "prog")

	const fsTools = "$(S)/build/scripts/fs_tools.py"

	// (1) the consuming module's LD node carries the BUNDLE MOVE_FILE input.
	ld := mustNodeByOutput(t, g, "$(B)/prog/prog")
	if !nodeHasInput(ld, fsTools) {
		t.Errorf("LD node inputs missing the BUNDLE MOVE_FILE input %q: %v", fsTools, vfsStringsT3(ld.flatInputs()))
	}

	// (2) the BN producer shape is unchanged: still carries fs_tools.py and
	// depends on the bundled archive.
	depAR := mustNodeByOutput(t, g, "$(B)/dep/libdep.a")
	bn := mustNodeByOutput(t, g, "$(B)/prog/x.bundle")
	if !nodeHasInput(bn, fsTools) {
		t.Errorf("BN node inputs missing %q: %v", fsTools, vfsStringsT3(bn.flatInputs()))
	}
	if !depsContain(graphDeps(g, bn), depAR.UID) {
		t.Errorf("graphDeps(BN) %v does not include the bundled AR uid %q", graphDeps(g, bn), depAR.UID)
	}

	// (3) the objcopy/BN chain stays paired.
	oc := findNodeByOutputPrefix(g, "$(B)/prog/objcopy_")
	if oc == nil {
		t.Fatal("graph is missing the prog resource objcopy node")
	}
	if !nodeHasInput(oc, "$(B)/prog/x.bundle") {
		t.Errorf("objcopy inputs missing the BN build output $(B)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if !depsContain(graphDeps(g, ld), oc.UID) {
		t.Errorf("graphDeps(LD) %v does not include the objcopy uid %q", graphDeps(g, ld), oc.UID)
	}

	// (4) asymmetry: a BUNDLE-declaring LIBRARY's archive node must NOT carry the
	// MOVE_FILE fs_tools.py input (attributed to LD only).
	blibAR := mustNodeByOutput(t, g, "$(B)/blib/libblib.a")
	if nodeHasInput(blibAR, fsTools) {
		t.Errorf("LIBRARY AR node must not carry the BUNDLE MOVE_FILE input %q: %v", fsTools, vfsStringsT3(blibAR.flatInputs()))
	}
}
