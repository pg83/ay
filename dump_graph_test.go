package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestFinalizeDumpGraph_StripsOnlyTicketScaffolding(t *testing.T) {
	emit := NewBufferedEmitter()

	fetchUsed := emit.Emit(&Node{
		KV:      map[string]interface{}{"p": "FETCH"},
		Outputs: []VFS{Build("resources/YMAKE_PYTHON3")},
	})
	emit.Emit(&Node{
		KV:      map[string]interface{}{"p": "FETCH"},
		Outputs: []VFS{Build("resources/CLANG")},
	})
	emit.Emit(&Node{
		KV:      map[string]interface{}{"p": "FETCH"},
		Outputs: []VFS{Build("tool-cache/CLANG")},
	})
	emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build("contrib/libs/llvm16/include/llvm/IR/Attributes.inc")},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	llvmReferenced := emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build("contrib/libs/llvm16/include/llvm/IR/IntrinsicsX86.h")},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build("contrib/libs/llvm16/include/generated.cpp")},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build("other/module/generated.inc")},
		TargetProperties: map[string]string{"module_dir": "other/module"},
	})
	consumer := emit.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"clang"}}},
		DepRefs: []NodeRef{fetchUsed, llvmReferenced},
		ForeignDepRefs: map[string][]NodeRef{
			"tool": {fetchUsed},
		},
		KV:      map[string]interface{}{"p": "CC"},
		Outputs: []VFS{Build("obj/consumer.o")},
	})
	root := emit.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"ld"}}},
		DepRefs: []NodeRef{consumer},
		KV:      map[string]interface{}{"p": "LD"},
		Outputs: []VFS{Build("bin/root")},
	})
	emit.Result(root)

	got := finalizeDumpGraph(emit)

	if want := []string{
		"bin/root",
		"obj/consumer.o",
		"contrib/libs/llvm16/include/llvm/IR/IntrinsicsX86.h",
		"tool-cache/CLANG",
		"contrib/libs/llvm16/include/generated.cpp",
		"other/module/generated.inc",
	}; !reflect.DeepEqual(graphPrimaryOutputs(got.Graph), want) {
		t.Fatalf("dump graph outputs = %v, want %v", graphPrimaryOutputs(got.Graph), want)
	}

	consumerNode := findGraphNodeByOutput(got.Graph, "obj/consumer.o")
	if consumerNode == nil {
		t.Fatal("consumer node missing after finalizeDumpGraph")
	}

	llvmNode := findGraphNodeByOutput(got.Graph, "contrib/libs/llvm16/include/llvm/IR/IntrinsicsX86.h")
	if llvmNode == nil {
		t.Fatal("llvm referenced node missing after finalizeDumpGraph")
	}

	if !reflect.DeepEqual(consumerNode.Deps, []string{llvmNode.UID}) {
		t.Fatalf("consumer deps = %v, want [%s]", consumerNode.Deps, llvmNode.UID)
	}
	if consumerNode.ForeignDeps != nil {
		t.Fatalf("consumer foreign_deps = %v, want nil after fetch trim", consumerNode.ForeignDeps)
	}

	assertUIDMatchesNode(t, consumerNode)
	assertUIDMatchesNode(t, findGraphNodeByOutput(got.Graph, "bin/root"))
}

func TestFinalizeDumpGraph_KeepsMatchingResultNode(t *testing.T) {
	emit := NewBufferedEmitter()
	expected := "contrib/libs/llvm16/include/llvm/IR/Attributes.inc"

	root := emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build(expected)},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	emit.Result(root)

	got := finalizeDumpGraph(emit)

	if want := []string{expected}; !reflect.DeepEqual(graphPrimaryOutputs(got.Graph), want) {
		t.Fatalf("dump graph outputs = %v, want %v", graphPrimaryOutputs(got.Graph), want)
	}
	assertUIDMatchesNode(t, findGraphNodeByOutput(got.Graph, expected))
}

func TestFinalizeDumpGraph_PrunesTransitiveStandaloneLLVM(t *testing.T) {
	emit := NewBufferedEmitter()

	leaf := "contrib/libs/llvm16/include/llvm/IR/Leaf.inc"
	parent := "contrib/libs/llvm16/include/llvm/IR/Parent.inc"

	leafRef := emit.Emit(&Node{
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build(leaf)},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	emit.Emit(&Node{
		DepRefs:          []NodeRef{leafRef},
		KV:               map[string]interface{}{"p": "PR"},
		Outputs:          []VFS{Build(parent)},
		TargetProperties: map[string]string{"module_dir": "contrib/libs/llvm16/include"},
	})
	root := emit.Emit(&Node{
		KV:      map[string]interface{}{"p": "LD"},
		Outputs: []VFS{Build("bin/root")},
	})
	emit.Result(root)

	got := finalizeDumpGraph(emit)

	if want := []string{"bin/root"}; !reflect.DeepEqual(graphPrimaryOutputs(got.Graph), want) {
		t.Fatalf("dump graph outputs = %v, want %v", graphPrimaryOutputs(got.Graph), want)
	}
	if node := findGraphNodeByOutput(got.Graph, leaf); node != nil {
		t.Fatalf("transitively standalone llvm leaf survived prune: %+v", node)
	}
	if node := findGraphNodeByOutput(got.Graph, parent); node != nil {
		t.Fatalf("standalone llvm parent survived prune: %+v", node)
	}
}

func TestFinalizeDumpGraph_PreservesFinalizeValidation(t *testing.T) {
	tests := []struct {
		name   string
		needle string
		build  func() *BufferedEmitter
	}{
		{
			name:   "BogusDepRef",
			needle: "out-of-range NodeRef",
			build: func() *BufferedEmitter {
				emit := NewBufferedEmitter()
				emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "FETCH"},
					Outputs: []VFS{Build("resources/CLANG")},
				})
				leaf := emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "PR"},
					Outputs: []VFS{Build("obj/leaf.o")},
				})
				root := emit.Emit(&Node{
					DepRefs: []NodeRef{leaf, NodeRef{id: 99}},
					KV:      map[string]interface{}{"p": "LD"},
					Outputs: []VFS{Build("bin/root")},
				})
				emit.Result(root)

				return emit
			},
		},
		{
			name:   "BogusResultRef",
			needle: "out-of-range NodeRef",
			build: func() *BufferedEmitter {
				emit := NewBufferedEmitter()
				emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "FETCH"},
					Outputs: []VFS{Build("resources/CLANG")},
				})
				root := emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "LD"},
					Outputs: []VFS{Build("bin/root")},
				})
				emit.Result(root)
				emit.Result(NodeRef{id: 99})

				return emit
			},
		},
		{
			name:   "PrepopulatedDeps",
			needle: "pre-populated Deps",
			build: func() *BufferedEmitter {
				emit := NewBufferedEmitter()
				emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "FETCH"},
					Outputs: []VFS{Build("resources/CLANG")},
				})
				root := emit.Emit(&Node{
					Deps:    []string{"FAKE"},
					KV:      map[string]interface{}{"p": "LD"},
					Outputs: []VFS{Build("bin/root")},
				})
				emit.Result(root)

				return emit
			},
		},
		{
			name:   "PrepopulatedForeignDeps",
			needle: "pre-populated ForeignDeps",
			build: func() *BufferedEmitter {
				emit := NewBufferedEmitter()
				emit.Emit(&Node{
					KV:      map[string]interface{}{"p": "FETCH"},
					Outputs: []VFS{Build("resources/CLANG")},
				})
				root := emit.Emit(&Node{
					ForeignDeps: map[string][]string{"tool": []string{"FAKE"}},
					KV:          map[string]interface{}{"p": "LD"},
					Outputs:     []VFS{Build("bin/root")},
				})
				emit.Result(root)

				return emit
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, wantExc := finalizeExc(tt.build())
			if wantExc == nil {
				t.Fatal("Finalize unexpectedly accepted invalid emitter")
			}

			_, gotExc := finalizeDumpGraphExc(tt.build())
			if gotExc == nil {
				t.Fatalf("finalizeDumpGraph unexpectedly accepted invalid emitter; want %q", wantExc.Error())
			}

			if got, want := gotExc.Error(), wantExc.Error(); got != want {
				t.Fatalf("finalizeDumpGraph error = %q, want Finalize error %q", got, want)
			}
			if !strings.Contains(gotExc.Error(), tt.needle) {
				t.Fatalf("finalizeDumpGraph error %q does not mention %q", gotExc.Error(), tt.needle)
			}
		})
	}
}

func finalizeDumpGraphExc(e *BufferedEmitter) (g *Graph, exc *Exception) {
	exc = Try(func() {
		g = finalizeDumpGraph(e)
	})

	return g, exc
}

func graphPrimaryOutputs(nodes []*Node) []string {
	out := make([]string, len(nodes))
	for i, node := range nodes {
		if len(node.Outputs) == 0 {
			continue
		}
		out[i] = node.Outputs[0].Rel
	}

	return out
}

func findGraphNodeByOutput(nodes []*Node, rel string) *Node {
	for _, node := range nodes {
		for _, out := range node.Outputs {
			if out.Rel == rel {
				return node
			}
		}
	}

	return nil
}

func assertUIDMatchesNode(t *testing.T, node *Node) {
	t.Helper()

	if node == nil {
		t.Fatal("node missing")
	}

	if got, want := node.UID, nodeUID(node); got != want {
		t.Fatalf("uid = %q, want recomputed %q", got, want)
	}
	if node.SelfUID != node.UID {
		t.Fatalf("self_uid = %q, want uid %q", node.SelfUID, node.UID)
	}
}
