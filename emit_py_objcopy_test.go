package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestGen_ResourceRelativeOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        data.json
    OUT_NOAUTO data.json
)
RESOURCE(
    data.json /data.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/db/data.json") {
		t.Fatalf("objcopy inputs still use source-root data.json: %#v", objcopy.flatInputs())
	}
}

// A FROM_SANDBOX output embedded by a RESOURCE via a module-dir root-relative
// path: the objcopy reads from $(B) and depends on the SB fetch node, never a
// $(S) path nor the module-dir-doubled $(B) path. The resfs key is unchanged.
func TestGen_ResourceRootRelativeFromSandboxOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "yt/bundle/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
FROM_SANDBOX(
    FILE 2531143113
    OUT_NOAUTO llvm-symbolizer
)
RESOURCE(
    yt/bundle/llvm-symbolizer /ytprof/llvm-symbolizer
)
END()
`)

	g := testGen(newMemFS(files), "yt/bundle")

	sb := mustNodeByOutput(t, g, "$(B)/yt/bundle/llvm-symbolizer")
	if sb.KV.P != pkSB {
		t.Fatalf("llvm-symbolizer producer kind = %q, want SB", sb.KV.P.string())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/yt/bundle/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing yt/bundle objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs missing build-root fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs use the source path for a fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(B)/yt/bundle/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs double the module dir: %#v", objcopy.flatInputs())
	}

	if !slices.Contains(graphDeps(g, objcopy), sb.UID) {
		t.Fatalf("objcopy deps missing SB fetch uid %q: %v", sb.UID, graphDeps(g, objcopy))
	}

	wantKey := encb64.StdEncoding.EncodeToString([]byte("/ytprof/llvm-symbolizer"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 RESOURCE key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}
}

// A RESOURCE() in a PROTO_LIBRARY body belongs to the C++ _CPP_PROTO submodule:
// its MODULE_TAG folds into the objcopy output-name hash and stamps the node's
// module_tag (cpp_proto); the submodule's .global.a carries cpp_proto_global.
func TestGen_ProtoLibraryResourceObjcopyAndGlobalUseCppProtoTag(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "px/ya.make",
		"PROTO_LIBRARY()\nSRCS(foo.proto)\nRESOURCE(px/tree.pb.txt px/tree.pb.txt)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO PY_PROTO PY3_PROTO)\nEND()\n")
	writeTestModuleFile(files, "px/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")
	writeTestModuleFile(files, "px/tree.pb.txt", "stub\n")

	g := testGen(newMemFS(files), "px")

	objcopy := findNodeByOutputPrefix(g, "$(B)/px/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing px objcopy output")
	}
	if got := objcopy.TargetProperties.ModuleTag.string(); got != "cpp_proto" {
		t.Fatalf("objcopy module_tag = %q, want cpp_proto", got)
	}

	global := mustNodeByOutputSuffix(t, g, "/libpx.global.a")
	if got := global.TargetProperties.ModuleTag.string(); got != "cpp_proto_global" {
		t.Fatalf("global archive module_tag = %q, want cpp_proto_global", got)
	}
}

// A RESOURCE_FILES path may name an ordinary source file at the root OUTSIDE the
// consuming module. Both its payload and resfs/src kv members must bind to the
// same root $(S)/<path> (not a module-dir-joined phantom), with no producer dep.
func TestGen_ResourceFilesRootRelativeSourceFromOtherModule(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["modadvert/dyn_disclaimers/disclaimers_config.pb.txt"] = "config\n"

	writeTestModuleFile(files, "yabs/server/libs/banner_flags/ya.make", `PY3_LIBRARY()
PEERDIR(library/python/resource)
RESOURCE_FILES(
    modadvert/dyn_disclaimers/disclaimers_config.pb.txt
)
END()
`)

	g := testGen(newMemFS(files), "yabs/server/libs/banner_flags")

	objcopy := findNodeByOutputPrefix(g, "$(B)/yabs/server/libs/banner_flags/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing banner_flags objcopy output")
	}

	const rootSrc = "$(S)/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	const phantom = "$(S)/yabs/server/libs/banner_flags/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"

	if !nodeHasInput(objcopy, rootSrc) {
		t.Fatalf("objcopy inputs missing root source %q: %#v", rootSrc, objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, phantom) {
		t.Fatalf("objcopy inputs carry the module-prefixed phantom %q: %#v", phantom, objcopy.flatInputs())
	}

	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 resfs/file key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}

	wantKv := "resfs/src/resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt=modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	phantomKv := "resfs/src/resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt=yabs/server/libs/banner_flags/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"
	args := prCmdArgStrings(objcopy)
	if !slices.Contains(args, wantKv) {
		t.Fatalf("objcopy --kvs missing root-relative resfs/src %q: %v", wantKv, args)
	}
	if slices.Contains(args, phantomKv) {
		t.Fatalf("objcopy --kvs carries module-prefixed phantom resfs/src %q: %v", phantomKv, args)
	}
}

// A PY_SRCS source staged by COPY_FILE is a packaging stage: the copy producer's
// transitive $(S) closure (original source + copy tooling) rides every consumer.
// The py3cc bytecode node carries all three; the resource-objcopy carries the
// original source but not the scripts (normalized away ref-side).
func TestGen_CopyFileStagedPySrcCarriesOriginalSourceClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "build/scripts/fs_tools.py", "import process_command_files\n")
	writeTestModuleFile(files, "build/scripts/process_command_files.py", "pass\n")

	writeTestModuleFile(files, "pkg/keys.py", "KEY = 1\n")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
COPY_FILE(pkg/keys.py keys.py)
PY_SRCS(keys.py)
END()
`)

	g := testGen(newMemFS(files), "mod")

	const origSrc = "$(S)/pkg/keys.py"
	const stagedCopy = "$(B)/mod/keys.py"
	const fsTools = "$(S)/build/scripts/fs_tools.py"
	const pcf = "$(S)/build/scripts/process_command_files.py"

	// (1) bytecode node carries the copy producer's $(S) closure.
	bc := mustNodeByOutput(t, g, "$(B)/mod/keys.py.yapyc3")
	if !nodeHasInput(bc, stagedCopy) {
		t.Fatalf("bytecode inputs missing staged copy %q: %#v", stagedCopy, bc.flatInputs())
	}
	if !nodeHasInput(bc, origSrc) {
		t.Fatalf("bytecode inputs missing original source %q: %#v", origSrc, bc.flatInputs())
	}
	if !nodeHasInput(bc, fsTools) {
		t.Fatalf("bytecode inputs missing copy tooling %q: %#v", fsTools, bc.flatInputs())
	}
	if !nodeHasInput(bc, pcf) {
		t.Fatalf("bytecode inputs missing copy tooling %q: %#v", pcf, bc.flatInputs())
	}

	// (2) objcopy names the original source, not just the staged copy, and does
	// NOT over-emit the copy scripts.
	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}
	if !nodeHasInput(objcopy, origSrc) {
		t.Fatalf("objcopy inputs missing original source %q: %#v", origSrc, objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, fsTools) || nodeHasInput(objcopy, pcf) {
		t.Fatalf("objcopy over-emits copy tooling scripts: %#v", objcopy.flatInputs())
	}
}

// A COPY_FILE whose source is a $(B) generated output is NOT a source-root
// packaging stage: no $(S) closure to lift. The objcopy and bytecode node ride
// only the staged $(B) copy, never the original $(B) producer-location path.
func TestGen_CopyFileStagedBuildRootSrcCarriesNoOriginalClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "build/scripts/fs_tools.py", "import process_command_files\n")
	writeTestModuleFile(files, "build/scripts/process_command_files.py", "pass\n")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
COPY_FILE(${ARCADIA_BUILD_ROOT}/gen/gen.py gen.py)
PY_SRCS(gen.py)
END()
`)

	g := testGen(newMemFS(files), "mod")

	const origBuildSrc = "$(B)/gen/gen.py"
	const stagedCopy = "$(B)/mod/gen.py"

	bc := mustNodeByOutput(t, g, "$(B)/mod/gen.py.yapyc3")
	if !nodeHasInput(bc, stagedCopy) {
		t.Fatalf("bytecode inputs missing staged copy %q: %#v", stagedCopy, bc.flatInputs())
	}
	if nodeHasInput(bc, origBuildSrc) {
		t.Fatalf("bytecode leaks build-root original %q: %#v", origBuildSrc, bc.flatInputs())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}
	if nodeHasInput(objcopy, origBuildSrc) {
		t.Fatalf("objcopy leaks build-root original %q: %#v", origBuildSrc, objcopy.flatInputs())
	}
}

// A RESOURCE embedding a RUN_PROGRAM build-root output carries that producer's
// build-root OUTPUT_INCLUDES closure onto the objcopy node (OUTPUT_INCLUDES
// attaches to every output of the run). Normalization keeps only the $(B) half,
// so the generated .pb.h rides (with a producer dep); resfs key and hash unchanged.
func TestGen_ResourceBindirOutputCarriesProducerBuildClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/gen/bin", "gen")
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "p/ya.make", "PROTO_LIBRARY()\nSRCS(dep.proto)\nEND()\n")
	writeTestModuleFile(files, "p/dep.proto", "syntax = \"proto3\";\nmessage Dep {}\n")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(p)
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/payload.pb
    OUTPUT_INCLUDES
        p/dep.pb.h
    OUT ${BINDIR}/payload.pb
)
RESOURCE(
    ${BINDIR}/payload.pb payload.pb
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/db/payload.pb") {
		t.Fatalf("objcopy inputs missing build-root payload.pb: %#v", objcopy.flatInputs())
	}

	// (2) the producer's build-root OUTPUT_INCLUDES closure rides the objcopy.
	const depPbH = "$(B)/p/dep.pb.h"
	if !nodeHasInput(objcopy, depPbH) {
		t.Fatalf("objcopy inputs missing producer build-root closure %q: %#v", depPbH, objcopy.flatInputs())
	}

	// (3) the source .proto leaf is NOT carried.
	if nodeHasInput(objcopy, "$(S)/p/dep.proto") {
		t.Fatalf("objcopy must not carry source-tree .proto leaf: %#v", objcopy.flatInputs())
	}

	// (4) the objcopy depends on dep.pb.h's producer.
	depProducer := mustNodeByOutput(t, g, depPbH)
	foundDep := false
	for _, d := range graphDeps(g, objcopy) {
		if d == depProducer.UID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Fatalf("objcopy deps %v do not include dep.pb.h producer uid %q", graphDeps(g, objcopy), depProducer.UID)
	}

	// (5) the closure is a node input only — never in the command nor the hash.
	args := prCmdArgStrings(objcopy)
	for _, a := range args {
		if strings.Contains(a, "dep.pb.h") {
			t.Fatalf("objcopy command must not name the closure header: %v", args)
		}
	}
	wantHashInputs := []string{
		"${BINDIR}/payload.pb",
		encb64.StdEncoding.EncodeToString([]byte("payload.pb")),
		"$S/db",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/db/objcopy_" + wantHash + ".o"
	if got := objcopy.Outputs[0].string(); got != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (hash must ignore the closure)", got, wantOutput)
	}
}

func TestGen_ResourceBindirOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/json_gen/bin", "json_gen")

	writeTestModuleFile(files, "db/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/json_gen/bin
        --output
        ${BINDIR}/data.json
    OUT_NOAUTO ${BINDIR}/data.json
)
RESOURCE(
    ${BINDIR}/data.json /data.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}
	if !nodeHasInput(objcopy, "$(B)/db/data.json") {
		t.Fatalf("objcopy inputs missing build-root data.json: %#v", objcopy.flatInputs())
	}
	for _, in := range objcopy.flatInputs() {
		if strings.Contains(in.string(), "${BINDIR}") {
			t.Fatalf("objcopy inputs still leak ${BINDIR}: %#v", objcopy.flatInputs())
		}
	}

	// The hash is over the RAW RESOURCE() Path ('${BINDIR}/data.json'), not the
	// expanded form; pre-expanding here would drift the objcopy_<hash> filename.
	wantHashInputs := []string{
		"${BINDIR}/data.json",
		// base64 of "/data.json"; RESOURCE() Key is literal, not resfs/file-prefixed.
		"L2RhdGEuanNvbg==",
		"$S/db",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/db/objcopy_" + wantHash + ".o"
	gotOutput := objcopy.Outputs[0].string()
	if gotOutput != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (REF hashes RESOURCE Path RAW)", gotOutput, wantOutput)
	}
}

// A RUN_PROGRAM STDOUT(_NOAUTO) output embedded via RESOURCE_FILES is a build
// artifact: both its payload and resfs/src kv members must resolve to $(B) with
// the producer dependency, never a phantom $(S)/db/common.json.
func TestGen_ResourceFilesStdoutOutputFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "db/dumper", "dumper")

	writeTestModuleFile(files, "db/ya.make", `PY3_LIBRARY()
PEERDIR(library/python/resource)
RUN_PROGRAM(
    db/dumper
    CWD ${ARCADIA_BUILD_ROOT}
    STDOUT_NOAUTO common.json
)
RESOURCE_FILES(
    PREFIX feature_store/catalog/
    common.json
)
END()
`)

	g := testGen(newMemFS(files), "db")

	objcopy := findNodeByOutputPrefix(g, "$(B)/db/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing db objcopy output")
	}

	// (1) both payload and resfs/src kv point at $(B), no phantom $(S).
	if !nodeHasInput(objcopy, "$(B)/db/common.json") {
		t.Fatalf("objcopy inputs missing build-root common.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/db/common.json") {
		t.Fatalf("objcopy inputs still carry phantom source-root common.json: %#v", objcopy.flatInputs())
	}

	// (2) the objcopy depends on the STDOUT_NOAUTO producer of common.json.
	producer := findNodeByOutputPrefix(g, "$(B)/db/common.json")
	if producer == nil {
		t.Fatal("graph is missing the RUN_PROGRAM producer of common.json")
	}
	foundDep := false
	for _, d := range graphDeps(g, objcopy) {
		if d == producer.UID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Fatalf("objcopy deps %v do not include the common.json producer uid %q", graphDeps(g, objcopy), producer.UID)
	}

	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/feature_store/catalog/common.json"))
	foundKey := false
	for _, c := range objcopy.Cmds {
		for _, a := range c.CmdArgs.flat() {
			if a.string() == wantKey {
				foundKey = true
			}
		}
	}
	if !foundKey {
		t.Fatalf("objcopy command missing base64 resource key %q", wantKey)
	}
}

// ALL_RESOURCE_FILES(Ext PREFIX p Dirs...) globs <dir>/*.<ext> and feeds the
// matches to RESOURCE_FILES. The globbed paths keep the literal ${ARCADIA_ROOT}
// marker, so the resfs/file key embeds it verbatim. The emitted objcopy matches
// the equivalent explicit RESOURCE_FILES, .json-only and sorted.
func TestGen_AllResourceFilesGlobMatchesResourceFiles(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/ignore.txt"] = "not a resource\n"

	// STRIP=${ARCADIA_ROOT}/<moddir>/ is not a prefix here, so the key retains the
	// literal ${ARCADIA_ROOT} marker.
	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"
	sorted := []string{"a.json", "b.json"} // glob is .json-only and sorted

	var hashPaths, keysB64, kvsHash []string
	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	// The objcopy filename is hashed over the UNRENDERED key (literal marker).
	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, nil)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)
	if objcopy == nil {
		t.Fatalf("graph is missing the ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/cfg/a.json") || !nodeHasInput(objcopy, "$(S)/mod/cfg/b.json") {
		t.Fatalf("objcopy inputs missing the globbed json sources: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/cfg/ignore.txt") {
		t.Fatalf("objcopy picked up the non-json file ignore.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)
	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))
		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 marker key for %q: %v", f, args)
		}
		// --kvs: the marker is rendered to $(S), the rootrel resolved.
		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/mod/cfg/" + f + "=mod/cfg/" + f
		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
		if slices.Contains(args, "resfs/src/resfs/file/"+prefix+"${ARCADIA_ROOT}/mod/cfg/"+f+"=mod/cfg/"+f) {
			t.Fatalf("objcopy --kvs leaked the literal ${ARCADIA_ROOT} marker for %q: %v", f, args)
		}
	}

	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")
	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}
	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}

// ALL_RESOURCE_FILES(Ext Dirs...) with a RELATIVE DIR globs against the module
// dir. Here the STRIP=${ARCADIA_ROOT}/<moddir>/ default IS a prefix, so the key
// becomes the moddir-relative <dir>/<file> — no literal marker survives.
func TestGen_AllResourceFilesGlobRelativeDir(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/app/templates/x.j2"] = "x\n"
	files["mod/app/templates/y.j2"] = "y\n"
	files["mod/app/templates/skip.txt"] = "not a resource\n"

	writeTestModuleFile(files, "mod/app/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(j2 templates)
END()
`)

	g := testGen(newMemFS(files), "mod/app")

	const moddir = "mod/app"
	sorted := []string{"x.j2", "y.j2"} // glob is .j2-only and sorted

	var hashPaths, keysB64, kvsHash []string
	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/app/templates/" + f
		fileKey := "resfs/file/templates/" + f
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, nil)
	wantOutput := "$(B)/mod/app/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)
	if objcopy == nil {
		t.Fatalf("graph is missing the relative-DIR ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/app/templates/x.j2") || !nodeHasInput(objcopy, "$(S)/mod/app/templates/y.j2") {
		t.Fatalf("objcopy inputs missing the globbed j2 sources: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/app/templates/skip.txt") {
		t.Fatalf("objcopy picked up the non-j2 file skip.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)
	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/templates/" + f))
		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 key for %q: %v", f, args)
		}
		wantKv := "resfs/src/resfs/file/templates/" + f + "=mod/app/templates/" + f
		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}
}

// ALL_RESOURCE_FILES_FROM_DIRS with a relative DIR carrying `..` segments: the
// `..` is reconstructed and the key is PREFIX-joined to the ${ARCADIA_ROOT}-rooted
// match (STRIP at the module dir does not cover the parent-relative config dir).
func TestGen_AllResourceFilesFromDirsRelativeParentDir(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["base/configs/p/a.cfg"] = "a\n"
	files["base/configs/p/b.cfg"] = "b\n"

	writeTestModuleFile(files, "base/tools/sync/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES_FROM_DIRS(PREFIX adminka ../../configs/p)
END()
`)

	g := testGen(newMemFS(files), "base/tools/sync")

	const moddir = "base/tools/sync"
	const prefix = "adminka"
	sorted := []string{"a.cfg", "b.cfg"} // FROM_DIRS globs all files, sorted

	var hashPaths, keysB64, kvsHash []string
	for _, f := range sorted {
		// ../../configs/p resolved against base/tools/sync cleans to base/configs/p.
		path := "${ARCADIA_ROOT}/base/configs/p/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, nil)
	wantOutput := "$(B)/base/tools/sync/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)
	if objcopy == nil {
		t.Fatalf("graph is missing the FROM_DIRS `..` objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/base/configs/p/a.cfg") || !nodeHasInput(objcopy, "$(S)/base/configs/p/b.cfg") {
		t.Fatalf("objcopy inputs missing the `..`-resolved config sources: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)
	for _, f := range sorted {
		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/base/configs/p/" + f + "=base/configs/p/" + f
		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}
}

// ALL_RESOURCE_FILES with a SOURCE-ROOTED DIR carrying a trailing slash: the
// empty trailing segment is dropped, so each match reconstructs to a normalized
// path with no double slash. The resource path, hash and keys carry no `//`.
func TestGen_AllResourceFilesGlobSourceRootedTrailingSlash(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"

	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg/
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"
	sorted := []string{"a.json", "b.json"}

	var hashPaths, keysB64, kvsHash []string
	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, nil)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)
	if objcopy == nil {
		t.Fatalf("graph is missing the trailing-slash ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	args := prCmdArgStrings(objcopy)
	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))
		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing normalized base64 marker key for %q: %v", f, args)
		}
		// No `//` in any rendered kvs argument.
		for _, a := range args {
			if strings.Contains(a, "mod/cfg//") {
				t.Fatalf("objcopy arg carries a double slash from the trailing-slash DIR: %q", a)
			}
		}
	}
}

// ALL_RESOURCE_FILES with a DIR token ending in `*`: the per-DIR glob pattern is
// `dir/*/*.json`, an interior `*` expanding to every immediate subdir then `.json`
// inside each. The depth-2 matches feed the objcopy like explicit RESOURCE_FILES;
// a depth-1 `dir/top.json` is NOT matched, and non-json files are excluded.
func TestGen_AllResourceFilesGlobDirWildcard(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/sub1/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/sub2/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/sub1/skip.txt"] = "not a resource\n"
	files["mod/cfg/top.json"] = "{\"top\":0}\n" // depth-1: NOT matched by dir/*/*.json

	writeTestModuleFile(files, "mod/libs/cpp/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ALL_RESOURCE_FILES(
    json
    PREFIX cfg
    ${ARCADIA_ROOT}/mod/cfg/*
)
END()
`)

	g := testGen(newMemFS(files), "mod/libs/cpp")

	const moddir = "mod/libs/cpp"
	const prefix = "cfg"
	// Traversal order: sorted subdirs (sub1, sub2), sorted files within each.
	sorted := []string{"sub1/a.json", "sub2/b.json"}

	var hashPaths, keysB64, kvsHash []string
	for _, f := range sorted {
		path := "${ARCADIA_ROOT}/mod/cfg/" + f
		fileKey := "resfs/file/" + prefix + path
		hashPaths = append(hashPaths, path)
		keysB64 = append(keysB64, encb64.StdEncoding.EncodeToString([]byte(fileKey)))
		kvsHash = append(kvsHash, "resfs/src/"+fileKey+"=${rootrel;context=TEXT;input=TEXT:\""+path+"\"}")
	}

	wantHash := objcopyHash(hashPaths, keysB64, kvsHash, moddir, nil)
	wantOutput := "$(B)/mod/libs/cpp/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantOutput)
	if objcopy == nil {
		t.Fatalf("graph is missing the dir/* ALL_RESOURCE_FILES objcopy output %q\nobjcopy nodes: %v", wantOutput, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/mod/cfg/sub1/a.json") || !nodeHasInput(objcopy, "$(S)/mod/cfg/sub2/b.json") {
		t.Fatalf("objcopy inputs missing the depth-2 globbed json sources: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/cfg/top.json") {
		t.Fatalf("dir/*/*.json matched a depth-1 file top.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/cfg/sub1/skip.txt") {
		t.Fatalf("objcopy picked up the non-json file skip.txt: %#v", objcopy.flatInputs())
	}

	args := prCmdArgStrings(objcopy)
	for _, f := range sorted {
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))
		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 marker key for %q: %v", f, args)
		}
		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/mod/cfg/" + f + "=mod/cfg/" + f
		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
	}

	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")
	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}
	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the dir/* resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}

// A build-generated PY_SRCS source (the OUT_NOAUTO of a RUN_PROGRAM) is packaged
// like a checked-in py: an objcopy resfs node embeds the generated .py and its
// .yapyc3 from $(B), with deps on both producers. It is NOT routed through the
// rescompiler _raw.auxcpp path, and is EXCLUDED from the py/namespace resource
// (extended source search needs an arc source, which a $(B) file is not).
func TestGen_GeneratedPySrcsResourcedAsObjcopyNotRawAux(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "mod/gen/bin", "gen")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(__init__.py)
RUN_PROGRAM(
    mod/gen/bin
        --save_file_path __init__.py
    OUT_NOAUTO __init__.py
)
END()
`)

	g := testGen(newMemFS(files), "mod")

	// The hash is over the same path/key/kvHash strings as a checked-in py.
	pyKey := "resfs/file/py/mod/__init__.py"
	ypKey := "resfs/file/py/mod/__init__.py.yapyc3"
	paths := []string{"__init__.py", "__init__.py.yapyc3"}
	keysB64 := []string{
		encb64.StdEncoding.EncodeToString([]byte(pyKey)),
		encb64.StdEncoding.EncodeToString([]byte(ypKey)),
	}
	kvsHash := []string{
		"resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"__init__.py\"}",
		"resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"__init__.py.yapyc3\"}",
	}
	wantHash := objcopyHash(paths, keysB64, kvsHash, "mod", stringPtr("PY3"))
	wantObjcopy := "$(B)/mod/objcopy_" + wantHash + ".o"

	objcopy := nodeByOutput(g, wantObjcopy)
	if objcopy == nil {
		t.Fatalf("graph is missing generated-py objcopy output %q\nobjcopy nodes: %v", wantObjcopy, objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(B)/mod/__init__.py") {
		t.Fatalf("objcopy inputs missing build-root generated source: %#v", objcopy.flatInputs())
	}
	if !nodeHasInput(objcopy, "$(B)/mod/__init__.py.yapyc3") {
		t.Fatalf("objcopy inputs missing build-root bytecode: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/__init__.py") {
		t.Fatalf("objcopy inputs use a source path for the generated py: %#v", objcopy.flatInputs())
	}

	// The objcopy depends on both producers: the RUN_PROGRAM and the py3cc.
	producer := mustNodeByOutput(t, g, "$(B)/mod/__init__.py")
	bytecode := mustNodeByOutput(t, g, "$(B)/mod/__init__.py.yapyc3")
	deps := graphDeps(g, objcopy)
	if !slices.Contains(deps, producer.UID) {
		t.Fatalf("objcopy deps %v missing RUN_PROGRAM producer uid %q", deps, producer.UID)
	}
	if !slices.Contains(deps, bytecode.UID) {
		t.Fatalf("objcopy deps %v missing py3cc bytecode producer uid %q", deps, bytecode.UID)
	}

	// No rescompiler _raw.auxcpp path for a generated PY_SRCS source.
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}
		o := n.Outputs[0].string()
		if strings.HasPrefix(o, "$(B)/mod/") && strings.Contains(o, "_raw.auxcpp") {
			t.Fatalf("generated PY_SRCS produced a rescompiler aux node %q; want objcopy resfs", o)
		}
	}

	// No py/namespace resource: the only PY_SRCS is generated.
	for _, n := range g.Graph {
		for _, a := range prCmdArgStrings(n) {
			if strings.Contains(a, "py/namespace") && strings.Contains(a, "/mod=") {
				t.Fatalf("generated-only PY_SRCS emitted a namespace resource: %q", a)
			}
		}
	}

	globalAr := mustNodeByOutput(t, g, "$(B)/mod/libpy3mod.global.a")
	if !slices.Contains(prCmdArgStrings(globalAr), wantObjcopy) {
		t.Fatalf("global archive does not link the generated-py objcopy %q: %v", wantObjcopy, prCmdArgStrings(globalAr))
	}
}

// Guard: an ordinary checked-in PY_SRCS module is unaffected. Its objcopy embeds
// the .py from $(S), and it still emits a py/namespace resource.
func TestGen_CheckedInPySrcsObjcopyUnaffected(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "modc/foo.py", "x = 1\n")
	writeTestModuleFile(files, "modc/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(foo.py)
END()
`)

	g := testGen(newMemFS(files), "modc")

	pyKey := "resfs/file/py/modc/foo.py"
	ypKey := "resfs/file/py/modc/foo.py.yapyc3"
	keysB64 := []string{
		encb64.StdEncoding.EncodeToString([]byte(pyKey)),
		encb64.StdEncoding.EncodeToString([]byte(ypKey)),
	}
	kvsHash := []string{
		"resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"foo.py\"}",
		"resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"foo.py.yapyc3\"}",
	}
	wantHash := objcopyHash([]string{"foo.py", "foo.py.yapyc3"}, keysB64, kvsHash, "modc", stringPtr("PY3"))
	objcopy := nodeByOutput(g, "$(B)/modc/objcopy_"+wantHash+".o")
	if objcopy == nil {
		t.Fatalf("graph is missing checked-in py objcopy: %v", objcopyOutputs(g))
	}

	if !nodeHasInput(objcopy, "$(S)/modc/foo.py") {
		t.Fatalf("checked-in objcopy inputs missing source foo.py: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(B)/modc/foo.py") {
		t.Fatalf("checked-in objcopy inputs use a build path for a source py: %#v", objcopy.flatInputs())
	}

	sawNamespace := false
	for _, n := range g.Graph {
		for _, a := range prCmdArgStrings(n) {
			if strings.Contains(a, "py/namespace") && strings.Contains(a, "/modc=") {
				sawNamespace = true
			}
		}
	}
	if !sawNamespace {
		t.Fatal("checked-in PY_SRCS did not emit the expected py/namespace resource")
	}
}

// A checked-in `.py` passed to PY_SRCS as a fully-root-relative token keys its
// py/namespace resource at the empty namespace root (`py/namespace/<md5>/=<value>`),
// since mod_root_path = rootrel[:-(len(token)+1)] is empty. The VALUE stays
// module-derived.
func TestGen_RootRelativePySrcsNamespaceKeyedAtRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "other/place/thing.py", "x = 1\n")
	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(other/place/thing.py)
END()
`)

	g := testGen(newMemFS(files), "mod")

	h := md5.New()
	h.Write([]byte("mod.other.place.thing"))
	nsMD5 := enchex.EncodeToString(h.Sum(nil))

	wantKeyRoot := "py/namespace/" + nsMD5 + "/"
	kvHash := wantKeyRoot + "=\"mod.\""
	kvCmd := wantKeyRoot + "=mod."

	wantHash := objcopyHash(nil, nil, []string{kvHash}, "mod", stringPtr("PY3"))
	wantObjcopy := "$(B)/mod/objcopy_" + wantHash + ".o"

	// The namespace must key at the root, not the module dir.
	sawRoot := false
	for _, n := range g.Graph {
		for _, a := range prCmdArgStrings(n) {
			if a == kvCmd {
				sawRoot = true
			}
			if strings.HasPrefix(a, "py/namespace/") && strings.Contains(a, "/mod=") {
				t.Fatalf("root-relative PY_SRCS keyed namespace at the module dir: %q", a)
			}
		}
	}
	if !sawRoot {
		t.Fatalf("missing root-keyed namespace kv %q\nobjcopy nodes: %v", kvCmd, objcopyOutputs(g))
	}

	objcopy := nodeByOutput(g, wantObjcopy)
	if objcopy == nil {
		t.Fatalf("graph is missing root-keyed namespace objcopy %q\nobjcopy nodes: %v", wantObjcopy, objcopyOutputs(g))
	}

	globalAr := mustNodeByOutput(t, g, "$(B)/mod/libpy3mod.global.a")
	if !slices.Contains(prCmdArgStrings(globalAr), wantObjcopy) {
		t.Fatalf("global archive does not link the root-keyed namespace objcopy %q: %v", wantObjcopy, prCmdArgStrings(globalAr))
	}
}

// A swig-injected PY_SRCS source arrives as a full ${ARCADIA_BUILD_ROOT}/<full>.py
// token and stays on the rescompiler _raw.auxcpp path — NOT re-routed to objcopy
// resfs (unlike the bare-token RUN_PROGRAM case above).
func TestGen_SwigGeneratedPyStaysOnRawAuxNotObjcopy(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "contrib/tools/swig", "swig")

	writeTestModuleFile(files, "swigmod/_libfoo.swg", "%module libfoo\n")
	writeTestModuleFile(files, "swigmod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(
    SWIG_C
    TOP_LEVEL
    _libfoo.swg
)
END()
`)

	g := testGen(newMemFS(files), "swigmod")

	sawAux := false
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}
		o := n.Outputs[0].string()
		if strings.HasPrefix(o, "$(B)/swigmod/") && strings.HasSuffix(o, "_raw.auxcpp") {
			sawAux = true
		}
		// No objcopy resfs node for the swig py.
		if strings.HasPrefix(o, "$(B)/swigmod/objcopy_") {
			t.Fatalf("swig generated py was routed to objcopy resfs %q; want _raw.auxcpp", o)
		}
	}
	if !sawAux {
		t.Fatal("swig generated py did not produce the expected _raw.auxcpp resource node")
	}
}

// A PY3_PROGRAM is a multimodule: the PROGRAM half ignores RESOURCE, so a
// RESOURCE() objcopy is owned by the LIBRARY twin (PY3_BIN_LIB) and stamped
// module_tag=py3_bin_lib. Only the program-side PY_MAIN carries module_tag=py3_bin.
func TestGen_Py3ProgramResourceObjcopyUsesLibTagPyMainKeepsBinTag(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":             "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/runtime_py3/main/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/cpp/malloc/jemalloc/ya.make":     "LIBRARY()\nSRCS(je.cpp)\nEND()\n",
		"library/cpp/malloc/jemalloc/je.cpp":      "int je(){return 0;}\n",
		"library/cpp/malloc/api/ya.make":          "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/api/api.cpp":          "int api(){return 0;}\n",
		"contrib/libs/jemalloc/ya.make":           "LIBRARY()\nSRCS(c.cpp)\nEND()\n",
		"contrib/libs/jemalloc/c.cpp":             "int c(){return 0;}\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "library/python/import_tracing/constructor/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")
	writeTestModuleFile(files, "library/python/testing/import_test/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "app/data.txt", "stub\n")
	writeTestModuleFile(files, "app/ya.make", `PY3_PROGRAM()
DISABLE(PYTHON_SQLITE3)
ENABLE(PYBUILD_NO_PYC)
RESOURCE(
    data.txt app/data.txt
)
PY_MAIN(app:main)
END()
`)

	g := testGen(newMemFS(files), "app")

	var resObjcopy, mainObjcopy *Node
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 || !strings.Contains(n.Outputs[0].string(), "/objcopy_") {
			continue
		}
		args := prCmdArgStrings(n)
		switch {
		case slices.Contains(args, "PY_MAIN=app:main"):
			mainObjcopy = n
		case slices.Contains(args, encb64.StdEncoding.EncodeToString([]byte("app/data.txt"))):
			resObjcopy = n
		}
	}

	if resObjcopy == nil {
		t.Fatalf("graph is missing the RESOURCE objcopy: %v", objcopyOutputs(g))
	}
	if mainObjcopy == nil {
		t.Fatalf("graph is missing the PY_MAIN objcopy: %v", objcopyOutputs(g))
	}

	if got := resObjcopy.TargetProperties.ModuleTag.string(); got != "py3_bin_lib" {
		t.Fatalf("RESOURCE objcopy module_tag = %q, want py3_bin_lib", got)
	}
	if got := mainObjcopy.TargetProperties.ModuleTag.string(); got != "py3_bin" {
		t.Fatalf("PY_MAIN objcopy module_tag = %q, want py3_bin", got)
	}
}

// A PY3_PROGRAM's PROGRAM half ignores RESOURCE: the RESOURCE objcopy is owned by
// the PY3_BIN_LIB twin and reaches the program ONLY through that twin's global
// archive, never as a direct LD member — whereas the PY_MAIN objcopy is a genuine
// direct LD member.
func TestGen_Py3ProgramLDDoesNotDirectlyLinkResourceObjcopy(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":             "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/runtime_py3/main/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/cpp/malloc/jemalloc/ya.make":     "LIBRARY()\nSRCS(je.cpp)\nEND()\n",
		"library/cpp/malloc/jemalloc/je.cpp":      "int je(){return 0;}\n",
		"library/cpp/malloc/api/ya.make":          "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/api/api.cpp":          "int api(){return 0;}\n",
		"contrib/libs/jemalloc/ya.make":           "LIBRARY()\nSRCS(c.cpp)\nEND()\n",
		"contrib/libs/jemalloc/c.cpp":             "int c(){return 0;}\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "library/python/import_tracing/constructor/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")
	writeTestModuleFile(files, "library/python/testing/import_test/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "app/data.txt", "stub\n")
	writeTestModuleFile(files, "app/ya.make", `PY3_PROGRAM()
DISABLE(PYTHON_SQLITE3)
ENABLE(PYBUILD_NO_PYC)
RESOURCE(
    data.txt app/data.txt
)
PY_MAIN(app:main)
END()
`)

	g := testGen(newMemFS(files), "app")

	var resObjcopy, mainObjcopy *Node
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 || !strings.Contains(n.Outputs[0].string(), "/objcopy_") {
			continue
		}
		args := prCmdArgStrings(n)
		switch {
		case slices.Contains(args, "PY_MAIN=app:main"):
			mainObjcopy = n
		case slices.Contains(args, encb64.StdEncoding.EncodeToString([]byte("app/data.txt"))):
			resObjcopy = n
		}
	}

	if resObjcopy == nil {
		t.Fatalf("graph is missing the RESOURCE objcopy: %v", objcopyOutputs(g))
	}
	if mainObjcopy == nil {
		t.Fatalf("graph is missing the PY_MAIN objcopy: %v", objcopyOutputs(g))
	}

	ld := mustNodeByOutput(t, g, "$(B)/app/app")

	// (1) the PROGRAM LD must NOT carry the LIBRARY-owned RESOURCE objcopy.
	resOut := resObjcopy.Outputs[0].string()
	if nodeHasInput(ld, resOut) {
		t.Errorf("LD inputs over-link the LIBRARY-owned RESOURCE objcopy %q: %v", resOut, vfsStringsT3(ld.flatInputs()))
	}
	if slices.Contains(prCmdArgStrings(ld), strings.TrimPrefix(resOut, "$(B)/")) {
		t.Errorf("LD cmds over-link the LIBRARY-owned RESOURCE objcopy %q", resOut)
	}
	if depsContain(graphDeps(g, ld), resObjcopy.UID) {
		t.Errorf("graphDeps(LD) %v over-includes the RESOURCE objcopy uid %q", graphDeps(g, ld), resObjcopy.UID)
	}

	// (2) control: the PROGRAM-side PY_MAIN objcopy is a genuine direct LD member.
	mainOut := mainObjcopy.Outputs[0].string()
	if !nodeHasInput(ld, mainOut) {
		t.Errorf("LD inputs missing the PROGRAM-side PY_MAIN objcopy %q: %v", mainOut, vfsStringsT3(ld.flatInputs()))
	}
	if !depsContain(graphDeps(g, ld), mainObjcopy.UID) {
		t.Errorf("graphDeps(LD) %v missing the PY_MAIN objcopy uid %q", graphDeps(g, ld), mainObjcopy.UID)
	}

	// (3) the RESOURCE objcopy is not lost: the PY3_BIN_LIB twin's global archive
	// still packs it.
	var global *Node
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}
		if strings.HasSuffix(n.Outputs[0].string(), ".global.a") && n.TargetProperties.ModuleTag.string() == "py3_bin_lib_global" {
			global = n
			break
		}
	}
	if global == nil {
		t.Fatal("graph is missing the py3_bin_lib_global archive that must own the RESOURCE objcopy")
	}
	if !nodeHasInput(global, resOut) {
		t.Errorf("py3_bin_lib_global archive does not pack the RESOURCE objcopy %q: %v", resOut, vfsStringsT3(global.flatInputs()))
	}
	if !nodeHasInput(ld, global.Outputs[0].string()) {
		t.Errorf("LD inputs missing the peer global archive %q: %v", global.Outputs[0].string(), vfsStringsT3(ld.flatInputs()))
	}
}

func objcopyOutputs(g *Graph) []string {
	var out []string
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.Contains(n.Outputs[0].string(), "/objcopy_") {
			out = append(out, n.Outputs[0].string())
		}
	}
	return out
}

// FROM_SANDBOX(... OUT_NOAUTO ${VAR}) materializes fetched build artifacts, then
// RESOURCE_FILES(${VAR}) embeds them. Each payload is a $(B) fetch output: every
// objcopy chunk binds the $(B) payload and depends on the SB node, never a $(S).
func TestGen_FromSandboxVarOutNoautoResourceFilesFeedsObjcopyFromBuildRoot(t *testing.T) {
	files := map[string]string{}
	files["build/scripts/fetch_from_sandbox.py"] = "\n"
	files["build/scripts/fetch_from.py"] = "\n"
	files["build/scripts/process_command_files.py"] = "\n"

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	// Enough dict files to force several objcopy chunks. The SB node's main output
	// is the first listed file; it appears in every chunk's inputs because each
	// chunk depends on the single SB node via the OutTogether edge.
	var setBody strings.Builder
	const nDicts = 48
	for i := 2; i <= nDicts; i++ {
		setBody.WriteString("    plutonium_dicts/" + strconv.Itoa(i) + ".dict\n")
	}

	writeTestModuleFile(files, "dictmod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SET_APPEND(DICTS
`+setBody.String()+`)
FROM_SANDBOX(12019934890 OUT_NOAUTO
    ${DICTS}
)
RESOURCE_FILES(${DICTS})
END()
`)

	g := testGen(newMemFS(files), "dictmod")

	mainOut := "$(B)/dictmod/plutonium_dicts/2.dict"
	sb := mustNodeByAnyOutput(t, g, mainOut)
	if sb.KV.P != pkSB {
		t.Fatalf("main output producer kind = %q, want SB", sb.KV.P.string())
	}

	var chunks []*Node
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.HasPrefix(n.Outputs[0].string(), "$(B)/dictmod/objcopy_") {
			chunks = append(chunks, n)
		}
	}
	if len(chunks) < 2 {
		t.Fatalf("expected several objcopy chunks, got %d", len(chunks))
	}

	embedsMain := func(n *Node) bool {
		args := prCmdArgStrings(n)
		inInputs := false
		for _, a := range args {
			switch a {
			case "--inputs":
				inInputs = true
			case "--keys", "--kvs":
				inInputs = false
			default:
				if inInputs && a == mainOut {
					return true
				}
			}
		}
		return false
	}

	sawNonFirst := false
	for _, c := range chunks {
		// Every dict payload binds to $(B), never a phantom $(S) source.
		if nodeHasInput(c, "$(S)/dictmod/plutonium_dicts/2.dict") {
			t.Fatalf("objcopy %s carries phantom source-root 2.dict: %#v", c.Outputs[0].string(), vfsStringsT3(c.flatInputs()))
		}
		// Every chunk lists the SB main output (directly or via OutTogether).
		if !nodeHasInput(c, mainOut) {
			t.Fatalf("objcopy %s missing SB main-output input %s: %#v", c.Outputs[0].string(), mainOut, vfsStringsT3(c.flatInputs()))
		}
		if !embedsMain(c) {
			sawNonFirst = true
		}
	}
	if !sawNonFirst {
		t.Fatal("test did not exercise a non-first chunk (one that does not embed 2.dict); increase nDicts")
	}
}

// A RESOURCE embedding a chained-RUN_PROGRAM build output carries the producer
// chain's transitive $(S) source leaves onto the objcopy. The embedded second.bin
// gains c.gztproto (direct) plus a.remorph and gz.gzt (transitive through the
// opaque first.bin), but not base.proto (a compile-closure leaf pruned ref-side).
// The leaves ride as cache-key inputs only; command and hash stay unchanged.
func TestGen_ResourceGeneratedPayloadCarriesProducerSourceInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/gen/bin", "gen")

	files["mod/a.remorph"] = "rules\n"
	files["mod/gz.gzt"] = "gazetteer\n"
	files["mod/c.gztproto"] = "proto-ish\n"
	files["mod/base.proto"] = "syntax = \"proto3\";\nmessage Base {}\n"

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/first.bin
    IN
        a.remorph
        base.proto
        gz.gzt
    OUT_NOAUTO ${BINDIR}/first.bin
)
RUN_PROGRAM(
    tools/gen/bin
        --out
        ${BINDIR}/second.bin
    IN
        ${BINDIR}/first.bin
        c.gztproto
    OUT_NOAUTO ${BINDIR}/second.bin
)
RESOURCE(
    ${BINDIR}/second.bin KEY
)
END()
`)

	g := testGen(newMemFS(files), "mod")

	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}

	if !nodeHasInput(objcopy, "$(B)/mod/second.bin") {
		t.Fatalf("objcopy inputs missing build-root second.bin: %#v", objcopy.flatInputs())
	}

	// (2) the producer chain's transitive $(S) source leaves ride the objcopy.
	for _, want := range []string{
		"$(S)/mod/c.gztproto",
		"$(S)/mod/a.remorph",
		"$(S)/mod/gz.gzt",
	} {
		if !nodeHasInput(objcopy, want) {
			t.Fatalf("objcopy inputs missing producer source leaf %q: %#v", want, objcopy.flatInputs())
		}
	}

	// (3) base.proto (compile-closure leaf) is pruned ref-side.
	if nodeHasInput(objcopy, "$(S)/mod/base.proto") {
		t.Fatalf("objcopy must not carry the .proto compile-closure leaf: %#v", objcopy.flatInputs())
	}

	// (4) the source leaves are cache-key inputs only — never in the command or hash.
	args := prCmdArgStrings(objcopy)
	for _, a := range args {
		if strings.Contains(a, ".remorph") || strings.Contains(a, ".gzt") || strings.Contains(a, ".proto") {
			t.Fatalf("objcopy command must not name a producer source leaf: %v", args)
		}
	}
	wantHashInputs := []string{
		"${BINDIR}/second.bin",
		encb64.StdEncoding.EncodeToString([]byte("KEY")),
		"$S/mod",
	}
	sort.Strings(wantHashInputs)
	wantHash := md5Hex(strings.Join(wantHashInputs, ","))[:hashLen]
	wantOutput := "$(B)/mod/objcopy_" + wantHash + ".o"
	if got := objcopy.Outputs[0].string(); got != wantOutput {
		t.Fatalf("objcopy output = %q, want %q (hash must ignore source attribution)", got, wantOutput)
	}
}

// Negative control: an ordinary source-tree RESOURCE resolves to no producer, so
// it gains no synthetic generated-producer source inputs.
func TestGen_ResourceStaticSourceGainsNoGeneratedProducerInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/data.txt"] = "static\n"
	files["mod/extra.remorph"] = "unrelated\n"

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RESOURCE(
    data.txt KEY
)
END()
`)

	g := testGen(newMemFS(files), "mod")

	objcopy := findNodeByOutputPrefix(g, "$(B)/mod/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing mod objcopy output")
	}

	// The payload is the source file itself.
	if !nodeHasInput(objcopy, "$(S)/mod/data.txt") {
		t.Fatalf("objcopy inputs missing static source data.txt: %#v", objcopy.flatInputs())
	}

	if nodeHasInput(objcopy, "$(S)/mod/extra.remorph") {
		t.Fatalf("static-resource objcopy gained a synthetic producer source input: %#v", objcopy.flatInputs())
	}
}

// TestEmitProgramResource_CppProgramLinksObjcopy covers the C++ PROGRAM resource
// link path: a PROGRAM() with BUNDLE + RESOURCE emits the resource objcopy and
// links it as an LD member, embedding the BN build output ($(B)/prog/x.bundle),
// not a $(S) placeholder. RESOURCE packs into objcopy for every module, not only
// Python ones.
func TestEmitProgramResource_CppProgramLinksObjcopy(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	writeTestModuleFile(files, "dep/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "dep/d.cpp", "int d(){return 0;}\n")

	writeTestModuleFile(files, "prog/ya.make", "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nBUNDLE(dep NAME x.bundle)\nRESOURCE(x.bundle dep/key)\nEND()\n")
	writeTestModuleFile(files, "prog/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "prog")

	depAR := mustNodeByOutput(t, g, "$(B)/dep/libdep.a")
	bn := mustNodeByOutput(t, g, "$(B)/prog/x.bundle")

	if bn.KV.P != pkBN {
		t.Errorf("bundle node kv.p = %q, want BN", bn.KV.P.string())
	}
	if !nodeHasInput(bn, "$(B)/dep/libdep.a") {
		t.Errorf("BN node inputs missing $(B)/dep/libdep.a: %v", vfsStringsT3(bn.flatInputs()))
	}
	if !depsContain(graphDeps(g, bn), depAR.UID) {
		t.Errorf("graphDeps(BN) %v does not include the bundled AR uid %q", graphDeps(g, bn), depAR.UID)
	}

	// (2) the resource objcopy embeds the BN build output and depends on it.
	oc := findNodeByOutputPrefix(g, "$(B)/prog/objcopy_")
	if oc == nil {
		t.Fatal("graph is missing the prog resource objcopy node (C++ PROGRAM resource not linked)")
	}
	if !nodeHasInput(oc, "$(B)/prog/x.bundle") {
		t.Errorf("objcopy inputs missing the BN build output $(B)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if nodeHasInput(oc, "$(S)/prog/x.bundle") {
		t.Errorf("objcopy lists the nonexistent source $(S)/prog/x.bundle: %v", vfsStringsT3(oc.flatInputs()))
	}
	if !depsContain(graphDeps(g, oc), bn.UID) {
		t.Errorf("graphDeps(objcopy) %v does not include the BN uid %q", graphDeps(g, oc), bn.UID)
	}

	// (3) the PROGRAM LD links the objcopy object and depends on it.
	ld := mustNodeByOutput(t, g, "$(B)/prog/prog")
	if !nodeHasInput(ld, oc.Outputs[0].string()) {
		t.Errorf("LD inputs missing the resource objcopy member %q: %v", oc.Outputs[0].string(), vfsStringsT3(ld.flatInputs()))
	}
	if !depsContain(graphDeps(g, ld), oc.UID) {
		t.Errorf("graphDeps(LD) %v does not include the objcopy uid %q", graphDeps(g, ld), oc.UID)
	}
}

func depsContain(deps []UID, want UID) bool {
	for _, d := range deps {
		if d == want {
			return true
		}
	}

	return false
}
