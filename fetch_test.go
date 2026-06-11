package main

import (
	"testing"
)

func emitTestCompileGraph(t *testing.T, host, target *Platform) *Graph {
	t.Helper()

	execEmit := newBufferedEmitter()
	// CLANG is declared by build/platform/clang; its FETCH node is emitted up front
	// (as genResourcesLibrary/emitResourceFetch would) into the shared fetchRefs the
	// emitter consults for consumers' $(CLANG) deps.
	fetchRefs := map[string]NodeRef{}
	execResourceEmit := newResourceAwareEmitter(host, execEmit, nil, fetchRefs)
	fetchRefs[resourcePatternClangTool] = execResourceEmit.emit(&Node{
		Platform: host,
		Cmds:     []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"ay", "fetch", "$(B)", "$(S)", "sbr:clang", "resources/CLANG"})}}},
		KV:       KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:  []VFS{build("resources/" + resourcePatternClangTool)},
	})
	clangTool := prebuiltToolchainFlags()["CLANG_TOOL"]
	ref := execResourceEmit.emit(&Node{Platform: target,
		Cmds: []Cmd{{
			CmdArgs: ArgChunks{appendInternStrs(nil, []string{clangTool, "-c", "$(S)/pkg/app/main.cpp", "-o", "$(B)/pkg/app/main.o"})},
			Env:     nil,
		}},
		Env:              nil,
		Inputs:           InputChunks{{intern("$(S)/pkg/app/main.cpp")}},
		KV:               KV{P: pkCC},
		Outputs:          []VFS{intern("$(B)/pkg/app/main.o")},
		Requirements:     Requirements{},
		TargetProperties: TargetProperties{},
		usesResources:    []string{resourcePatternClangTool},
	})
	execResourceEmit.result(ref)

	return finalize(execEmit)
}

func assertSingleUsedClangFetch(t *testing.T, graph *Graph) {
	t.Helper()

	fetchOutputs := map[string]bool{}
	var cc *Node
	for _, node := range graph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].string() == "$(B)/pkg/app/main.o" {
			cc = node
			continue
		}
		if len(node.Outputs) == 1 {
			fetchOutputs[node.Outputs[0].string()] = true
		}
	}

	if cc == nil {
		t.Fatal("execution graph missing expected CC node")
	}
	if len(graphDeps(graph, cc)) != 1 {
		t.Fatalf("execution graph CC deps = %v, want single used-resource FETCH dep", graphDeps(graph, cc))
	}
	if len(fetchOutputs) != 1 {
		t.Fatalf("execution graph fetch outputs = %#v, want only the used CLANG fetch node", fetchOutputs)
	}
	if !fetchOutputs["$(B)/resources/"+resourcePatternClangTool] {
		t.Fatalf("execution graph missing used CLANG fetch node: %#v", fetchOutputs)
	}
	if fetchOutputs["$(B)/resources/"+resourcePatternClang18] {
		t.Fatalf("execution graph unexpectedly contains unused CLANG18 fetch node: %#v", fetchOutputs)
	}
	if fetchOutputs["$(B)/resources/"+resourcePatternClang20] {
		t.Fatalf("execution graph unexpectedly contains unused CLANG20 fetch node: %#v", fetchOutputs)
	}
}

func TestResourceGraphEmitter_OnlyMaterializesUsedFetchNodes(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}

func TestResourceGraphEmitter_ReusedEmitterEmitsFetchPerEmitter(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}
