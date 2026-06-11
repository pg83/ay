package main

import "testing"

func TestEmitPB_ExtraProtocFlags(t *testing.T) {
	e := NewBufferedEmitter()
	inst := targetInstance("pkg/proto")

	EmitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		Intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		Intern("$(B)/contrib/tools/protoc/protoc"),
		Intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		"",
		false,
		false,
		internArgs([]string{"--fatal_warnings"}),
		nil,
		nil,
		nil,
		nil,
		nil,
		testToolchain(),
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	if !contains(e.nodes[0].Cmds[0].CmdArgs, "--fatal_warnings") {
		t.Fatalf("cmd_args missing --fatal_warnings: %v", e.nodes[0].Cmds[0].CmdArgs)
	}
}

func TestEmitPB_LiteHeadersAddDepsOutputAndCppOutOption(t *testing.T) {
	e := NewBufferedEmitter()
	inst := targetInstance("pkg/proto")

	EmitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		Intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		Intern("$(B)/contrib/tools/protoc/protoc"),
		Intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		"",
		false,
		true,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		testToolchain(),
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]
	wantOutputs := []string{
		"$(B)/pkg/proto/test.pb.h",
		"$(B)/pkg/proto/test.pb.cc",
		"$(B)/pkg/proto/test.deps.pb.h",
	}
	if len(got.Outputs) != len(wantOutputs) {
		t.Fatalf("outputs len = %d, want %d (%v)", len(got.Outputs), len(wantOutputs), got.Outputs)
	}
	for i, want := range wantOutputs {
		if got.Outputs[i].String() != want {
			t.Fatalf("outputs[%d] = %q, want %q", i, got.Outputs[i].String(), want)
		}
	}

	if !contains(got.Cmds[0].CmdArgs, "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("cmd_args missing lite-header cpp_out option: %v", got.Cmds[0].CmdArgs)
	}
	if !contains(got.Cmds[0].CmdArgs, "$(B)/pkg/proto/test.deps.pb.h") {
		t.Fatalf("cmd_args missing deps header output: %v", got.Cmds[0].CmdArgs)
	}
}
