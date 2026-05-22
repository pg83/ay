package main

import (
	"testing"
)

func TestGenDumpGraphWithMode_SkipsFetchNodesWithoutUIDDrift(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	resources := &resourceFetchPlan{
		items: []resourceFetch{{
			Pattern: "YMAKE_PYTHON3",
			URI:     "sbr:dummy-ymake-python3",
			Output:  Build("resources/YMAKE_PYTHON3"),
		}},
	}

	execEmit := NewBufferedEmitter()
	execResourceEmit := resourceGraphEmitter(host, execEmit, resources, true)
	execRef := execResourceEmit.Emit(bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: []string{"$(YMAKE_PYTHON3)/bin/python3", "$(S)/pkg/app/main.py"},
			Env:     map[string]string{},
		}},
		Env:              map[string]string{},
		Inputs:           []VFS{Source("pkg/app/main.py")},
		KV:               map[string]interface{}{"p": "CC"},
		Outputs:          []VFS{Build("pkg/app/main.o")},
		Requirements:     map[string]interface{}{},
		TargetProperties: map[string]string{},
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
	if execCC == nil || len(execCC.Deps) != 1 {
		t.Fatalf("execution graph CC deps = %v, want single FETCH dep", execCC.Deps)
	}

	dumpEmit := NewBufferedEmitter()
	dumpResourceEmit := resourceGraphEmitter(host, dumpEmit, resources, false)
	dumpRef := dumpResourceEmit.Emit(bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: []string{"$(YMAKE_PYTHON3)/bin/python3", "$(S)/pkg/app/main.py"},
			Env:     map[string]string{},
		}},
		Env:              map[string]string{},
		Inputs:           []VFS{Source("pkg/app/main.py")},
		KV:               map[string]interface{}{"p": "CC"},
		Outputs:          []VFS{Build("pkg/app/main.o")},
		Requirements:     map[string]interface{}{},
		TargetProperties: map[string]string{},
	}, p))
	dumpResourceEmit.Result(dumpRef)
	dumpGraph := Finalize(dumpEmit)
	byUID := make(map[string]*Node, len(dumpGraph.Graph))
	for _, node := range dumpGraph.Graph {
		if len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/resources/YMAKE_PYTHON3" {
			t.Fatalf("dump graph unexpectedly contains FETCH node: %+v", node)
		}
		byUID[node.UID] = node
	}
	if len(dumpGraph.Graph) != 1 {
		t.Fatalf("dump graph len = %d, want 1 CC node", len(dumpGraph.Graph))
	}

	var uidScratch canonBuf
	for _, node := range dumpGraph.Graph {
		if got, want := nodeUIDWithBuf(node, &uidScratch), node.UID; got != want {
			t.Fatalf("node UID drift after dump generation for %v:\n got: %s\nwant: %s", vfsStrings(node.Outputs), got, want)
		}
		if node.SelfUID != node.UID {
			t.Fatalf("node SelfUID mismatch for %v:\n got: %s\nwant: %s", vfsStrings(node.Outputs), node.SelfUID, node.UID)
		}
		for _, dep := range node.Deps {
			if _, ok := byUID[dep]; !ok {
				t.Fatalf("node %v has unknown dep %q in dump graph", vfsStrings(node.Outputs), dep)
			}
		}
	}
}
