package main

import "testing"

func TestEmitPB_ExtraProtocFlags(t *testing.T) {
	e := NewBufferedEmitter()
	inst := targetInstance("pkg/proto")

	EmitPB(
		inst,
		"pkg/proto/test.proto",
		NodeRef{id: 1},
		NodeRef{id: 2},
		NodeRef{},
		Build("contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		Build("contrib/tools/protoc/protoc"),
		Build("contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		nil,
		"",
		false,
		[]string{"--fatal_warnings"},
		nil,
		nil,
		false,
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	if !contains(e.nodes[0].Cmds[0].CmdArgs, "--fatal_warnings") {
		t.Fatalf("cmd_args missing --fatal_warnings: %v", e.nodes[0].Cmds[0].CmdArgs)
	}
}
