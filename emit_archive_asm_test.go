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
