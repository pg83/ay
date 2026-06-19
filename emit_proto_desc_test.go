package main

import (
	"strings"
	"testing"
)

// TestDescProtoOutputRel_SRCDIRRebasesDescUnderModule pins the upstream
// ${output;suf=.desc:File} SRCDIR rebasing for DESC_PROTO `.desc` outputs: when a
// .proto SRC resolves (through SRCDIR) outside the declaring builtin module, the
// `.desc` output must root under the module build dir with the `..` ascent mapped
// to `__` segments; an in-module source keeps its rootrel path. Before the fix
// descProtoOutputRel does not exist (compile failure) and the resolved physical
// path was used verbatim.
func TestDescProtoOutputRel_SRCDIRRebasesDescUnderModule(t *testing.T) {
	cases := []struct {
		name, instance, srcRel, resolved, want string
	}{
		{
			name:     "protos_from_protobuf any.proto",
			instance: "contrib/libs/protobuf/builtin_proto/protos_from_protobuf",
			srcRel:   "google/protobuf/any.proto",
			resolved: "contrib/libs/protobuf/src/google/protobuf/any.proto",
			want:     "contrib/libs/protobuf/builtin_proto/protos_from_protobuf/__/__/src/google/protobuf/any.proto.desc",
		},
		{
			name:     "protos_from_protoc plugin.proto",
			instance: "contrib/libs/protobuf/builtin_proto/protos_from_protoc",
			srcRel:   "google/protobuf/compiler/plugin.proto",
			resolved: "contrib/libs/protoc/src/google/protobuf/compiler/plugin.proto",
			want:     "contrib/libs/protobuf/builtin_proto/protos_from_protoc/__/__/__/protoc/src/google/protobuf/compiler/plugin.proto.desc",
		},
		{
			name:     "normal in-module source",
			instance: "myproto",
			srcRel:   "foo.proto",
			resolved: "myproto/foo.proto",
			want:     "myproto/foo.proto.desc",
		},
	}

	for _, c := range cases {
		if got := descProtoOutputRel(c.instance, c.srcRel, c.resolved); got != c.want {
			t.Errorf("%s: descProtoOutputRel(%q, %q, %q) = %q, want %q",
				c.name, c.instance, c.srcRel, c.resolved, got, c.want)
		}
	}
}

// TestEmitDescProto_SRCDIRBuiltinDescRoot is the graph regression for the T-37
// residual: a protos_from_protobuf-style PROTO_LIBRARY whose SRCS resolve through
// SRCDIR outside the module must emit its `.desc` under the module build root
// (with `__` ascent), keep `.rawproto` at the physical source root, and feed the
// rebased `.desc` to its `.self.protodesc` merge command.
func TestEmitDescProto_SRCDIRBuiltinDescRoot(t *testing.T) {
	const moduleDir = "contrib/libs/protobuf/builtin_proto/protos_from_protobuf"
	const srcDir = "contrib/libs/protobuf/src"
	const descDir = "desc"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files[moduleDir+"/ya.make"] = "PROTO_LIBRARY()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nPROTO_NAMESPACE(GLOBAL " + srcDir + ")\nSRCDIR(" + srcDir + ")\nSRCS(google/protobuf/any.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[srcDir+"/google/protobuf/any.proto"] = "syntax = \"proto3\";\npackage google.protobuf;\nmessage Any { int32 x = 1; }\n"
	files[descDir+"/ya.make"] = "PROTO_DESCRIPTIONS()\nPEERDIR(" + moduleDir + ")\nEND()\n"

	g := testGen(newMemFS(files), descDir)

	outputs := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			outputs[o.string()] = n
		}
	}

	hash := moddirHash(moduleDir)
	rebasedDesc := "$(B)/" + moduleDir + "/__/__/src/google/protobuf/any.proto.desc"
	physDesc := "$(B)/" + srcDir + "/google/protobuf/any.proto.desc"
	rawOut := "$(B)/" + srcDir + "/google/protobuf/any.proto." + hash + ".rawproto"

	if outputs[rebasedDesc] == nil {
		t.Errorf("graph missing module-rooted .desc output %q", rebasedDesc)
	}
	if outputs[physDesc] != nil {
		t.Errorf("graph still emits physical-source-root .desc output %q", physDesc)
	}
	if outputs[rawOut] == nil {
		t.Errorf("graph missing physical-source-root .rawproto output %q", rawOut)
	}

	prj := realPrjName(moduleDir)
	merge := outputs["$(B)/"+moduleDir+"/"+prj+".self.protodesc"]
	if merge == nil {
		t.Fatalf("no .self.protodesc merge node")
	}
	var mergeCmd string
	for _, c := range merge.Cmds {
		for _, a := range c.CmdArgs.flat() {
			mergeCmd += a.string() + " "
		}
	}
	if !strings.Contains(mergeCmd, moduleDir+"/__/__/src/google/protobuf/any.proto.desc") {
		t.Errorf(".self.protodesc merge cmd does not consume rebased .desc\ncmd: %s", mergeCmd)
	}
	if strings.Contains(mergeCmd, srcDir+"/google/protobuf/any.proto.desc") {
		t.Errorf(".self.protodesc merge cmd still consumes physical-source-root .desc\ncmd: %s", mergeCmd)
	}
}

// TestEmitProtoDescriptions_PDProducerShape reproduces the sg7 PD gap: a
// PROTO_DESCRIPTIONS target that PEERDIRs a PROTO_LIBRARY must, via the
// DESC_PROTO submodule, emit one proto-description producer per .proto SRC
// (desc_rawproto_wrapper.py around protoc) writing <proto>.desc and the hashed
// <proto>.<md5(MODDIR)>.rawproto, plus the .self.protodesc / .protosrc and the
// PROTO_DESCRIPTIONS .protodesc / .tar merge outputs. Before this change none of
// these nodes existed, so the assertions (and the missing emitter symbol) fail.
func TestEmitProtoDescriptions_PDProducerShape(t *testing.T) {
	const protoDir = "myproto"
	const descDir = "desc"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files[protoDir+"/ya.make"] = "PROTO_LIBRARY()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nSRCS(foo.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[protoDir+"/foo.proto"] = "syntax = \"proto3\";\npackage foo;\nmessage Foo { int32 x = 1; }\n"
	files[descDir+"/ya.make"] = "PROTO_DESCRIPTIONS()\nPEERDIR(" + protoDir + ")\nEND()\n"

	g := testGen(newMemFS(files), descDir)

	outputs := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			outputs[o.string()] = n
		}
	}

	hash := moddirHash(protoDir)
	descOut := "$(B)/" + protoDir + "/foo.proto.desc"
	rawOut := "$(B)/" + protoDir + "/foo.proto." + hash + ".rawproto"

	// The merge outputs must exist (no longer reference-only).
	for _, want := range []string{
		descOut,
		rawOut,
		"$(B)/" + protoDir + "/myproto.self.protodesc",
		"$(B)/" + protoDir + "/myproto.protosrc",
		"$(B)/" + descDir + "/desc.protodesc",
		"$(B)/" + descDir + "/desc.tar",
	} {
		if outputs[want] == nil {
			t.Errorf("graph missing PD output %q", want)
		}
	}

	pd := outputs[descOut]
	if pd == nil {
		t.Fatalf("no PD producer node for %q", descOut)
	}

	// kv p=PD pc=light-cyan; module_tag desc_proto.
	if pd.KV.P != pkPD {
		t.Errorf("PD producer kv.p = %v, want PD", pd.KV.P)
	}
	if pd.KV.PC != pcLightCyan {
		t.Errorf("PD producer kv.pc = %v, want light-cyan", pd.KV.PC)
	}
	if pd.TargetProperties.ModuleTag != strDescProtoTag {
		t.Errorf("PD producer module_tag = %q, want desc_proto", pd.TargetProperties.ModuleTag.string())
	}

	// Both produced files on the one node.
	if len(pd.Outputs) != 2 || pd.Outputs[0].string() != descOut || pd.Outputs[1].string() != rawOut {
		t.Errorf("PD producer outputs = %v, want [%s %s]", pd.Outputs, descOut, rawOut)
	}

	// deps == foreign_deps == [protoc]: the tool rides ForeignDepRefs, no
	// separate DepRefs.
	if len(pd.DepRefs) != 0 {
		t.Errorf("PD producer DepRefs = %d, want 0 (protoc rides foreign_deps)", len(pd.DepRefs))
	}
	if len(pd.ForeignDepRefs) != 1 {
		t.Errorf("PD producer ForeignDepRefs = %d, want 1 (protoc)", len(pd.ForeignDepRefs))
	}

	// Inputs include the source proto, the wrapper, and protoc.
	ins := map[string]bool{}
	for _, in := range pd.flatInputs() {
		ins[in.string()] = true
	}
	for _, want := range []string{
		"$(S)/" + protoDir + "/foo.proto",
		"$(S)/build/scripts/desc_rawproto_wrapper.py",
	} {
		if !ins[want] {
			t.Errorf("PD producer inputs missing %q (have %v)", want, pd.flatInputs())
		}
	}
	var hasProtoc bool
	for in := range ins {
		if strings.HasSuffix(in, "/protoc") {
			hasProtoc = true
		}
	}
	if !hasProtoc {
		t.Errorf("PD producer inputs missing protoc binary (have %v)", pd.flatInputs())
	}

	// The command runs the wrapper with --desc-output / --rawproto-output /
	// --proto-file then `-- protoc … --include_source_info`.
	flat := pd.Cmds[0].CmdArgs.flat()
	joined := make([]string, len(flat))
	for i, a := range flat {
		joined[i] = a.string()
	}
	cmd := strings.Join(joined, " ")
	for _, want := range []string{
		"build/scripts/desc_rawproto_wrapper.py",
		"--desc-output",
		"--rawproto-output",
		"--proto-file " + protoDir + "/foo.proto",
		"--include_source_info",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("PD producer cmd missing %q\ncmd: %s", want, cmd)
		}
	}
}

// TestEmitDescProto_MergeNodeFlattensProducerSourceInputs reproduces the T-51
// Split A residual: the DESC_PROTO submodule merge node (.self.protodesc /
// .protosrc) must carry, as direct inputs, the per-proto producer source/script
// closure in addition to the generated .desc/.rawproto and its own merge/collect
// scripts. Upstream-normalized merge nodes flatten the desc_rawproto_wrapper.py
// script, every declared source proto, and the parsed proto import closure
// (e.g. an imported, non-source descriptor.proto). Before this change the merge
// node received only generated descriptor/rawproto inputs plus merge scripts, so
// the wrapper, source, and import inputs are reference-only.
func TestEmitDescProto_MergeNodeFlattensProducerSourceInputs(t *testing.T) {
	const moduleDir = "contrib/libs/protobuf/builtin_proto/protos_from_protobuf"
	const srcDir = "contrib/libs/protobuf/src"
	const descDir = "descmerge"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files["build/scripts/desc_rawproto_wrapper.py"] = "print('wrap')\n"
	files["build/scripts/merge_files.py"] = "print('merge')\n"
	files["build/scripts/collect_rawproto.py"] = "print('collect')\n"
	// type.proto is a declared source that imports any.proto; any.proto is NOT a
	// source — it is an import-only descriptor that must still reach the merge
	// node as a direct input (the protos_from_protoc → descriptor.proto shape).
	files[moduleDir+"/ya.make"] = "PROTO_LIBRARY()\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nPROTO_NAMESPACE(GLOBAL " + srcDir + ")\nSRCDIR(" + srcDir + ")\nSRCS(google/protobuf/type.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[srcDir+"/google/protobuf/any.proto"] = "syntax = \"proto3\";\npackage google.protobuf;\nmessage Any { int32 x = 1; }\n"
	files[srcDir+"/google/protobuf/type.proto"] = "syntax = \"proto3\";\npackage google.protobuf;\nimport \"google/protobuf/any.proto\";\nmessage Type { Any a = 1; }\n"
	files[descDir+"/ya.make"] = "PROTO_DESCRIPTIONS()\nPEERDIR(" + moduleDir + ")\nEND()\n"

	g := testGen(newMemFS(files), descDir)

	prj := realPrjName(moduleDir)
	merge := mustNodeByAnyOutput(t, g, "$(B)/"+moduleDir+"/"+prj+".self.protodesc")

	// The same node also produces .protosrc (one merge node, two outputs); the
	// flattened direct inputs cover the sibling output class too.
	var hasProtosrc bool
	for _, o := range merge.Outputs {
		if o.string() == "$(B)/"+moduleDir+"/"+prj+".protosrc" {
			hasProtosrc = true
		}
	}
	if !hasProtosrc {
		t.Fatalf("merge node does not also emit .protosrc (outputs %v)", merge.Outputs)
	}

	ins := map[string]bool{}
	for _, in := range merge.flatInputs() {
		ins[in.string()] = true
	}

	hash := moddirHash(moduleDir)
	want := []string{
		// producer source/script closure flattened onto the merge node
		"$(S)/build/scripts/desc_rawproto_wrapper.py",
		"$(S)/" + srcDir + "/google/protobuf/type.proto",
		"$(S)/" + srcDir + "/google/protobuf/any.proto",
		// generated descriptor/rawproto inputs
		"$(B)/" + moduleDir + "/__/__/src/google/protobuf/type.proto.desc",
		"$(B)/" + srcDir + "/google/protobuf/type.proto." + hash + ".rawproto",
		// the merge node's own merge/collect scripts
		"$(S)/build/scripts/merge_files.py",
		"$(S)/build/scripts/collect_rawproto.py",
	}
	for _, w := range want {
		if !ins[w] {
			t.Errorf("merge node inputs missing %q\nhave: %v", w, merge.flatInputs())
		}
	}
}

// TestEmitDescProto_ProtoNamespaceNestedSourceDescOutputAndIncludes reproduces
// the T-39A residual: a PROTO_LIBRARY with PROTO_NAMESPACE(yt) whose .proto src
// lives in a subdirectory of the module must (a) write its descriptor under the
// declaring module root with the ymake `_/` output-name prefix
// ($(B)/<mod>/_/<sub>/x.proto.desc), and (b) render the descriptor protoc
// command's full _PROTO__INCLUDE span — the structural -I=$(B) -I=$(S)
// -I=$(S)/<ns> band plus every proto peer's namespace contribution (here the
// PEERDIR'd PROTO_NAMESPACE(yt) tail -I=$(S)/yt). Before this change the desc
// output omitted `_/` and the command omitted the whole middle band. The
// .rawproto output keeps its natural path (upstream `norel`) and md5 stem.
func TestEmitDescProto_ProtoNamespaceNestedSourceDescOutputAndIncludes(t *testing.T) {
	const clientMod = "yt/yt_proto/yt/client"
	const coreMod = "yt/yt_proto/yt/core"
	const descDir = "descns"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"

	files[coreMod+"/ya.make"] = "PROTO_LIBRARY()\nPROTO_NAMESPACE(yt)\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nSRCS(core.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[coreMod+"/core.proto"] = "syntax = \"proto3\";\npackage yt;\nmessage Core { int32 x = 1; }\n"

	files[clientMod+"/ya.make"] = "PROTO_LIBRARY()\nPROTO_NAMESPACE(yt)\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nPEERDIR(" + coreMod + ")\nSRCS(hive/proto/cluster.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[clientMod+"/hive/proto/cluster.proto"] = "syntax = \"proto3\";\npackage yt;\nmessage Cluster { int32 x = 1; }\n"

	files[descDir+"/ya.make"] = "PROTO_DESCRIPTIONS()\nPEERDIR(" + clientMod + ")\nEND()\n"

	g := testGen(newMemFS(files), descDir)

	hash := moddirHash(clientMod)
	descOut := "$(B)/" + clientMod + "/_/hive/proto/cluster.proto.desc"
	rawOut := "$(B)/" + clientMod + "/hive/proto/cluster.proto." + hash + ".rawproto"

	pd := mustNodeByAnyOutput(t, g, descOut)

	// The .rawproto keeps its natural (non-rebased) path and md5 stem.
	if mustNodeByAnyOutput(t, g, rawOut) != pd {
		t.Errorf("rawproto %q not produced by the same node as desc", rawOut)
	}
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if o.string() == "$(B)/"+clientMod+"/hive/proto/cluster.proto.desc" {
				t.Errorf("found non-rebased desc output %q (should be under _/)", o.string())
			}
		}
	}

	args := pd.Cmds[0].CmdArgs.flat()

	// Exact ordered _PROTO__INCLUDE span: own namespace, structural $(B)/$(S),
	// own cppOutRoot, the peer namespace tail (-I=$(S)/yt from yt/core), then the
	// trailing build-root + protobuf-src and --include_source_info.
	wantSpan := []string{
		"-I=./yt", "-I=$(S)/yt",
		"-I=$(B)", "-I=$(S)", "-I=$(S)/yt",
		"-I=$(S)/yt",
		"-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
		"--include_source_info",
	}
	flat := make([]string, len(args))
	for i, a := range args {
		flat[i] = a.string()
	}
	start := indexOfArg(args, "-I=./yt")
	if start < 0 {
		t.Fatalf("descriptor cmd has no -I=./yt: %v", flat)
	}
	got := flat[start:]
	if len(got) != len(wantSpan) {
		t.Fatalf("descriptor include span len = %d, want %d\n got=%v\nwant=%v", len(got), len(wantSpan), got, wantSpan)
	}
	for i := range wantSpan {
		if got[i] != wantSpan[i] {
			t.Fatalf("descriptor include span[%d] = %q, want %q\n got=%v\nwant=%v", i, got[i], wantSpan[i], got, wantSpan)
		}
	}
}

// TestEmitDescProto_ProtoNamespaceRootLevelSourceNoUnderscore pins the
// complement of the nested case: a root-level (flat) source under a
// PROTO_NAMESPACE(yt) module must NOT gain the `_/` output-name segment.
func TestEmitDescProto_ProtoNamespaceRootLevelSourceNoUnderscore(t *testing.T) {
	const ormMod = "yt/yt_proto/yt/orm/api"
	const descDir = "descroot"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"

	files[ormMod+"/ya.make"] = "PROTO_LIBRARY()\nPROTO_NAMESPACE(yt)\nDISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\nSRCS(access_control_service.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n"
	files[ormMod+"/access_control_service.proto"] = "syntax = \"proto3\";\npackage yt;\nmessage Access { int32 x = 1; }\n"

	files[descDir+"/ya.make"] = "PROTO_DESCRIPTIONS()\nPEERDIR(" + ormMod + ")\nEND()\n"

	g := testGen(newMemFS(files), descDir)

	descOut := "$(B)/" + ormMod + "/access_control_service.proto.desc"
	mustNodeByAnyOutput(t, g, descOut)

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.Contains(o.string(), ormMod+"/_/") {
				t.Errorf("root-level source gained an `_/` segment: %q", o.string())
			}
		}
	}
}
