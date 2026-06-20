package main

import (
	"slices"
	"testing"
)

// TestGen_RunProgramBuiltToolDepIdentityTracksClosure is the T-36 regression.
//
// Upstream: a RUN_PROGRAM-produced header that runs a built PROGRAM tool records
// the tool as a dependency carrying that tool's node identity — the tool's LD
// uid, a Merkle hash over the tool's own source/header link closure — so the
// producer re-fires when the tool's binary identity changes. In ay this is
// emitRunProgram -> ctx.toolResult(toolPath) -> LDRef, placed on the producer as
// ForeignDepRefs (emit_pr.go). A plain source-file argument to the tool
// invocation stays a plain $(S) input (runProgramInputVFS), never promoted to a
// built-tool dep.
//
// This guards the property the yabs/plutonium/libs/profiles/*_profile_gen tools
// rely on for their *.yaff.h producers: the producer's tool dep IS the built
// tool's linked identity, so the tool's source/header closure (not just its
// path) feeds the producer's uid — while a source argument is unaffected.
func TestGen_RunProgramBuiltToolDepIdentityTracksClosure(t *testing.T) {
	build := func(toolHeader string) *Graph {
		files := map[string]string{}

		// A built PROGRAM tool whose binary identity depends on its own header
		// closure: main.cpp #includes tool.h, so tool.h's content reaches the
		// tool's CC node and hence the tool's LD uid.
		writeTestModuleFile(files, "tools/genhdr/ya.make", `PROGRAM(genhdr)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
		writeTestModuleFile(files, "tools/genhdr/main.cpp", "#include \"tool.h\"\nint main(){return 0;}\n")
		writeTestModuleFile(files, "tools/genhdr/tool.h", toolHeader)

		// A LIBRARY whose RUN_PROGRAM runs the built tool with a source-file
		// argument (template.h.in, declared IN) and produces gen.h.
		writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr
        template.h.in
        gen.h
    IN
        template.h.in
    OUT
        gen.h
)
END()
`)
		writeTestModuleFile(files, "gen/template.h.in", "#pragma once\n")

		writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(main.cpp)
END()
`)
		writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

		return testGen(newMemFS(files), "app")
	}

	const toolLDOut = "$(B)/tools/genhdr/genhdr"
	const producerOut = "$(B)/gen/gen.h"
	const sourceArgInput = "$(S)/gen/template.h.in"

	g1 := build("#pragma once\n")
	toolLD1 := mustNodeByOutput(t, g1, toolLDOut)
	prod1 := mustNodeByOutput(t, g1, producerOut)

	if toolLD1.KV.P != pkLD {
		t.Fatalf("tool node %q kv.p = %v, want LD", toolLDOut, toolLD1.KV.P)
	}

	// (1) The producer's tool dep IS the built tool's LD identity.
	foreign1 := graphForeignDeps(g1, prod1)
	if !slices.Contains(foreign1, toolLD1.UID) {
		t.Fatalf("producer %q foreign (tool) deps %v missing built-tool LD uid %q",
			producerOut, foreign1, toolLD1.UID)
	}

	// (2) The source argument is a plain input, NOT promoted to a tool dep.
	if !nodeHasInput(prod1, sourceArgInput) {
		t.Fatalf("producer %q inputs %v missing source arg %q",
			producerOut, prod1.flatInputs(), sourceArgInput)
	}
	for _, fd := range foreign1 {
		if fd == toolLD1.UID {
			continue
		}
		t.Fatalf("producer %q has unexpected extra foreign dep %q (source arg must not be a tool dep)",
			producerOut, fd)
	}

	// (3) Change the tool's header closure. The tool's binary identity (LD uid)
	// must change, and the producer's tool dep must follow it to the new
	// identity — proving the producer tracks the tool's source/header closure,
	// not merely its path.
	g2 := build("#pragma once\nint genhdr_marker = 1;\n")
	toolLD2 := mustNodeByOutput(t, g2, toolLDOut)
	prod2 := mustNodeByOutput(t, g2, producerOut)

	if toolLD1.UID == toolLD2.UID {
		t.Fatalf("built-tool LD uid unchanged (%q) after tool.h closure change — tool identity does not track its source/header closure",
			toolLD1.UID)
	}

	foreign2 := graphForeignDeps(g2, prod2)
	if slices.Contains(foreign2, toolLD1.UID) {
		t.Fatalf("producer %q still depends on stale tool identity %q after the tool closure changed",
			producerOut, toolLD1.UID)
	}
	if !slices.Contains(foreign2, toolLD2.UID) {
		t.Fatalf("producer %q foreign deps %v missing new built-tool LD uid %q",
			producerOut, foreign2, toolLD2.UID)
	}

	// (4) The source-only tool argument's modeling stays unchanged across the
	// tool-closure change: still the same plain $(S) input.
	if !nodeHasInput(prod2, sourceArgInput) {
		t.Fatalf("producer %q lost source arg input %q after tool closure change",
			producerOut, sourceArgInput)
	}
}
