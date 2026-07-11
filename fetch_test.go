package main

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
)

func emitTestCompileGraph(t *testing.T, host, target *Platform) *Graph {
	t.Helper()

	execEmit := newStreamingEmitter(nil)

	execEmit.fetchRefs.put(internStr(resourcePatternClangTool), execEmit.emitNode(Node{
		Platform: host,
		Cmds:     []Cmd{{CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{"ay", "fetch", "$(B)", "$(S)", "sbr:clang", "resources/CLANG"}))}}},
		KV:       &KV{P: pkFETCH, PC: pcYellow, ShowOut: true},
		Outputs:  []VFS{build("resources/" + resourcePatternClangTool)},
	}))
	clangTool := prebuiltToolchainFlags()["CLANG_TOOL"]
	ref := execEmit.emitNode(Node{Platform: target,
		Cmds: []Cmd{{
			CmdArgs: ArgChunks{ToAnySlice(appendInternStrs(nil, []string{clangTool, "-c", "$(S)/pkg/app/main.cpp", "-o", "$(B)/pkg/app/main.o"}))},
			Env:     nil,
		}},
		Env:          nil,
		Inputs:       InputChunks{{source("pkg/app/main.cpp")}},
		KV:           &KV{P: pkCC},
		Outputs:      []VFS{build("pkg/app/main.o")},
		Requirements: &emptyRequirements,
		Resources:    []STR{internStr(resourcePatternClangTool)},
	})
	execEmit.result(ref)

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

func TestPlaceSandboxResource_RenameResource(t *testing.T) {
	t.Chdir(t.TempDir())

	fetched := filepath.Join(t.TempDir(), "resource")

	if err := os.WriteFile(fetched, []byte("BLACKLIST"), 0o644); err != nil {
		t.Fatal(err)
	}

	placeSandboxResource(fetched, ".", "", []string{"RESOURCE"}, []string{"blacklist_default.json"}, false)

	got, err := os.ReadFile("blacklist_default.json")

	if err != nil {
		t.Fatalf("output not produced: %v", err)
	}

	if string(got) != "BLACKLIST" {
		t.Fatalf("output content = %q, want %q", got, "BLACKLIST")
	}
}

func TestPlaceSandboxResource_UntarTo(t *testing.T) {
	t.Chdir(t.TempDir())

	fetched := filepath.Join(t.TempDir(), "resource")
	f, err := os.Create(fetched)

	if err != nil {
		t.Fatal(err)
	}

	tw := tar.NewWriter(f)
	body := []byte("{}")
	throw(tw.WriteHeader(&tar.Header{Name: "icookie_blacklist.json", Mode: 0o644, Size: int64(len(body))}))
	throw2(tw.Write(body))
	throw(tw.Close())
	throw(f.Close())

	placeSandboxResource(fetched, "", ".", nil, []string{"icookie_blacklist.json"}, false)

	if _, err := os.Stat("icookie_blacklist.json"); err != nil {
		t.Fatalf("extracted output missing: %v", err)
	}
}

func TestResourceGraphEmitter_OnlyMaterializesUsedFetchNodes(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}

func TestResourceGraphEmitter_ReusedEmitterEmitsFetchPerEmitter(t *testing.T) {
	host := newTestPlatform(OSLinux, ISAX8664, "yes")
	target := sandboxedX8664TargetPlatform()

	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
	assertSingleUsedClangFetch(t, emitTestCompileGraph(t, host, target))
}
