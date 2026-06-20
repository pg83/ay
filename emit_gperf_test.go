package main

import (
	"reflect"
	"testing"
)

// TestGen_GperfGeneratesAndCompiles pins the _SRC("gperf") mechanism
// (ymake.core.conf:3277-3278): contrib/tools/gperf runs with $GP_FLAGS, the
// generated -Nin_<basename>_set symbol, the .gperf source as input, stdout
// redirected to <basename>.gperf.cpp (GP/yellow). The generated cpp is then
// compiled and its object archived into the module library. Representatives:
// library/cpp/html/html5/tag.gperf, library/cpp/html/spec/tags.gperf.
func TestGen_GperfGeneratesAndCompiles(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "contrib/tools/gperf", "gperf")

	writeTestModuleFile(files, "gpmod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(tags.gperf)
END()
`)
	writeTestModuleFile(files, "gpmod/tags.gperf", `%{
#include "tag.h"
%}
%%
`)
	writeTestModuleFile(files, "gpmod/tag.h", "#pragma once\n")

	g := testGen(newMemFS(files), "gpmod")

	gp := mustNodeByOutput(t, g, "$(B)/gpmod/tags.gperf.cpp")

	if gp.KV.P != pkGP || gp.KV.PC != pcYellow {
		t.Fatalf("gperf node kv = {%s,%s}, want {GP,yellow}", gp.KV.P, gp.KV.PC)
	}

	gperfBin := "$(B)/contrib/tools/gperf/gperf"
	wantCmd := []string{
		gperfBin,
		"-CtTLANSI-C",
		"-Dk*",
		"-c",
		"-Nin_tags_set",
		"$(S)/gpmod/tags.gperf",
	}

	if got := strStrs(gp.Cmds[0].CmdArgs.flat()); !reflect.DeepEqual(got, wantCmd) {
		t.Fatalf("gperf cmd_args mismatch:\n  got:  %#v\n  want: %#v", got, wantCmd)
	}

	if gp.Cmds[0].Stdout.string() != "$(B)/gpmod/tags.gperf.cpp" {
		t.Fatalf("gperf stdout = %q, want $(B)/gpmod/tags.gperf.cpp", gp.Cmds[0].Stdout.string())
	}

	for _, want := range []string{gperfBin, "$(S)/gpmod/tags.gperf", "$(S)/gpmod/tag.h"} {
		if !nodeHasInput(gp, want) {
			t.Fatalf("gperf node inputs missing %q: %#v", want, gp.flatInputs())
		}
	}

	ldNode := mustNodeByOutput(t, g, gperfBin)
	if fd := graphForeignDeps(g, gp); len(fd) != 1 || fd[0] != ldNode.UID {
		t.Fatalf("gperf ForeignDeps = %v, want {tool: [%q]}", fd, ldNode.UID)
	}

	// The generated cpp compiles and depends on the GP producer.
	cc := mustNodeByOutput(t, g, "$(B)/gpmod/tags.gperf.cpp.o")
	if !nodeHasInput(cc, "$(B)/gpmod/tags.gperf.cpp") {
		t.Fatalf("gperf CC inputs missing the generated cpp: %#v", cc.flatInputs())
	}

	found := false
	for _, dep := range graphDeps(g, cc) {
		if dep == gp.UID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("gperf CC deps = %v, want to contain GP UID %q", graphDeps(g, cc), gp.UID)
	}

	// The generated object is archived into the module library.
	ar := mustNodeByOutputSuffix(t, g, ".a")
	if !nodeHasInput(ar, "$(B)/gpmod/tags.gperf.cpp.o") {
		t.Fatalf("module archive missing the gperf object: %#v", ar.flatInputs())
	}
}

// TestGen_OrdinaryCppUnchangedByGperf is the negative guard: an ordinary .cpp
// source produces no GP producer node and its CC object is archived directly,
// untouched by the gperf path.
func TestGen_OrdinaryCppUnchangedByGperf(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "plain/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(a.cpp)
END()
`)
	writeTestModuleFile(files, "plain/a.cpp", "int a(){return 0;}\n")

	g := testGen(newMemFS(files), "plain")

	for _, n := range g.Graph {
		if n.KV.P == pkGP {
			t.Fatalf("ordinary .cpp module unexpectedly emitted a GP node: %#v", n.Outputs)
		}
	}

	cc := mustNodeByOutput(t, g, "$(B)/plain/a.cpp.o")
	if cc.KV.P != pkCC {
		t.Fatalf("a.cpp.o kv.p = %s, want CC", cc.KV.P)
	}

	ar := mustNodeByOutputSuffix(t, g, ".a")
	if !nodeHasInput(ar, "$(B)/plain/a.cpp.o") {
		t.Fatalf("module archive missing the ordinary object: %#v", ar.flatInputs())
	}
}
