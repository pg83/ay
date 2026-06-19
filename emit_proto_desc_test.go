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
