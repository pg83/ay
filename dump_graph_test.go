package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestFinalizeDumpGraph_StripsOnlyTicketScaffolding(t *testing.T) {
	emit := newBufferedEmitter()

	fetchUsed := emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkFETCH},
		Outputs: []VFS{intern("$(B)/resources/YMAKE_PYTHON3")},
	})
	emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkFETCH},
		Outputs: []VFS{intern("$(B)/resources/CLANG")},
	})
	emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkFETCH},
		Outputs: []VFS{intern("$(B)/tool-cache/CLANG")},
	})
	emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{intern("$(B)/contrib/libs/llvm16/include/llvm/IR/Attributes.inc")},
	})
	llvmReferenced := emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{intern("$(B)/contrib/libs/llvm16/include/llvm/IR/IntrinsicsX86.h")},
	})
	emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{intern("$(B)/contrib/libs/llvm16/include/generated.cpp")},
	})
	emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{intern("$(B)/other/module/generated.inc")},
	})
	consumer := emit.emit(&Node{Platform: testTargetP,
		Cmds:           []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"clang"})}}},
		DepRefs:        []NodeRef{llvmReferenced},
		ForeignDepRefs: []NodeRef{fetchUsed},
		KV:             KV{P: pkCC},
		Outputs:        []VFS{intern("$(B)/obj/consumer.o")},
	})
	root := emit.emit(&Node{Platform: testTargetP,
		Cmds:    []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"ld"})}}},
		DepRefs: []NodeRef{consumer},
		KV:      KV{P: pkLD},
		Outputs: []VFS{intern("$(B)/bin/root")},
	})
	emit.result(root)

	got := finalizeDumpGraph(emit)

	if want := []string{
		"bin/root",
		"obj/consumer.o",
		"contrib/libs/llvm16/include/llvm/IR/IntrinsicsX86.h",
		"resources/YMAKE_PYTHON3",
		"resources/CLANG",
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

	pythonNode := findGraphNodeByOutput(got.Graph, "resources/YMAKE_PYTHON3")

	if pythonNode == nil {
		t.Fatal("python fetch node missing after finalizeDumpGraph")
	}

	if !reflect.DeepEqual(graphDeps(got, consumerNode), []UID{llvmNode.UID, pythonNode.UID}) {
		t.Fatalf("consumer deps = %v, want [%s %s]", graphDeps(got, consumerNode), llvmNode.UID, pythonNode.UID)
	}

	if !reflect.DeepEqual(graphForeignDeps(got, consumerNode), []UID{pythonNode.UID}) {
		t.Fatalf("consumer foreign_deps = %v, want [%s]", graphForeignDeps(got, consumerNode), pythonNode.UID)
	}

	assertUIDMatchesNode(t, got, consumerNode)
	assertUIDMatchesNode(t, got, findGraphNodeByOutput(got.Graph, "bin/root"))
}

func TestFinalizeDumpGraph_KeepsMatchingResultNode(t *testing.T) {
	emit := newBufferedEmitter()
	expected := "contrib/libs/llvm16/include/llvm/IR/Attributes.inc"

	root := emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{build(expected)},
	})
	emit.result(root)

	got := finalizeDumpGraph(emit)

	if want := []string{expected}; !reflect.DeepEqual(graphPrimaryOutputs(got.Graph), want) {
		t.Fatalf("dump graph outputs = %v, want %v", graphPrimaryOutputs(got.Graph), want)
	}

	assertUIDMatchesNode(t, got, findGraphNodeByOutput(got.Graph, expected))
}

func TestFinalizeDumpGraph_PrunesTransitiveStandaloneLLVM(t *testing.T) {
	emit := newBufferedEmitter()

	leaf := "contrib/libs/llvm16/include/llvm/IR/Leaf.inc"
	parent := "contrib/libs/llvm16/include/llvm/IR/Parent.inc"

	leafRef := emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkPR},
		Outputs: []VFS{build(leaf)},
	})
	emit.emit(&Node{Platform: testTargetP,
		DepRefs: []NodeRef{leafRef},
		KV:      KV{P: pkPR},
		Outputs: []VFS{build(parent)},
	})
	root := emit.emit(&Node{Platform: testTargetP,
		KV:      KV{P: pkLD},
		Outputs: []VFS{intern("$(B)/bin/root")},
	})
	emit.result(root)

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
				emit := newBufferedEmitter()
				emit.emit(&Node{Platform: testTargetP,
					KV:      KV{P: pkFETCH},
					Outputs: []VFS{intern("$(B)/resources/CLANG")},
				})
				leaf := emit.emit(&Node{Platform: testTargetP,
					KV:      KV{P: pkPR},
					Outputs: []VFS{intern("$(B)/obj/leaf.o")},
				})
				root := emit.emit(&Node{Platform: testTargetP,
					DepRefs: []NodeRef{leaf, NodeRef(99)},
					KV:      KV{P: pkLD},
					Outputs: []VFS{intern("$(B)/bin/root")},
				})
				emit.result(root)

				return emit
			},
		},
		{
			name:   "BogusResultRef",
			needle: "out-of-range NodeRef",
			build: func() *BufferedEmitter {
				emit := newBufferedEmitter()
				emit.emit(&Node{Platform: testTargetP,
					KV:      KV{P: pkFETCH},
					Outputs: []VFS{intern("$(B)/resources/CLANG")},
				})
				root := emit.emit(&Node{Platform: testTargetP,
					KV:      KV{P: pkLD},
					Outputs: []VFS{intern("$(B)/bin/root")},
				})
				emit.result(root)
				emit.result(NodeRef(99))

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
				t.Fatalf("finalizeDumpGraph unexpectedly accepted invalid emitter; want %q", wantExc.error())
			}

			if got, want := gotExc.error(), wantExc.error(); got != want {
				t.Fatalf("finalizeDumpGraph error = %q, want Finalize error %q", got, want)
			}

			if !strings.Contains(gotExc.error(), tt.needle) {
				t.Fatalf("finalizeDumpGraph error %q does not mention %q", gotExc.error(), tt.needle)
			}
		})
	}
}

func finalizeDumpGraphExc(e *BufferedEmitter) (g *Graph, exc *Exception) {
	exc = try(func() {
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

		out[i] = node.Outputs[0].rel()
	}

	return out
}

func findGraphNodeByOutput(nodes []*Node, rel string) *Node {
	for _, node := range nodes {
		for _, out := range node.Outputs {
			if out.rel() == rel {
				return node
			}
		}
	}

	return nil
}

func assertUIDMatchesNode(t *testing.T, g *Graph, node *Node) {
	t.Helper()

	if node == nil {
		t.Fatal("node missing")
	}

	c := CanonBuf{uids: g.uids}

	if got, want := node.UID, nodeUIDWithBuf(node, &c); got != want {
		t.Fatalf("uid = %q, want recomputed %q", got, want)
	}

	if node.SelfUID != node.UID {
		t.Fatalf("self_uid = %q, want uid %q", node.SelfUID, node.UID)
	}
}
