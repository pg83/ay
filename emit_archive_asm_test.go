package main

import "testing"

// testGenX86 builds the graph for targetDir with an x86_64 target (the .rodata
// yasm pipeline is x86_64-only), mirroring testGenContour's opensource contour.
func testGenX86(fs FS, targetDir string) *Graph {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	targetFlags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		targetFlags[k] = v
	}
	targetFlags["PIC"] = "no"
	target := newPlatform(fs, OSLinux, ISAX8664, targetFlags, "", "")
	return Gen(fs, targetDir, host, target, func(Warn) {})
}

// TestEmitArchiveAsm_RunProgramStdoutEqualsOutNoauto reproduces the
// kernel/lemmer/new_dict/rus/extra divergence: a RUN_PROGRAM that names the SAME
// physical file in both STDOUT and OUT_NOAUTO roles (the program's stdout *is*
// the declared output). Upstream's output set is path-keyed, so the file is
// listed exactly once on the producer node; before the fix emitPR appended the
// STDOUT VFS and the OUT_NOAUTO VFS separately, listing the file twice and
// perturbing the node's content hash (cascading a differing Merkle uid into the
// whole ARCHIVE_ASM .rodata chain).
func TestEmitArchiveAsm_RunProgramStdoutEqualsOutNoauto(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "m/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/maker make-all --input lister.txt
    STDOUT out.dict.bin
    IN lister.txt
    OUT_NOAUTO out.dict.bin
)
ARCHIVE_ASM(
    NAME Dict
    DONTCOMPRESS
    ${BINDIR}/out.dict.bin
)
END()
`)
	files["m/lister.txt"] = "word\n"
	writeToolProgram(files, "tools/maker", "maker")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "contrib/tools/yasm", "yasm")

	g := testGenX86(newMemFS(files), "m")

	// (1) the producer lists the generated binary exactly once, despite the file
	// being declared through both STDOUT and OUT_NOAUTO.
	pr := mustNodeByOutput(t, g, "$(B)/m/out.dict.bin")
	if pr.KV.P != pkPR {
		t.Errorf("out.dict.bin kv.p = %q, want PR", pr.KV.P.string())
	}
	n := 0
	for _, o := range pr.Outputs {
		if o.string() == "$(B)/m/out.dict.bin" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("PR node must list $(B)/m/out.dict.bin exactly once, got %d: %v", n, vfsStringsT3(pr.Outputs))
	}

	// (2) the binary still flows once into the ARCHIVE_ASM .rodata resource.
	ar := mustNodeByOutput(t, g, "$(B)/m/Dict.rodata")
	m := 0
	for _, in := range ar.flatInputs() {
		if in.string() == "$(B)/m/out.dict.bin" {
			m++
		}
	}
	if m != 1 {
		t.Fatalf("AR .rodata must list the binary input exactly once, got %d: %v", m, vfsStringsT3(ar.flatInputs()))
	}
}

// TestEmitArchiveAsm_RunPythonOutThroughRodata reproduces the
// kernel/lemmer/new_dict/ara/builtin divergence: a RUN_PYTHON3 OUT_NOAUTO
// consumed by ARCHIVE_ASM must produce the dictionary binary (PY), the
// archive-as-assembly resource (AR <NAME>.rodata), and the rodata→asm→object
// compile (RD), with the RD node carrying the PY node's $(S) source leaves and
// the non-global object archived into the module's .a. Before the fix
// ARCHIVE_ASM is a no-op, so none of these nodes exist.
func TestEmitArchiveAsm_RunPythonOutThroughRodata(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "m/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PYTHON3(
    ${ARCADIA_ROOT}/m/unzip.py in.bin.gz out.dict.bin
    IN in.bin.gz
    OUT_NOAUTO out.dict.bin
)
ARCHIVE_ASM(
    NAME Dict
    DONTCOMPRESS
    ${BINDIR}/out.dict.bin
)
END()
`)
	files["m/unzip.py"] = "print('unzip')\n"
	files["m/in.bin.gz"] = ""
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "contrib/tools/yasm", "yasm")

	g := testGenX86(newMemFS(files), "m")

	// (1) RUN_PYTHON3 OUT_NOAUTO dictionary binary becomes reachable.
	py := mustNodeByOutput(t, g, "$(B)/m/out.dict.bin")
	if py.KV.P != pkPY {
		t.Errorf("out.dict.bin kv.p = %q, want PY", py.KV.P.string())
	}

	// (2) ARCHIVE_ASM emits the AR .rodata resource, kv AR / light-cyan, with
	// the dictionary binary as a `:`-suffixed member and a producer dep on PY.
	ar := mustNodeByOutput(t, g, "$(B)/m/Dict.rodata")
	if ar.KV.P != pkAR {
		t.Errorf("Dict.rodata kv.p = %q, want AR", ar.KV.P.string())
	}
	if ar.KV.PC != pcLightCyan {
		t.Errorf("Dict.rodata kv.pc = %q, want light-cyan", ar.KV.PC.string())
	}
	if !nodeHasInput(ar, "$(B)/m/out.dict.bin") {
		t.Errorf("AR .rodata inputs missing the dictionary binary: %v", vfsStringsT3(ar.flatInputs()))
	}
	if !depsContain(graphDeps(g, ar), py.UID) {
		t.Errorf("graphDeps(AR .rodata) %v does not include the PY uid %q", graphDeps(g, ar), py.UID)
	}
	memberArg := "$(B)/m/out.dict.bin:"
	if indexOfArg(ar.Cmds[0].CmdArgs.flat(), memberArg) < 0 {
		t.Errorf("AR .rodata cmd missing `:`-suffixed member %q: %v", memberArg, strStrs(ar.Cmds[0].CmdArgs.flat()))
	}

	// (3) the rodata compile (RD) produces the object, carries the PY node's
	// $(S) source leaves, and depends on the AR .rodata producer.
	rd := mustNodeByAnyOutput(t, g, "$(B)/m/Dict.rodata.o")
	if rd.KV.P != pkRD {
		t.Errorf("Dict.rodata.o kv.p = %q, want RD", rd.KV.P.string())
	}
	for _, leaf := range []string{"$(S)/m/in.bin.gz", "$(S)/m/unzip.py"} {
		if !nodeHasInput(rd, leaf) {
			t.Errorf("RD node missing propagated $(S) source leaf %q: %v", leaf, vfsStringsT3(rd.flatInputs()))
		}
	}
	if !nodeHasInput(rd, "$(B)/m/Dict.rodata") {
		t.Errorf("RD node inputs missing the .rodata source: %v", vfsStringsT3(rd.flatInputs()))
	}
	if !depsContain(graphDeps(g, rd), ar.UID) {
		t.Errorf("graphDeps(RD) %v does not include the AR .rodata uid %q", graphDeps(g, rd), ar.UID)
	}

	// (4) the non-global rodata object is archived into the module's .a.
	lib := mustNodeByOutput(t, g, "$(B)/m/libm.a")
	if !nodeHasInput(lib, "$(B)/m/Dict.rodata.o") {
		t.Errorf("library .a inputs missing the rodata object: %v", vfsStringsT3(lib.flatInputs()))
	}
}
