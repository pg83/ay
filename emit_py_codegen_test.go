package main

import "testing"

func TestEmitPyRegister_ProducerEmittedAtTargetPlatform(t *testing.T) {
	emit := newBufferedEmitter()
	ctx := &GenCtx{
		emit:   emit,
		na:     emit.nodeArenas(),
		host:   testHostP,
		target: testTargetP,
	}
	d := &ModuleData{pyRegister: STRS("_sqlite3")}
	hostInst := ModuleInstance{
		Path:     source("contrib/tools/python3/Modules/_sqlite"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
	targetInst := hostInst
	targetInst.Platform = testTargetP

	emitPyRegister(ctx, hostInst, d, ModuleCCInputs{}, false)
	emitPyRegister(ctx, targetInst, d, ModuleCCInputs{}, false)

	// No cross-platform cache: each instance emits its own .reg3.cpp producer.
	// gen_py3_reg.py is platform-independent codegen, attributed to the target
	// platform, so both producers carry testTargetP and no tool tag — byte-
	// identical, hence they collapse by uid in the finalized graph.
	wantOutput := "$(B)/contrib/tools/python3/Modules/_sqlite/_sqlite3.reg3.cpp"
	var pyNodes []*Node

	for _, n := range emit.nodes {
		if len(n.Outputs) == 1 && n.Outputs[0].string() == wantOutput {
			pyNodes = append(pyNodes, n)
		}
	}

	if len(pyNodes) != 2 {
		t.Fatalf("emitted %d PY producers, want 2 (one per instance)", len(pyNodes))
	}

	for _, n := range pyNodes {
		if string(n.Platform.Target) != string(testTargetP.Target) {
			t.Errorf("PY node platform = %q, want %q (target)", n.Platform.Target, testTargetP.Target)
		}
	}
}

// lxml-like fixture: CYTHONIZE_PY appears BEFORE any CYTHON_C/CYTHON_CPP
// directive, so its `.py` source falls into the default C++ bucket (upstream
// `pyxs = pyxs_cpp`). Textual order is _difflib(cpp), objectify(C),
// etree(C_API_H), but the fixed bucket order is CYTHON_C, CYTHON_C_API_H,
// CYTHON_CPP — so the regular archive lists objectify, etree, _difflib, and the
// global archive's .reg3.cpp members follow the same order.
func TestGen_CythonizePyDefaultCppBucketARMemberOrder(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nPY_SRCS(TOP_LEVEL CYTHONIZE_PY _difflib.py CYTHON_C objectify.pyx CYTHON_C_API_H etree.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/_difflib.py", "def d():\n    return 0\n")
	writeTestModuleFile(files, "pkg/objectify.pyx", "def o():\n    return 1\n")
	writeTestModuleFile(files, "pkg/etree.pyx", "def e():\n    return 2\n")

	g := testGen(newMemFS(files), "pkg")

	regular := mustNodeByOutput(t, g, "$(B)/pkg/libpy3pkg.a")
	objectify := arMemberIndex(t, regular, "pkg", "objectify.pyx.c.o")
	etree := arMemberIndex(t, regular, "pkg", "etree.c.o")
	difflib := arMemberIndex(t, regular, "pkg", "_difflib.py.cpp.o")

	if !(objectify < etree && etree < difflib) {
		t.Fatalf("regular archive order objectify(%d) < etree(%d) < _difflib(%d) violated: %v",
			objectify, etree, difflib, vfsStrings(regular.flatInputs()))
	}

	global := mustNodeByOutput(t, g, "$(B)/pkg/libpy3pkg.global.a")
	objectifyR := arMemberIndex(t, global, "pkg", "objectify.reg3.cpp.o")
	etreeR := arMemberIndex(t, global, "pkg", "etree.reg3.cpp.o")
	difflibR := arMemberIndex(t, global, "pkg", "_difflib.reg3.cpp.o")

	if !(objectifyR < etreeR && etreeR < difflibR) {
		t.Fatalf("global .reg3.cpp order objectify(%d) < etree(%d) < _difflib(%d) violated: %v",
			objectifyR, etreeR, difflibR, vfsStrings(global.flatInputs()))
	}
}

// A generated PY_SRCS source (PY_SRCS(__init__.py) where __init__.py is the
// OUT_NOAUTO output of a RUN_PROGRAM) must reproduce upstream pybuild.py's
// `rootrel_arc_src(path, unit) + '-'` py3cc source-name argument. For a build-
// generated source, rootrel_arc_src resolves into $B (not $S) and falls through
// to `return src`, so the argument is the raw PY_SRCS token (`__init__.py-`),
// not the module-rooted path (`mod/__init__.py-`). The bytecode node also
// inherits the producer's transitive $(S) source closure (upstream flat-input
// model) — the direct IN leaf AND its transitive includes — not just the direct
// leaf, and depends on the RUN_PROGRAM producer node.
func TestGen_GeneratedPySrcsBytecodeNamingAndProducerClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "mod/gen/bin", "gen")

	writeTestModuleFile(files, "other/other.h", "#pragma once\n")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(__init__.py)
RUN_PROGRAM(
    mod/gen/bin
        --save_file_path __init__.py
    IN_NOPARSE gen.h
    OUT_NOAUTO __init__.py
)
END()
`)
	writeTestModuleFile(files, "mod/gen.h", "#pragma once\n#include <other/other.h>\n")

	g := testGen(newMemFS(files), "mod")

	bc := mustNodeByOutput(t, g, "$(B)/mod/__init__.py.yapyc3")
	args := bc.Cmds[0].CmdArgs.flat()

	// (1) Source-name argument is the raw token, not the module-rooted path.
	if indexOfArg(args, "__init__.py-") < 0 {
		t.Fatalf("py3cc cmd missing generated source-name arg %q: %v", "__init__.py-", strStrs(args))
	}
	if indexOfArg(args, "mod/__init__.py-") >= 0 {
		t.Fatalf("py3cc cmd uses module-rooted source name, want raw token: %v", strStrs(args))
	}

	// (2) Bytecode node depends on the RUN_PROGRAM producer of the generated source.
	producer := mustNodeByOutput(t, g, "$(B)/mod/__init__.py")
	foundDep := false
	for _, d := range graphDeps(g, bc) {
		if d == producer.UID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Fatalf("bytecode deps %v do not include producer uid %q", graphDeps(g, bc), producer.UID)
	}

	// (3) Bytecode node carries the producer's transitive $(S) source closure:
	// the direct IN gen.h AND its transitive include other/other.h.
	if !nodeHasInput(bc, "$(S)/mod/gen.h") {
		t.Fatalf("bytecode inputs missing direct generator source gen.h: %#v", bc.flatInputs())
	}
	if !nodeHasInput(bc, "$(S)/other/other.h") {
		t.Fatalf("bytecode inputs missing transitive generator closure other/other.h: %#v", bc.flatInputs())
	}
}
