package main

import (
	"slices"
	"strings"
	"testing"
)

// TestGen_Library_ProtoNamespaceRootsLibraryHostedProtoCommand reproduces the
// T-29 gap: a plain LIBRARY() (not PROTO_LIBRARY) that carries PROTO_NAMESPACE(yt)
// and SRCS(*.proto) compiles its proto through emitLibraryProtoSource, which must
// root the protoc output/import roots at the namespace exactly like the
// PROTO_LIBRARY path. Representative upstream node:
// $(B)/yt/yt/library/query/proto/query.pb.cc.
func TestGen_Library_ProtoNamespaceRootsLibraryHostedProtoCommand(t *testing.T) {
	files := map[string]string{}

	// Peer LIBRARY with the same PROTO_NAMESPACE(yt): its GLOBAL `FOR proto $(S)/yt`
	// source addincl propagates, so the consumer's _PROTO__INCLUDE carries the
	// namespace a second time (own + peer), yielding -I=$(S)/yt three times total.
	writeTestModuleFile(files, "yt/yt/client/ya.make", `LIBRARY()
PROTO_NAMESPACE(yt)
SRCS(data.proto)
END()
`)
	writeTestModuleFile(files, "yt/yt/client/data.proto", "syntax = \"proto3\";\npackage yt;\nmessage Data {}\n")

	writeTestModuleFile(files, "yt/yt/library/query/proto/ya.make", `LIBRARY()
PROTO_NAMESPACE(yt)
SRCS(query.proto)
PEERDIR(yt/yt/client)
END()
`)
	writeTestModuleFile(files, "yt/yt/library/query/proto/query.proto", "syntax = \"proto3\";\npackage yt;\nmessage Query {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "yt/yt/library/query/proto")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/yt/yt/library/query/proto/query.pb.h",
		"$(B)/yt/yt/library/query/proto/query.pb.cc",
	)
	args := strStrs(pb.Cmds[0].CmdArgs.flat())
	count := func(want string) int {
		n := 0
		for _, a := range args {
			if a == want {
				n++
			}
		}
		return n
	}

	// Output roots and the local-dir / source-root import roots carry the namespace.
	for _, tok := range []string{
		"-I=./yt",
		"--cpp_out=:$(B)/yt",
		"--cpp_styleguide_out=:$(B)/yt",
	} {
		if c := count(tok); c != 1 {
			t.Fatalf("library-hosted proto cmd: want exactly one %q, got %d: %v", tok, c, args)
		}
	}

	// The namespace import appears three times: the explicit -I=$ARCADIA_ROOT/$NS
	// prefix plus the two _PROTO__INCLUDE copies (own + the namespace-sharing peer).
	if c := count("-I=$(S)/yt"); c != 3 {
		t.Fatalf("library-hosted proto cmd: want three -I=$(S)/yt (prefix + own + peer), got %d: %v", c, args)
	}

	// None of the unrooted forms may survive.
	for _, tok := range []string{"-I=./", "-I=$(S)/", "--cpp_out=:$(B)/", "--cpp_styleguide_out=:$(B)/"} {
		if c := count(tok); c != 0 {
			t.Fatalf("library-hosted proto cmd: unrooted %q must be gone, got %d: %v", tok, c, args)
		}
	}
}

// TestGen_Library_TopLevelProtoKeepsUnrootedCommand pins the complementary case:
// a top-level LIBRARY proto WITHOUT a PROTO_NAMESPACE keeps the $(B)/ / $(S)/ / ./
// root shape — the prefix is applied only when the module has an effective
// source-root namespace.
func TestGen_Library_TopLevelProtoKeepsUnrootedCommand(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "top/proto/ya.make", `LIBRARY()
SRCS(top.proto)
END()
`)
	writeTestModuleFile(files, "top/proto/top.proto", "syntax = \"proto3\";\npackage top;\nmessage Top {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "top/proto")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/top/proto/top.pb.h",
		"$(B)/top/proto/top.pb.cc",
	)
	args := strStrs(pb.Cmds[0].CmdArgs.flat())

	for _, tok := range []string{"-I=./", "--cpp_out=:$(B)/", "--cpp_styleguide_out=:$(B)/"} {
		if !slices.Contains(args, tok) {
			t.Fatalf("top-level proto cmd: missing unrooted %q: %v", tok, args)
		}
	}
}

func TestEmitPB_ExtraProtocFlags(t *testing.T) {
	e := newBufferedEmitter()
	inst := targetInstance("pkg/proto")

	blocks := composePBArgBlocks(testToolchain(),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false, "", false, false,
		internArgs([]string{"--fatal_warnings"}), nil, nil)
	emitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		false,
		nil,
		nil,
		nil,
		nil,
		blocks,
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	if !contains(e.nodes[0].Cmds[0].CmdArgs.flat(), "--fatal_warnings") {
		t.Fatalf("cmd_args missing --fatal_warnings: %v", e.nodes[0].Cmds[0].CmdArgs.flat())
	}
}

func TestEmitPB_LiteHeadersAddDepsOutputAndCppOutOption(t *testing.T) {
	e := newBufferedEmitter()
	inst := targetInstance("pkg/proto")

	blocks := composePBArgBlocks(testToolchain(),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false, "", false, true,
		nil, nil, nil)
	emitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		true,
		nil,
		nil,
		nil,
		nil,
		blocks,
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
		if got.Outputs[i].string() != want {
			t.Fatalf("outputs[%d] = %q, want %q", i, got.Outputs[i].string(), want)
		}
	}

	if !contains(got.Cmds[0].CmdArgs.flat(), "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("cmd_args missing lite-header cpp_out option: %v", got.Cmds[0].CmdArgs.flat())
	}
	if !contains(got.Cmds[0].CmdArgs.flat(), "$(B)/pkg/proto/test.deps.pb.h") {
		t.Fatalf("cmd_args missing deps header output: %v", got.Cmds[0].CmdArgs.flat())
	}
}

// TestGen_ProtoLibrary_PluginDepAddInclLeadsDeclaredPeer reproduces the sg7
// socdem_type.h_serialized.cpp.o cmds-only divergence: a CPP_PROTO compile must
// emit the proto plugin DEPS' GLOBAL ADDINCL (upstream CPP_EVLOG → eventlog →
// library/cpp/blockcodecs's brotli/snappy) BEFORE the declared PEERDIR closure's
// include dirs. Upstream's per-source `_CPP_PROTO_EVLOG_CMD .PEERDIR=` induces the
// plugin-runtime peer ahead of the declared PEERDIR for include-dir propagation.
// Before the fix the declared peer's -I leads (plugin runtime emitted after),
// which this test rejects.
func TestGen_ProtoLibrary_PluginDepAddInclLeadsDeclaredPeer(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
PEERDIR(declared/peer)
CPP_PROTO_PLUGIN0(myplug tools/myplug DEPS plugin/runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeTestModuleFile(files, "tools/myplug/ya.make", `PROGRAM(myplug)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/myplug/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	// The declared PEERDIR exposes a GLOBAL ADDINCL; the proto-plugin DEPS runtime
	// exposes another. The plugin-runtime include must precede the declared one.
	writeTestModuleFile(files, "declared/peer/ya.make", "LIBRARY()\nADDINCL(GLOBAL declared/peer/inc)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "declared/peer/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "declared/peer/inc/h.h", "#pragma once\n")
	writeTestModuleFile(files, "plugin/runtime/ya.make", "LIBRARY()\nADDINCL(GLOBAL plugin/runtime/inc)\nSRCS(r.cpp)\nEND()\n")
	writeTestModuleFile(files, "plugin/runtime/r.cpp", "int r(){return 0;}\n")
	writeTestModuleFile(files, "plugin/runtime/inc/h.h", "#pragma once\n")

	g := testGen(newMemFS(files), "protos")

	cc := findGraphNodeByOutputs(t, g, "$(B)/protos/test.pb.cc.o")
	args := cc.Cmds[0].CmdArgs.flat()

	pluginInc := indexOfArg(args, "-I$(S)/plugin/runtime/inc")
	declaredInc := indexOfArg(args, "-I$(S)/declared/peer/inc")

	if pluginInc < 0 || declaredInc < 0 {
		t.Fatalf("missing -I dirs in compile cmd: plugin=%d declared=%d args=%v", pluginInc, declaredInc, args)
	}

	if pluginInc > declaredInc {
		t.Fatalf("proto plugin DEPS include must precede declared peer include: plugin/runtime/inc=%d declared/peer/inc=%d", pluginInc, declaredInc)
	}
}

// TestGen_ProtoLibrary_PluginRuntimeLeadsLinkArchiveOrder reproduces the sg7
// dsp_profile_gen link-archive-order-only cmds drift: a PROGRAM linking a
// PROTO_LIBRARY whose CPP_PROTO_PLUGIN0 DEPS (the plugin-runtime peer) and whose
// declared PEERDIR both contribute an archive must emit the plugin-runtime
// archive BEFORE the declared peer's archive — upstream induces the
// plugin-runtime peer (CPP_PROTOBUF_PEERS, ymake.core.conf:2002) ahead of the
// declared PEERDIR in the single peer order that drives both ADDINCL and the
// link/archive closure. Before the fix the declared peer's archive leads (the
// plugin DEP is appended last), which this test rejects. The compile -I order
// (T-14, already correct) is asserted too: the two move together.
func TestGen_ProtoLibrary_PluginRuntimeLeadsLinkArchiveOrder(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "app/ya.make", `PROGRAM(app)
PEERDIR(protos)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
PEERDIR(declared/peer)
CPP_PROTO_PLUGIN0(myplug tools/myplug DEPS plugin/runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeTestModuleFile(files, "tools/myplug/ya.make", `PROGRAM(myplug)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/myplug/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	// Both peers carry an archive AND a GLOBAL ADDINCL. The plugin-runtime peer
	// must precede the declared peer in both the link archive sequence and the
	// proto compile -I order.
	writeTestModuleFile(files, "declared/peer/ya.make", "LIBRARY()\nADDINCL(GLOBAL declared/peer/inc)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "declared/peer/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "declared/peer/inc/h.h", "#pragma once\n")
	writeTestModuleFile(files, "plugin/runtime/ya.make", "LIBRARY()\nADDINCL(GLOBAL plugin/runtime/inc)\nSRCS(r.cpp)\nEND()\n")
	writeTestModuleFile(files, "plugin/runtime/r.cpp", "int r(){return 0;}\n")
	writeTestModuleFile(files, "plugin/runtime/inc/h.h", "#pragma once\n")

	g := testGen(newMemFS(files), "app")

	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV.P == pkLD {
			ldNode = n
			break
		}
	}
	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}

	var linkArgs []STR
	for _, c := range ldNode.Cmds {
		flat := c.CmdArgs.flat()
		if indexOfArg(flat, "$(S)/build/scripts/link_exe.py") >= 0 {
			linkArgs = flat
			break
		}
	}
	if linkArgs == nil {
		t.Fatal("no link_exe.py command found on LD node")
	}

	pluginIdx := indexOfArg(linkArgs, "plugin/runtime/libplugin-runtime.a")
	declaredIdx := indexOfArg(linkArgs, "declared/peer/libdeclared-peer.a")
	if pluginIdx < 0 || declaredIdx < 0 {
		t.Fatalf("link args missing peer archives: plugin=%d declared=%d args=%v", pluginIdx, declaredIdx, linkArgs)
	}
	if pluginIdx > declaredIdx {
		t.Fatalf("plugin-runtime archive [%d] must precede declared peer archive [%d] in link order", pluginIdx, declaredIdx)
	}

	// ADDINCL stability: the proto compile keeps plugin-runtime's -I ahead of the
	// declared peer's (T-14 behavior must not regress).
	cc := findGraphNodeByOutputs(t, g, "$(B)/protos/test.pb.cc.o")
	ccArgs := cc.Cmds[0].CmdArgs.flat()
	pluginInc := indexOfArg(ccArgs, "-I$(S)/plugin/runtime/inc")
	declaredInc := indexOfArg(ccArgs, "-I$(S)/declared/peer/inc")
	if pluginInc < 0 || declaredInc < 0 {
		t.Fatalf("missing -I dirs in compile cmd: plugin=%d declared=%d args=%v", pluginInc, declaredInc, ccArgs)
	}
	if pluginInc > declaredInc {
		t.Fatalf("proto plugin DEPS include must precede declared peer include: plugin=%d declared=%d", pluginInc, declaredInc)
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin0WiresToolDeps(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
GRPC()
CPP_PROTO_PLUGIN0(config_proto_plugin tools/config_plugin DEPS deps/generated_runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(files, "tools/config_plugin/ya.make", `PROGRAM(config_proto_plugin)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(
    deps/plugin_runtime
)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/config_plugin/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "deps/generated_runtime/ya.make", "LIBRARY()\nSRCS(gen.cpp)\nEND()\n")
	writeTestModuleFile(files, "deps/generated_runtime/gen.cpp", "int gen(){return 0;}\n")
	writeTestModuleFile(files, "deps/plugin_runtime/ya.make", "LIBRARY()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "deps/plugin_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.grpc.pb.cc",
		"$(B)/protos/test.grpc.pb.h",
	)
	styleguide := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	grpcCpp := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	protoc := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/protoc")
	configPlugin := mustNodeByOutput(t, g, "$(B)/tools/config_plugin/config_proto_plugin")
	pluginRuntime := mustNodeByOutput(t, g, "$(B)/deps/plugin_runtime/libdeps-plugin_runtime.a")
	_ = mustNodeByOutput(t, g, "$(B)/deps/generated_runtime/libdeps-generated_runtime.a")

	if !containsString(strStrs(pb.Cmds[0].CmdArgs.flat()), "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin") {
		t.Fatalf("pb cmd args missing config proto plugin: %v", pb.Cmds[0].CmdArgs.flat())
	}
	if !containsString(strStrs(pb.Cmds[0].CmdArgs.flat()), "--config_proto_plugin_out=$(B)/") {
		t.Fatalf("pb cmd args missing config proto plugin out flag: %v", pb.Cmds[0].CmdArgs.flat())
	}

	sourceIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "protos/test.proto")
	grpcIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	configIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin")
	if sourceIdx < 0 || grpcIdx < 0 || configIdx < 0 {
		t.Fatalf("missing source/grpc/config args in pb cmd: %v", pb.Cmds[0].CmdArgs.flat())
	}
	if !(sourceIdx < grpcIdx && grpcIdx < configIdx) {
		t.Fatalf("pb plugin arg order = source:%d grpc:%d config:%d, want source < grpc < config", sourceIdx, grpcIdx, configIdx)
	}

	inputs := make([]string, 0, len(pb.flatInputs()))
	for _, input := range pb.flatInputs() {
		inputs = append(inputs, input.string())
	}
	wantInputsPrefix := []string{
		"$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp",
		"$(B)/contrib/tools/protoc/protoc",
		"$(B)/tools/config_plugin/config_proto_plugin",
		"$(S)/build/scripts/cpp_proto_wrapper.py",
		"$(S)/protos/test.proto",
	}
	if len(inputs) < len(wantInputsPrefix) || !equalStrings(inputs[:len(wantInputsPrefix)], wantInputsPrefix) {
		t.Fatalf("pb inputs prefix = %v, want %v", inputs, wantInputsPrefix)
	}

	wantDeps := []UID{styleguide.UID, grpcCpp.UID, protoc.UID, configPlugin.UID}
	if len(graphDeps(g, pb)) != len(wantDeps) {
		t.Fatalf("pb deps len = %d, want %d (%v)", len(graphDeps(g, pb)), len(wantDeps), graphDeps(g, pb))
	}
	for _, want := range wantDeps {
		if !slices.Contains(graphDeps(g, pb), want) {
			t.Fatalf("pb deps = %v, missing %q", graphDeps(g, pb), want)
		}
	}
	if got := graphForeignDeps(g, pb); len(got) != len(wantDeps) {
		t.Fatalf("pb foreign_deps[tool] len = %d, want %d (%v)", len(got), len(wantDeps), got)
	} else {
		for _, want := range wantDeps {
			if !slices.Contains(got, want) {
				t.Fatalf("pb foreign_deps[tool] = %v, missing %q", got, want)
			}
		}
	}
	if !slices.Contains(graphDeps(g, configPlugin), pluginRuntime.UID) {
		t.Fatalf("config proto plugin deps = %v, want runtime peer uid %q", graphDeps(g, configPlugin), pluginRuntime.UID)
	}
}

func TestGen_ProtoLibrary_CPPProtoPluginOutputsReachWrapper(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(tasklet_cpp tools/tasklet_plugin .tasklet.h)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "tools/tasklet_plugin/ya.make", `PROGRAM(tasklet_cpp)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/tasklet_plugin/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	)

	outputsIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--outputs")
	separatorIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--")
	if outputsIdx < 0 || separatorIdx < 0 || separatorIdx <= outputsIdx {
		t.Fatalf("pb wrapper output section malformed: %v", pb.Cmds[0].CmdArgs.flat())
	}

	wantWrapperOutputs := []string{
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	}
	gotWrapperOutputs := pb.Cmds[0].CmdArgs.flat()[outputsIdx+1 : separatorIdx]
	if !equalStrings(strStrs(gotWrapperOutputs), wantWrapperOutputs) {
		t.Fatalf("pb wrapper outputs = %v, want %v", gotWrapperOutputs, wantWrapperOutputs)
	}
}

func TestGen_ProtoLibrary_YAFFContributesCppProtoPlugin(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
YAFF(NAMESPACE NMyNs FILES
    a.proto
    EXPERIMENTAL
    b.proto
)
SRCS(
    a.proto
    b.proto
)
END()
`)
	writeTestModuleFile(files, "protos/a.proto", "syntax = \"proto3\";\npackage test;\nmessage A {}\n")
	writeTestModuleFile(files, "protos/b.proto", "syntax = \"proto3\";\npackage test;\nmessage B {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/a.pb.h",
		"$(B)/protos/a.pb.cc",
		"$(B)/protos/a.yaff.h",
		"$(B)/protos/a.yaff.cpp",
	)

	args := strStrs(pb.Cmds[0].CmdArgs.flat())
	wantArgs := []string{
		"--plugin=protoc-gen-yaff=$(B)/library/cpp/yaff/tools/protoc_plugin/protoc_plugin",
		"--yaff_out=$(B)/",
		"--yaff_opt=namespace=NMyNs",
		"--yaff_opt=file=a.proto",
		"--yaff_opt=experimental=b.proto",
	}
	for _, want := range wantArgs {
		if !containsString(args, want) {
			t.Fatalf("pb cmd args missing %q: %v", want, args)
		}
	}

	// The plugin opt block must NOT carry the unsplit/colon form.
	for _, bad := range args {
		if strings.HasPrefix(bad, "--yaff_opt=:") || bad == "--yaff_opt=namespace=NMyNs,file=a.proto,experimental=b.proto" {
			t.Fatalf("pb cmd args carry unsplit/colon yaff_opt %q: %v", bad, args)
		}
	}

	if !nodeHasInput(pb, "$(B)/library/cpp/yaff/tools/protoc_plugin/protoc_plugin") {
		t.Fatalf("pb inputs missing yaff plugin binary: %#v", pb.flatInputs())
	}

	// Every proto in the module gets per-proto yaff outputs, not just the listed ones.
	_ = mustNodeByAnyOutput(t, g, "$(B)/protos/b.yaff.h")
	_ = mustNodeByAnyOutput(t, g, "$(B)/protos/b.yaff.cpp")
}

func TestGen_ProtoLibrary_YAFFSchemaContributesCppProtoPlugin(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
YAFF_SCHEMA(tsar_vectors NUserProfileTsarVectors)
SRCS(
    a.proto
)
END()
`)
	writeTestModuleFile(files, "protos/a.proto", "syntax = \"proto3\";\npackage test;\nmessage A {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/a.pb.h",
		"$(B)/protos/a.pb.cc",
		"$(B)/protos/a_tsar_vectors.yaff.h",
		"$(B)/protos/a_tsar_vectors.yaff.cpp",
	)

	args := strStrs(pb.Cmds[0].CmdArgs.flat())
	wantArgs := []string{
		"--plugin=protoc-gen-yaff_tsar_vectors=$(B)/library/cpp/yaff/tools/protoc_plugin/protoc_plugin",
		"--yaff_tsar_vectors_out=$(B)/",
		"--yaff_tsar_vectors_opt=tag=tsar_vectors",
		"--yaff_tsar_vectors_opt=namespace=NUserProfileTsarVectors",
	}
	for _, want := range wantArgs {
		if !containsString(args, want) {
			t.Fatalf("pb cmd args missing %q: %v", want, args)
		}
	}

	// tag precedes namespace, mirroring the upstream EXTRA_OUT_FLAG order.
	tagIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--yaff_tsar_vectors_opt=tag=tsar_vectors")
	nsIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--yaff_tsar_vectors_opt=namespace=NUserProfileTsarVectors")
	if !(tagIdx >= 0 && nsIdx >= 0 && tagIdx < nsIdx) {
		t.Fatalf("yaff_tsar_vectors opt order = tag:%d namespace:%d, want tag < namespace", tagIdx, nsIdx)
	}

	if !nodeHasInput(pb, "$(B)/library/cpp/yaff/tools/protoc_plugin/protoc_plugin") {
		t.Fatalf("pb inputs missing yaff plugin binary: %#v", pb.flatInputs())
	}
}

func TestGen_ProtoLibrary_TransitivePROTONamespaceReachesCppProtoCmd(t *testing.T) {
	files := map[string]string{}

	// Leaf PROTO_LIBRARY declares a bare (non-GLOBAL) PROTO_NAMESPACE(yt).
	// Upstream expands it to `GLOBAL FOR proto $(S)/yt`, which propagates through
	// the CPP_PROTO peer closure into every transitive consumer's protoc command
	// as -I=$(S)/yt — including PROTO_LIBRARY (cpp_proto) consumers.
	writeTestModuleFile(files, "leaf/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
SRCS(leaf.proto)
END()
`)
	writeTestModuleFile(files, "leaf/leaf.proto", "syntax = \"proto3\";\npackage test;\nmessage Leaf {}\n")

	// Intermediate declares a GLOBAL PROTO_NAMESPACE: it rides the
	// _PROTO__INCLUDE chain (peerProtoAddIncl), ahead of the bare-namespace tail.
	writeTestModuleFile(files, "mid/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(GLOBAL midns)
PEERDIR(leaf)
SRCS(mid.proto)
END()
`)
	writeTestModuleFile(files, "mid/mid.proto", "syntax = \"proto3\";\npackage test;\nmessage Mid {}\n")

	writeTestModuleFile(files, "consumer/ya.make", `PROTO_LIBRARY()
PEERDIR(mid)
SRCS(brand.proto)
END()
`)
	writeTestModuleFile(files, "consumer/brand.proto", "syntax = \"proto3\";\npackage test;\nmessage Brand {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/consumer/brand.pb.h",
		"$(B)/consumer/brand.pb.cc",
	)

	args := strStrs(pb.Cmds[0].CmdArgs.flat())

	ytCount := 0
	for _, a := range args {
		if a == "-I=$(S)/yt" {
			ytCount++
		}
	}
	if ytCount == 0 {
		t.Fatalf("consumer pb cmd missing transitive PROTO_NAMESPACE token -I=$(S)/yt: %v", args)
	}
	if ytCount > 1 {
		t.Fatalf("consumer pb cmd duplicates -I=$(S)/yt (%d times): %v", ytCount, args)
	}

	// The bare-namespace tail (yt) trails the GLOBAL-namespace chain entry
	// (midns) in the _PROTO__INCLUDE order, and both sit inside the include
	// block (before --cpp_out).
	chainIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "-I=$(S)/midns")
	ytIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "-I=$(S)/yt")
	cppOutIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--cpp_out=:$(B)/")
	if chainIdx < 0 {
		t.Fatalf("consumer pb cmd missing GLOBAL chain token -I=$(S)/midns: %v", args)
	}
	if !(chainIdx < ytIdx && ytIdx < cppOutIdx) {
		t.Fatalf("expected chain < tail < cpp_out: midns=%d yt=%d cpp_out=%d args=%v", chainIdx, ytIdx, cppOutIdx, args)
	}
}

func TestGen_ProtoLibrary_ExportYmapsProtoReachesCppProtoCmd(t *testing.T) {
	files := map[string]string{}

	// A maps proto leaf uses EXPORT_YMAPS_PROTO(), which upstream expands to
	// PROTO_NAMESPACE(maps/doc/proto) -> PROTO_ADDINCL(GLOBAL maps/doc/proto).
	// The GLOBAL FOR proto SOURCE arm propagates $(S)/maps/doc/proto through the
	// CPP_PROTO peer closure into every transitive consumer's protoc command.
	writeTestModuleFile(files, "leaf/ya.make", `PROTO_LIBRARY()
EXPORT_YMAPS_PROTO()
SRCS(leaf.proto)
END()
`)
	writeTestModuleFile(files, "leaf/leaf.proto", "syntax = \"proto3\";\npackage test;\nmessage Leaf {}\n")

	writeTestModuleFile(files, "consumer/ya.make", `PROTO_LIBRARY()
PEERDIR(leaf)
SRCS(brand.proto)
END()
`)
	writeTestModuleFile(files, "consumer/brand.proto", "syntax = \"proto3\";\npackage test;\nmessage Brand {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/consumer/brand.pb.h",
		"$(B)/consumer/brand.pb.cc",
	)

	args := strStrs(pb.Cmds[0].CmdArgs.flat())

	const wantTok = "-I=$(S)/maps/doc/proto"
	mapsCount := 0
	for _, a := range args {
		if a == wantTok {
			mapsCount++
		}
	}
	if mapsCount == 0 {
		t.Fatalf("consumer pb cmd missing transitive EXPORT_YMAPS_PROTO token %s: %v", wantTok, args)
	}
	if mapsCount > 1 {
		t.Fatalf("consumer pb cmd duplicates %s (%d times): %v", wantTok, mapsCount, args)
	}

	mapsIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), wantTok)
	cppOutIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--cpp_out=:$(B)/")
	if !(mapsIdx >= 0 && mapsIdx < cppOutIdx) {
		t.Fatalf("expected maps/doc/proto include before --cpp_out: maps=%d cpp_out=%d args=%v", mapsIdx, cppOutIdx, args)
	}

	// No source-root C++ include leakage: a C++ `-I$(S)/maps/doc/proto` is the
	// SOURCE arm of PROTO_ADDINCL's _ORDER_ADDINCL and belongs only to the protoc
	// command, never to a C++ compile. (The build-root `-I$(B)/maps/doc/proto`,
	// the ADDINCL(GLOBAL $(B)/...) half, IS expected on C++ compiles — T-32.)
	const cppSourceLeak = "-I$(S)/maps/doc/proto"
	for _, n := range g.Graph {
		for _, cmd := range n.Cmds {
			for _, a := range strStrs(cmd.CmdArgs.flat()) {
				if a == cppSourceLeak {
					t.Fatalf("source-root C++ include leakage of maps/doc/proto: token %q on outputs %v", a, vfsStrings(n.Outputs))
				}
			}
		}
	}
}

func TestGen_ProtoLibrary_ExportYmapsProtoReachesCppBuildRootAddIncl(t *testing.T) {
	files := map[string]string{}

	// EXPORT_YMAPS_PROTO() -> PROTO_NAMESPACE(maps/doc/proto) ->
	// PROTO_ADDINCL(GLOBAL maps/doc/proto). Besides the protoc include closure
	// (T-30), PROTO_ADDINCL emits `ADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/$Path)` —
	// an ordinary GLOBAL C++ ADDINCL of the build root that propagates through the
	// peer addincl closure into every transitive consumer's C++ compile commands,
	// including the generated-protobuf compile node `*.pb.cc.o`.
	writeTestModuleFile(files, "leaf/ya.make", `PROTO_LIBRARY()
EXPORT_YMAPS_PROTO()
SRCS(leaf.proto)
END()
`)
	writeTestModuleFile(files, "leaf/leaf.proto", "syntax = \"proto3\";\npackage test;\nmessage Leaf {}\n")

	writeTestModuleFile(files, "consumer/ya.make", `PROTO_LIBRARY()
PEERDIR(leaf)
SRCS(brand.proto)
END()
`)
	writeTestModuleFile(files, "consumer/brand.proto", "syntax = \"proto3\";\npackage test;\nmessage Brand {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	// The generated-protobuf C++ compile node for the consumer.
	ccObj := findGraphNodeByOutputs(t, g, "$(B)/consumer/brand.pb.cc.o")

	args := strStrs(ccObj.Cmds[0].CmdArgs.flat())

	const wantBuildTok = "-I$(B)/maps/doc/proto"
	const sourceTok = "-I$(S)/maps/doc/proto"

	buildCount, sourceCount := 0, 0
	for _, a := range args {
		switch a {
		case wantBuildTok:
			buildCount++
		case sourceTok:
			sourceCount++
		}
	}

	if buildCount == 0 {
		t.Fatalf("consumer pb.cc.o cmd missing transitive build-root addincl %s: %v", wantBuildTok, args)
	}
	if buildCount > 1 {
		t.Fatalf("consumer pb.cc.o cmd duplicates %s (%d times): %v", wantBuildTok, buildCount, args)
	}
	if sourceCount != 0 {
		t.Fatalf("source-root C++ include leakage %s on pb.cc.o (%d times): %v", sourceTok, sourceCount, args)
	}
}

func TestGen_ProtoLibrary_ExportYmapsProtoSetsProtoNamespaceOutputRoot(t *testing.T) {
	files := map[string]string{}

	// EXPORT_YMAPS_PROTO() == PROTO_NAMESPACE(maps/doc/proto). Besides the
	// transitive include effects (T-30/T-32), its SET(PROTO_NAMESPACE maps/doc/proto)
	// roots the maps module's OWN protoc command and output paths under
	// maps/doc/proto, and roots the proto import search path at $(S)/maps/doc/proto.
	const moduleDir = "maps/doc/proto/yandex/maps/proto/common2"
	writeTestModuleFile(files, moduleDir+"/ya.make", `PROTO_LIBRARY()
EXPORT_YMAPS_PROTO()
SRCS(response.proto attribution.proto)
END()
`)
	writeTestModuleFile(files, moduleDir+"/response.proto", `syntax = "proto3";
package yandex.maps.proto.common2;
import "yandex/maps/proto/common2/attribution.proto";
message Response {
  Attribution attribution = 1;
}
`)
	writeTestModuleFile(files, moduleDir+"/attribution.proto", `syntax = "proto3";
package yandex.maps.proto.common2;
message Attribution {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), moduleDir)

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/"+moduleDir+"/response.pb.h",
		"$(B)/"+moduleDir+"/response.pb.cc",
	)

	args := strStrs(pb.Cmds[0].CmdArgs.flat())
	count := func(want string) int {
		n := 0
		for _, a := range args {
			if a == want {
				n++
			}
		}
		return n
	}

	// Output roots and the local-dir import root must carry the namespace, once.
	for _, tok := range []string{
		"-I=./maps/doc/proto",
		"--cpp_out=:$(B)/maps/doc/proto",
		"--cpp_styleguide_out=:$(B)/maps/doc/proto",
	} {
		if c := count(tok); c != 1 {
			t.Fatalf("response.pb cmd: want exactly one %q, got %d: %v", tok, c, args)
		}
	}

	// The source-root namespace include must be present (the generic command
	// construction emits it both as the include-root and the cppOutRoot arm).
	if count("-I=$(S)/maps/doc/proto") == 0 {
		t.Fatalf("response.pb cmd missing -I=$(S)/maps/doc/proto: %v", args)
	}

	// None of the unrooted forms may survive.
	for _, tok := range []string{
		"-I=./",
		"-I=$(S)/",
		"--cpp_out=:$(B)/",
		"--cpp_styleguide_out=:$(B)/",
	} {
		if c := count(tok); c != 0 {
			t.Fatalf("response.pb cmd: unrooted %q must be gone, got %d: %v", tok, c, args)
		}
	}

	// The imported proto resolves through the namespace root, proving the import
	// search path roots at $(S)/maps/doc/proto.
	wantImport := "$(S)/" + moduleDir + "/attribution.proto"
	inputs := vfsStrings(pb.Inputs.flat())
	if !slices.Contains(inputs, wantImport) {
		t.Fatalf("response.pb inputs missing imported proto %q: %v", wantImport, inputs)
	}

	// The generated C++ compile gains the build-root namespace include exactly
	// once, and never a source-root C++ include.
	ccObj := findGraphNodeByOutputs(t, g, "$(B)/"+moduleDir+"/response.pb.cc.o")
	ccArgs := strStrs(ccObj.Cmds[0].CmdArgs.flat())
	buildCount, sourceCount := 0, 0
	for _, a := range ccArgs {
		switch a {
		case "-I$(B)/maps/doc/proto":
			buildCount++
		case "-I$(S)/maps/doc/proto":
			sourceCount++
		}
	}
	if buildCount != 1 {
		t.Fatalf("response.pb.cc.o: want exactly one -I$(B)/maps/doc/proto, got %d: %v", buildCount, ccArgs)
	}
	if sourceCount != 0 {
		t.Fatalf("response.pb.cc.o: source-root C++ include leakage, got %d: %v", sourceCount, ccArgs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoKeepsPublicImportsOnLitePBHeader(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(
    leaf.proto
    public.proto
    main.proto
)
END()
`)
	writeTestModuleFile(files, "protos/leaf.proto", `syntax = "proto3";
package test;
message Leaf {
  string value = 1;
}
`)
	writeTestModuleFile(files, "protos/public.proto", `syntax = "proto3";
package test;
import public "leaf.proto";
message PublicMessage {
  Leaf leaf = 1;
}
`)
	writeTestModuleFile(files, "protos/main.proto", `syntax = "proto3";
package test;
import public "public.proto";
message Main {
  PublicMessage message = 1;
}
`)
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", `#include <protos/main.pb.h>
int use() { return 0; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	publicPB := mustNodeByOutput(t, g, "$(B)/protos/public.pb.h")
	leafPB := mustNodeByOutput(t, g, "$(B)/protos/leaf.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.pb.h",
		"$(B)/protos/public.pb.h",
		"$(B)/protos/leaf.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.flatInputs())
		}
	}
	for _, want := range []UID{mainPB.UID, publicPB.UID, leafPB.UID} {
		if !slices.Contains(graphDeps(g, useCC), want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, graphDeps(g, useCC))
		}
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNo_DepsHeaderUsesRuntimeRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
import "google/protobuf/any.proto";
message Row {
  google.protobuf.Any body = 1;
}
`)
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", `#include <protos/test.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/any.proto", `syntax = "proto3";
package google.protobuf;
message Any {}
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/any.pb.h", "#pragma once\n")

	g := testGen(newMemFS(files), "app")
	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.deps.pb.h",
	)
	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	if !nodeHasInput(use, "$(B)/protos/test.deps.pb.h") {
		t.Fatalf("use.cpp.o inputs missing deps header output: %#v", use.flatInputs())
	}
	if !nodeHasInput(use, "$(S)/contrib/libs/protobuf/src/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs missing protobuf runtime WKT header: %#v", use.flatInputs())
	}
	if nodeHasInput(use, "$(S)/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs still contain unrebased WKT header path: %#v", use.flatInputs())
	}
	if !slices.Contains(graphDeps(g, use), pb.UID) {
		t.Fatalf("use.cpp.o deps missing PB producer uid %q: %v", pb.UID, graphDeps(g, use))
	}
}
