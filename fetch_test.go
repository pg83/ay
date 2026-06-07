package main

import (
	"testing"
)

func newTestToolchainResourceBundlesFS() FS {
	const body = `{"by_platform":{"linux-x86_64":{"uri":"sbr:linux"}}}`

	files := map[string]string{}
	for _, rel := range []string{
		"build/platform/python/ymake_python3/resources.json",
		"build/platform/clang/clang16.json",
		"build/platform/clang/clang18.json",
		"build/platform/lld/lld20.json",
		"build/platform/clang/clang20.json",
		"build/platform/java/jdk/jdk17/jdk.json",
	} {
		files[rel] = body
	}

	return newMemFS(files)
}

func emitTestCompileGraph(t *testing.T, host, target *Platform, plan *resourceFetchPlan) *Graph {
	t.Helper()

	execEmit := NewBufferedEmitter()
	execResourceEmit := resourceGraphEmitter(host, execEmit, plan, true, nil)
	clangTool := prebuiltToolchainFlags()["CLANG_TOOL"]
	ref := execResourceEmit.Emit(bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: anys(clangTool, "-c", "$(S)/pkg/app/main.cpp", "-o", "$(B)/pkg/app/main.o"),
			Env:     nil,
		}},
		Env:              nil,
		Inputs:           []VFS{Intern("$(S)/pkg/app/main.cpp")},
		KV:               KV{P: pkCC},
		Outputs:          []VFS{Intern("$(B)/pkg/app/main.o")},
		Requirements:     Requirements{},
		TargetProperties: TargetProperties{},
		usesResources:    []string{resourcePatternClangTool},
	}, target))
	execResourceEmit.Result(ref)

	return Finalize(execEmit)
}

func assertSingleUsedClangFetch(t *testing.T, graph *Graph) {
	t.Helper()

	fetchOutputs := map[string]bool{}
	var cc *Node
	for _, node := range graph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/pkg/app/main.o" {
			cc = node
			continue
		}
		if len(node.Outputs) == 1 {
			fetchOutputs[node.Outputs[0].String()] = true
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

func TestGenDumpGraphWithMode_SkipsFetchNodesWithoutUIDDrift(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	resources := &resourceFetchPlan{
		items: []resourceFetch{{
			Pattern: "YMAKE_PYTHON3",
			URI:     "sbr:dummy-ymake-python3",
			Output:  Intern("$(B)/resources/YMAKE_PYTHON3"),
		}},
	}

	execEmit := NewBufferedEmitter()
	execResourceEmit := resourceGraphEmitter(host, execEmit, resources, true, nil)
	execRef := execResourceEmit.Emit(bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: anys("$(YMAKE_PYTHON3)/bin/python3", "$(S)/pkg/app/main.py"),
			Env:     nil,
		}},
		Env:              nil,
		Inputs:           []VFS{Intern("$(S)/pkg/app/main.py")},
		KV:               KV{P: pkCC},
		Outputs:          []VFS{Intern("$(B)/pkg/app/main.o")},
		Requirements:     Requirements{},
		TargetProperties: TargetProperties{},
		usesResources:    []string{resourcePatternYMakePython3},
	}, p))
	execResourceEmit.Result(execRef)
	execGraph := Finalize(execEmit)
	foundFetch := false
	var execCC *Node
	for _, node := range execGraph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/resources/YMAKE_PYTHON3" {
			foundFetch = true
		}
		if len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/pkg/app/main.o" {
			execCC = node
		}
	}
	if !foundFetch {
		t.Fatalf("execution graph missing expected FETCH node")
	}
	if execCC == nil || len(graphDeps(execGraph, execCC)) != 1 {
		t.Fatalf("execution graph CC deps = %v, want single FETCH dep", graphDeps(execGraph, execCC))
	}

	dumpEmit := NewBufferedEmitter()
	dumpResourceEmit := resourceGraphEmitter(host, dumpEmit, resources, false, nil)
	dumpRef := dumpResourceEmit.Emit(bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: anys("$(YMAKE_PYTHON3)/bin/python3", "$(S)/pkg/app/main.py"),
			Env:     nil,
		}},
		Env:              nil,
		Inputs:           []VFS{Intern("$(S)/pkg/app/main.py")},
		KV:               KV{P: pkCC},
		Outputs:          []VFS{Intern("$(B)/pkg/app/main.o")},
		Requirements:     Requirements{},
		TargetProperties: TargetProperties{},
		usesResources:    []string{resourcePatternYMakePython3},
	}, p))
	dumpResourceEmit.Result(dumpRef)
	dumpGraph := Finalize(dumpEmit)
	byUID := make(map[UID]*Node, len(dumpGraph.Graph))
	for _, node := range dumpGraph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/resources/YMAKE_PYTHON3" {
			t.Fatalf("dump graph unexpectedly contains FETCH node: %+v", node)
		}
		byUID[node.UID] = node
	}
	if len(dumpGraph.Graph) != 1 {
		t.Fatalf("dump graph len = %d, want 1 CC node", len(dumpGraph.Graph))
	}

	uidScratch := canonBuf{uids: dumpGraph.uids}
	for _, node := range dumpGraph.Graph {
		if got, want := nodeUIDWithBuf(node, &uidScratch), node.UID; got != want {
			t.Fatalf("node UID drift after dump generation for %v:\n got: %s\nwant: %s", vfsStrings(node.Outputs), got, want)
		}
		if node.SelfUID != node.UID {
			t.Fatalf("node SelfUID mismatch for %v:\n got: %s\nwant: %s", vfsStrings(node.Outputs), node.SelfUID, node.UID)
		}
		for _, dep := range graphDeps(dumpGraph, node) {
			if _, ok := byUID[dep]; !ok {
				t.Fatalf("node %v has unknown dep %q in dump graph", vfsStrings(node.Outputs), dep)
			}
		}
	}
}

func TestResourceGraphEmitter_OnlyMaterializesUsedFetchNodes(t *testing.T) {
	fs := newTestToolchainResourceBundlesFS()

	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := sandboxedX8664TargetPlatform()
	conf := graphConfForToolchainFlags(fs, prebuiltToolchainFlags())
	plan := newResourceFetchPlan(fs.SourceRoot(), conf, host)

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target, plan))
}

func TestResourceGraphEmitter_ReusedPlanEmitsFetchPerEmitter(t *testing.T) {
	fs := newTestToolchainResourceBundlesFS()

	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	target := sandboxedX8664TargetPlatform()
	conf := graphConfForToolchainFlags(fs, prebuiltToolchainFlags())
	plan := newResourceFetchPlan(fs.SourceRoot(), conf, host)

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target, plan))
	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target, plan))
}
