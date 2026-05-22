package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCollectModule_YqlAbiMacrosAppendCXXFlags(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `LIBRARY()
YQL_LAST_ABI_VERSION()
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	want := []string{
		"-DUSE_CURRENT_UDF_ABI_VERSION",
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	}
	if !reflect.DeepEqual(d.cxxFlags, want) {
		t.Fatalf("cxxFlags = %#v, want %#v", d.cxxFlags, want)
	}
}

func TestCollectModule_YqlUdfStaticRoutesSrcsToGlobal(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir mod: %v", err)
	}

	const yamake = `YQL_UDF_CONTRIB(my_udf)
SRCS(lib.cpp nested/extra.cpp)
PEERDIR(custom/peer)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "mod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "mod", Kind: KindLib, Platform: testTargetP}))

	if d.moduleStmt == nil || d.moduleStmt.Name != "YQL_UDF_CONTRIB" {
		t.Fatalf("moduleStmt = %#v, want YQL_UDF_CONTRIB", d.moduleStmt)
	}
	if !equalStrings(d.moduleStmt.Args, []string{"my_udf"}) {
		t.Fatalf("module args = %v, want [my_udf]", d.moduleStmt.Args)
	}
	if len(d.srcs) != 0 {
		t.Fatalf("srcs = %v, want empty (SRCS must alias to GLOBAL_SRCS)", d.srcs)
	}
	if !equalStrings(d.globalSrcs, []string{"lib.cpp", "nested/extra.cpp"}) {
		t.Fatalf("globalSrcs = %v, want [lib.cpp nested/extra.cpp]", d.globalSrcs)
	}
	if !equalStrings(d.peerdirs, []string{
		"yql/essentials/public/udf",
		"yql/essentials/public/udf/support",
		"custom/peer",
	}) {
		t.Fatalf("peerdirs = %v", d.peerdirs)
	}
}

func TestCollectModule_ProtocFatalWarningsAddsProtoFlag(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.protocFlags, []string{"--fatal_warnings"}) {
		t.Fatalf("protocFlags = %v, want [--fatal_warnings]", d.protocFlags)
	}
}

func TestCollectModule_CPPProtoPluginRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(validation ydb/public/lib/validation .validation.pb.h DEPS ydb/public/api/protos/annotations EXTRA_OUT_FLAG lite=true)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if len(d.cppProtoPlugins) != 1 {
		t.Fatalf("cppProtoPlugins = %d, want 1", len(d.cppProtoPlugins))
	}

	plugin := d.cppProtoPlugins[0]
	if plugin.Name != "validation" {
		t.Fatalf("plugin.Name = %q, want validation", plugin.Name)
	}
	if plugin.ToolPath != "ydb/public/lib/validation" {
		t.Fatalf("plugin.ToolPath = %q, want ydb/public/lib/validation", plugin.ToolPath)
	}
	if !equalStrings(plugin.OutputSuffixes, []string{".validation.pb.h"}) {
		t.Fatalf("plugin.OutputSuffixes = %v, want [.validation.pb.h]", plugin.OutputSuffixes)
	}
	if !equalStrings(plugin.Deps, []string{"ydb/public/api/protos/annotations"}) {
		t.Fatalf("plugin.Deps = %v, want [ydb/public/api/protos/annotations]", plugin.Deps)
	}
	if plugin.ExtraOutFlag != "lite=true" {
		t.Fatalf("plugin.ExtraOutFlag = %q, want lite=true", plugin.ExtraOutFlag)
	}
	if !containsString(d.peerdirs, "ydb/public/api/protos/annotations") {
		t.Fatalf("peerdirs = %v, want ydb/public/api/protos/annotations", d.peerdirs)
	}
}

func TestCollectModule_FlatcFlagsRecorded(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "flatcmod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir flatcmod: %v", err)
	}

	const yamake = `LIBRARY()
FLATC_FLAGS(--scoped-enums --gen-all)
SRCS(Schema.fbs)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "flatcmod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "flatcmod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.flatcFlags, []string{"--scoped-enums", "--gen-all"}) {
		t.Fatalf("flatcFlags = %v, want [--scoped-enums --gen-all]", d.flatcFlags)
	}
}

func TestCollectModule_UseCommonGoogleApisAddsPeer(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "proto")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir proto: %v", err)
	}

	const yamake = `PROTO_LIBRARY()
USE_COMMON_GOOGLE_APIS(api/annotations)
SRCS(test.proto)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "proto", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "proto", Kind: KindLib, Platform: testTargetP}))

	if !containsString(d.peerdirs, "contrib/libs/googleapis-common-protos") {
		t.Fatalf("peerdirs = %v, want contrib/libs/googleapis-common-protos", d.peerdirs)
	}
}

func TestCollectModule_Py3ProgramSplitsPyMainFromPySrcs(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "pytool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir pytool: %v", err)
	}

	const yamake = `PY3_PROGRAM()
PY_SRCS(
    MAIN
    __main__.py
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))

	bin := collectModule(fs, "pytool", KindBin, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindBin, Platform: testTargetP}))
	if got := bin.pyMain; got == nil || *got != "pytool.__main__:main" {
		t.Fatalf("bin pyMain = %#v, want pytool.__main__:main", got)
	}
	if len(bin.pySrcs) != 0 {
		t.Fatalf("bin pySrcs = %v, want empty", bin.pySrcs)
	}

	lib := collectModule(fs, "pytool", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "pytool", Kind: KindLib, Platform: testTargetP}))
	if lib.pyMain != nil {
		t.Fatalf("lib pyMain = %#v, want nil", lib.pyMain)
	}
	if !equalStrings(lib.pySrcs, []string{"__main__.py"}) {
		t.Fatalf("lib pySrcs = %v, want [__main__.py]", lib.pySrcs)
	}
}

func TestCollectModule_CopyExpandsVarsIntoAutoSources(t *testing.T) {
	root := t.TempDir()
	modDir := filepath.Join(root, "copymod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir copymod: %v", err)
	}

	const yamake = `LIBRARY()
SET(ORIG_SRC_DIR src)
SET(ORIG_SOURCES a.cpp b.h)
COPY(
    WITH_CONTEXT
    AUTO
    FROM ${ORIG_SRC_DIR}
    ${ORIG_SOURCES}
    OUTPUT_INCLUDES dep.h
)
END()
`
	if err := os.WriteFile(filepath.Join(modDir, "ya.make"), []byte(yamake), 0o644); err != nil {
		t.Fatalf("write ya.make: %v", err)
	}

	fs := NewFS(root)
	mf := Throw2(ParseFile(fs, filepath.Join(modDir, "ya.make")))
	d := collectModule(fs, "copymod", KindLib, mf.Stmts, buildIfEnv(ModuleInstance{Path: "copymod", Kind: KindLib, Platform: testTargetP}))

	if !equalStrings(d.srcs, []string{"a.cpp", "b.h"}) {
		t.Fatalf("srcs = %v, want [a.cpp b.h]", d.srcs)
	}
	if len(d.copyFiles) != 2 {
		t.Fatalf("len(copyFiles) = %d, want 2", len(d.copyFiles))
	}
	if d.copyFiles[0].Src != "src/a.cpp" || d.copyFiles[1].Src != "src/b.h" {
		t.Fatalf("copyFiles srcs = %#v", d.copyFiles)
	}
}

func TestGen_YqlUdfStatic_UsesGlobalArchiveOnly(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("udfmod/ya.make", `YQL_UDF_CONTRIB(my_udf)
YQL_ABI_VERSION(2 44 0)
SRCS(lib.cpp)
END()
`)
	mkdirWrite("udfmod/lib.cpp", "int udf() { return 0; }\n")
	mkdirWrite("yql/essentials/public/udf/ya.make", "LIBRARY()\nEND()\n")
	mkdirWrite("yql/essentials/public/udf/support/ya.make", "LIBRARY()\nEND()\n")

	g := testGen(root, "udfmod")

	cc := findGraphNodeByOutputs(t, g, "$(B)/udfmod/lib.cpp.udfs.o")
	if cc.TargetProperties["module_tag"] != "yql_udf_static" {
		t.Fatalf("cc module_tag = %q, want yql_udf_static", cc.TargetProperties["module_tag"])
	}

	for _, want := range []string{
		"-DUDF_ABI_VERSION_MAJOR=2",
		"-DUDF_ABI_VERSION_MINOR=44",
		"-DUDF_ABI_VERSION_PATCH=0",
	} {
		if !contains(cc.Cmds[0].CmdArgs, want) {
			t.Fatalf("cc cmd_args missing %q: %v", want, cc.Cmds[0].CmdArgs)
		}
	}

	globalAR := findGraphNodeByOutputs(t, g, "$(B)/udfmod/libmy_udf.global.a")
	if globalAR.TargetProperties["module_tag"] != "yql_udf_static_global" {
		t.Fatalf("global AR module_tag = %q, want yql_udf_static_global", globalAR.TargetProperties["module_tag"])
	}

	for _, n := range g.Graph {
		for _, out := range n.Outputs {
			if out.String() == "$(B)/udfmod/libmy_udf.a" {
				t.Fatalf("unexpected regular archive output %q present in graph", out)
			}
		}
	}
}

func TestGen_FlatcSourcesEmitConsumerInputsAndDeps(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
FLATC_FLAGS(--scoped-enums)
SRCS(
    File.fbs
    Schema.fbs
    consumer.cpp
)
END()
`)
	mkdirWrite("mod/consumer.cpp", `#include "File.fbs.h"
int consume() { return 0; }
`)
	mkdirWrite("mod/Schema.fbs", `namespace test;
table Foo {
  value:int;
}
`)
	mkdirWrite("mod/File.fbs", `include "Schema.fbs";
namespace test;
table Bar {
  foo:Foo;
}
root_type Bar;
`)
	mkdirWrite("build/scripts/cpp_flatc_wrapper.py", "print('stub')\n")
	mkdirWrite("contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h", "#pragma once\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/ya.make", "PROGRAM(flatc)\nSRCS(main.cpp)\nEND()\n")
	mkdirWrite("contrib/libs/flatbuffers/flatc/main.cpp", "int main() { return 0; }\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.h", "$(B)/mod/File.fbs.cpp", "$(B)/mod/File.bfbs")
	findGraphNodeByOutputs(t, g, "$(B)/mod/Schema.fbs.h", "$(B)/mod/Schema.fbs.cpp", "$(B)/mod/Schema.bfbs")

	fileCC := findGraphNodeByOutputs(t, g, "$(B)/mod/File.fbs.cpp.o")
	wantFileInputs := []string{
		"$(B)/mod/File.fbs.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(fileCC.Inputs); !reflect.DeepEqual(got[:len(wantFileInputs)], wantFileInputs) {
		t.Fatalf("File.fbs.cpp inputs prefix = %v, want %v", got[:len(wantFileInputs)], wantFileInputs)
	}
	if len(fileCC.Deps) != 2 {
		t.Fatalf("len(File.fbs.cpp deps) = %d, want 2 (self + imported schema)", len(fileCC.Deps))
	}

	consumerCC := findGraphNodeByOutputs(t, g, "$(B)/mod/consumer.cpp.o")
	wantConsumerInputs := []string{
		"$(S)/mod/consumer.cpp",
		"$(B)/mod/File.fbs.h",
		"$(B)/mod/Schema.fbs.h",
		"$(S)/build/scripts/cpp_flatc_wrapper.py",
		"$(S)/mod/File.fbs",
		"$(S)/mod/Schema.fbs",
		"$(S)/contrib/libs/flatbuffers/include/flatbuffers/flatbuffers.h",
	}
	if got := vfsStringsT3(consumerCC.Inputs); !reflect.DeepEqual(got[:len(wantConsumerInputs)], wantConsumerInputs) {
		t.Fatalf("consumer.cpp inputs prefix = %v, want %v", got[:len(wantConsumerInputs)], wantConsumerInputs)
	}
	if len(consumerCC.Deps) != 2 {
		t.Fatalf("len(consumer.cpp deps) = %d, want 2 (reachable flatc producers)", len(consumerCC.Deps))
	}
}

func TestGen_CopyFileWithContextAutoCompilesBuildOutput(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func TestGen_CopyFileWithContextExpandsBuildRootModdirDestination(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE_WITH_CONTEXT(
    AUTO
    original.cpp
    ${ARCADIA_BUILD_ROOT}/${MODDIR}/copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{
		"$(B)/mod/copied.cpp",
		"$(S)/mod/original.cpp",
		"$(S)/mod/dep.h",
	}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
}

func TestGen_CopyFileAutoDoesNotPropagateSourceContext(t *testing.T) {
	root := t.TempDir()

	mkdirWrite := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mkdirWrite("mod/ya.make", `LIBRARY()
COPY_FILE(
    AUTO
    original.cpp
    copied.cpp
)
END()
`)
	mkdirWrite("mod/original.cpp", `#include "dep.h"
int copied() { return 0; }
`)
	mkdirWrite("mod/dep.h", "#pragma once\n")

	g := testGen(root, "mod")

	findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp")
	cc := findGraphNodeByOutputs(t, g, "$(B)/mod/copied.cpp.o")
	wantInputs := []string{"$(B)/mod/copied.cpp"}
	if got := vfsStringsT3(cc.Inputs); !reflect.DeepEqual(got[:len(wantInputs)], wantInputs) {
		t.Fatalf("copied.cpp inputs prefix = %v, want %v", got[:len(wantInputs)], wantInputs)
	}
	for _, unexpected := range []string{"$(S)/mod/original.cpp", "$(S)/mod/dep.h"} {
		for _, in := range vfsStringsT3(cc.Inputs) {
			if in == unexpected {
				t.Fatalf("copied.cpp inputs unexpectedly contain %s: %v", unexpected, vfsStringsT3(cc.Inputs))
			}
		}
	}
	if len(cc.Deps) != 1 {
		t.Fatalf("len(copied.cpp deps) = %d, want 1 (copy producer)", len(cc.Deps))
	}
}

func vfsStringsT3(in []VFS) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}
