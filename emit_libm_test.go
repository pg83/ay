package main

import "testing"

// USE_ARCADIA_LIBM (_BASE_UNIT, ymake.core.conf:933-945): ENABLE(USE_ARCADIA_LIBM)
// adds the implicit PEERDIR contrib/libs/libm on non-Emscripten targets. Default is
// "no" (system -lm), so only the explicit ENABLE reaches the peer.
func libmProgramFiles(enable bool) map[string]string {
	files := map[string]string{}

	enableStmt := ""
	if enable {
		enableStmt = "ENABLE(USE_ARCADIA_LIBM)\n"
	}

	writeTestModuleFile(files, "app/ya.make", "PROGRAM(app)\n"+enableStmt+"SRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	// Faithful to contrib/libs/libm/ya.make: the module exports its own GLOBAL
	// ADDINCL(include|platform). T-62 asserts those own-addincl roots land after
	// the language-default transitive closure, not ahead of it.
	writeTestModuleFile(files, "contrib/libs/libm/ya.make",
		"LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nADDINCL(GLOBAL contrib/libs/libm/include\nGLOBAL contrib/libs/libm/platform)\nSRCS(e_exp.c)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/libm/e_exp.c", "double e_exp(double x){return x;}\n")
	// filterExistingSourceDirs (modules.go) drops GLOBAL addincl dirs that do not
	// exist; materialise the two libm include roots so they survive.
	writeTestModuleFile(files, "contrib/libs/libm/include/math.h", "#pragma once\n")
	writeTestModuleFile(files, "contrib/libs/libm/platform/platform.h", "#pragma once\n")

	return files
}

// libmOrderingProgramFiles adds a `util` language-default peer carrying a
// transitive GLOBAL ADDINCL (via library/early). The program compile must order
// that transitive include ahead of libm's own GLOBAL include — the reference
// places the libm roots after the language/default transitive closure, which is
// the program-default slot, not the language-default own slot.
func libmOrderingProgramFiles() map[string]string {
	files := libmProgramFiles(true)

	// util is a language default for any C++ program (defaults.go). NO_RUNTIME /
	// NO_UTIL keep it from pulling its own language defaults; it only re-exports
	// library/early's GLOBAL addincl transitively.
	writeTestModuleFile(files, "util/ya.make", "LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(library/early)\nEND()\n")
	writeTestModuleFile(files, "util/u.cpp", "int u(){return 0;}\n")

	writeTestModuleFile(files, "library/early/ya.make",
		"LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nADDINCL(GLOBAL library/early/include)\nSRCS(early.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/early/early.cpp", "int early(){return 0;}\n")
	writeTestModuleFile(files, "library/early/include/early.h", "#pragma once\n")

	return files
}

func ccArgsOfSuffix(t *testing.T, g *Graph, suffix string) []STR {
	t.Helper()

	n := mustNodeByOutputSuffix(t, g, suffix)
	if len(n.Cmds) == 0 {
		t.Fatalf("CC node %q has no Cmds", suffix)
	}

	return n.Cmds[0].CmdArgs.flat()
}

func linkArgsOf(t *testing.T, g *Graph) []STR {
	t.Helper()

	for _, n := range g.Graph {
		if n.KV.P != pkLD {
			continue
		}

		for _, c := range n.Cmds {
			flat := c.CmdArgs.flat()
			if indexOfArg(flat, "$(S)/build/scripts/link_exe.py") >= 0 {
				return flat
			}
		}
	}

	t.Fatal("no link_exe.py command found on any LD node")

	return nil
}

func TestGen_UseArcadiaLibm_PeersLibmArchive(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(true)), "app")

	mustNodeByOutput(t, g, "$(B)/contrib/libs/libm/libcontrib-libs-libm.a")

	// In the link group archives appear as build-root-relative paths.
	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	linkArgs := linkArgsOf(t, g)
	if indexOfArg(linkArgs, libmLinkArg) < 0 {
		t.Fatalf("program link closure missing %s; link args = %v", libmLinkArg, linkArgs)
	}
}

func TestGen_UseArcadiaLibm_AbsentWithoutEnable(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(false)), "app")

	if n := nodeByOutput(g, "$(B)/contrib/libs/libm/libcontrib-libs-libm.a"); n != nil {
		t.Fatalf("libm archive must not be reachable without ENABLE(USE_ARCADIA_LIBM)")
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	linkArgs := linkArgsOf(t, g)
	if indexOfArg(linkArgs, libmLinkArg) >= 0 {
		t.Fatalf("link closure must not contain %s without the enable", libmLinkArg)
	}
}

func TestGen_UseArcadiaLibm_NoSelfPeer(t *testing.T) {
	// A link module under contrib/libs/libm that enables the flag must not peer
	// itself. The libm COMMON_LINK_SETTINGS peer lives in the program-default
	// path, so the self/descendant guard is checked there.
	mi := ModuleInstance{
		Path:     source("contrib/libs/libm"),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := defaultProgramPeerdirsForWithState(nil, mi, &ModuleData{useArcadiaLibm: true}, false)
	for _, p := range got {
		if p == "contrib/libs/libm" {
			t.Fatalf("contrib/libs/libm must not peer itself; got %v", got)
		}
	}
}

func TestGen_UseArcadiaLibm_AddInclOrderAfterTransitive(t *testing.T) {
	g := testGen(newMemFS(libmOrderingProgramFiles()), "app")

	args := ccArgsOfSuffix(t, g, "app/main.cpp.o")

	earlyIdx := indexOfArg(args, "-I$(S)/library/early/include")
	libmIdx := indexOfArg(args, "-I$(S)/contrib/libs/libm/include")

	if earlyIdx < 0 {
		t.Fatalf("program compile missing the language-default transitive addincl; args = %v", args)
	}

	if libmIdx < 0 {
		t.Fatalf("program compile missing the libm addincl; args = %v", args)
	}

	if earlyIdx > libmIdx {
		t.Fatalf("libm addincl (%d) must come after the language-default transitive addincl (%d); args = %v", libmIdx, earlyIdx, args)
	}
}

func TestGen_UseArcadiaLibm_NoSystemLm(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(true)), "app")

	linkArgs := linkArgsOf(t, g)
	if indexOfArg(linkArgs, "-lm") >= 0 {
		t.Fatalf("USE_ARCADIA_LIBM=yes link must not emit system -lm; link args = %v", linkArgs)
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	if indexOfArg(linkArgs, libmLinkArg) < 0 {
		t.Fatalf("USE_ARCADIA_LIBM=yes link must contain %s; link args = %v", libmLinkArg, linkArgs)
	}
}

func TestGen_UseArcadiaLibm_KeepsSystemLmWithoutEnable(t *testing.T) {
	g := testGen(newMemFS(libmProgramFiles(false)), "app")

	linkArgs := linkArgsOf(t, g)
	if indexOfArg(linkArgs, "-lm") < 0 {
		t.Fatalf("default USE_ARCADIA_LIBM=no link must keep system -lm; link args = %v", linkArgs)
	}

	const libmLinkArg = "contrib/libs/libm/libcontrib-libs-libm.a"
	if indexOfArg(linkArgs, libmLinkArg) >= 0 {
		t.Fatalf("default link must not gain the Arcadia libm archive; link args = %v", linkArgs)
	}
}
