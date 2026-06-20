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

	writeTestModuleFile(files, "contrib/libs/libm/ya.make", "LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(e_exp.c)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/libm/e_exp.c", "double e_exp(double x){return x;}\n")

	return files
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
	// A module under contrib/libs/libm that enables the flag must not peer itself.
	mi := ModuleInstance{
		Path:     source("contrib/libs/libm"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := defaultPeerdirsForWithState(nil, mi, &ModuleData{useArcadiaLibm: true})
	for _, p := range got {
		if p == "contrib/libs/libm" {
			t.Fatalf("contrib/libs/libm must not peer itself; got %v", got)
		}
	}
}
