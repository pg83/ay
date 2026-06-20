package main

import "testing"

// TestOverrideGeneratedModuleDir_CppProtoConsumerTagPropagation pins the T-99
// rule: a generated header produced OUTSIDE a PROTO_LIBRARY directory (here a
// RUN_PROGRAM/PR producer under a plain LIBRARY, the apphost cow well-known
// generator) but first-claimed by a consuming CPP_PROTO module is re-attributed
// to that consumer with BOTH its module_dir AND its module_tag — upstream
// Node2Module inherits dir+tag from the owning module. The pre-T-99 pass set
// module_dir only, leaving module_tag unset (the reproduced divergence on the
// well_known *.cow.pb.h nodes).
func TestOverrideGeneratedModuleDir_CppProtoConsumerTagPropagation(t *testing.T) {
	const producerDir = "apphost/gp/lib/proto/cow/generator/well_known"
	const consumerDir = "apphost/lib/proto_answers"

	out := build(producerDir + "/google/protobuf/any.cow.pb.h")

	node := &Node{
		KV:               KV{P: pkPR},
		Outputs:          []VFS{out},
		TargetProperties: TargetProperties{ModuleDir: producerDir},
	}

	e := &BufferedEmitter{
		nodes: []*Node{node},
		generatedFirstClaim: map[VFS]GenOwner{
			out: {Dir: consumerDir, Tag: tagCppProto},
		},
	}

	overrideGeneratedModuleDir(e)

	if got := node.TargetProperties.ModuleDir; got != consumerDir {
		t.Fatalf("module_dir: got %q, want consumer %q", got, consumerDir)
	}

	if got := node.TargetProperties.ModuleTag; got != tagCppProto {
		t.Fatalf("module_tag: got %v, want cpp_proto (%v)", got, tagCppProto)
	}
}

// TestOverrideGeneratedModuleDir_UntaggedConsumerLeavesTagUnset guards the
// common case: a first-claim from a consumer with no module_tag re-attributes
// the dir but must NOT invent a tag.
func TestOverrideGeneratedModuleDir_UntaggedConsumerLeavesTagUnset(t *testing.T) {
	const producerDir = "contrib/tools/gen/producer"
	const consumerDir = "lib/plain_consumer"

	out := build(producerDir + "/gen_table.inc")

	node := &Node{
		KV:               KV{P: pkPR},
		Outputs:          []VFS{out},
		TargetProperties: TargetProperties{ModuleDir: producerDir},
	}

	e := &BufferedEmitter{
		nodes: []*Node{node},
		generatedFirstClaim: map[VFS]GenOwner{
			out: {Dir: consumerDir},
		},
	}

	overrideGeneratedModuleDir(e)

	if got := node.TargetProperties.ModuleDir; got != consumerDir {
		t.Fatalf("module_dir: got %q, want consumer %q", got, consumerDir)
	}

	if got := node.TargetProperties.ModuleTag; got != 0 {
		t.Fatalf("module_tag: got %v, want unset (0)", got)
	}
}
