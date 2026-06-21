package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"slices"
	"sort"
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

// A FROM_SANDBOX OUT/OUT_NOAUTO file is a Sandbox-fetched build output. When a
// RESOURCE in the same module embeds it via an arcadia-root-relative path rooted
// at the module dir (yt/yt/library/ytprof/bundle/llvm-symbolizer), the objcopy
// packer must read the artifact from $(B) and depend on the SB fetch node — never
// resolve it to a $(S) source path (which faults the UID finalizer's content hash
// under --sandboxing) nor to the module-dir-doubled $(B) path. The resfs key
// (base64 of the literal RESOURCE key) is unchanged from a source resource.
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

	// FROM_SANDBOX emits an SB fetch node producing the OUT_NOAUTO file in $(B).
	sb := mustNodeByOutput(t, g, "$(B)/yt/bundle/llvm-symbolizer")
	if sb.KV.P != pkSB {
		t.Fatalf("llvm-symbolizer producer kind = %q, want SB", sb.KV.P.string())
	}

	objcopy := findNodeByOutputPrefix(g, "$(B)/yt/bundle/objcopy_")
	if objcopy == nil {
		t.Fatal("graph is missing yt/bundle objcopy output")
	}

	// The objcopy embeds the fetched artifact from $(B), not a source path and not
	// the module-dir-doubled build path.
	if !nodeHasInput(objcopy, "$(B)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs missing build-root fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs use the source path for a fetched artifact: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(B)/yt/bundle/yt/bundle/llvm-symbolizer") {
		t.Fatalf("objcopy inputs double the module dir: %#v", objcopy.flatInputs())
	}

	// The objcopy depends on the SB fetch node that produces the artifact.
	if !slices.Contains(graphDeps(g, objcopy), sb.UID) {
		t.Fatalf("objcopy deps missing SB fetch uid %q: %v", sb.UID, graphDeps(g, objcopy))
	}

	// The resfs key (the literal RESOURCE key, base64) is unaffected by the
	// build-root resolution — only the input VFS and the producer dep change.
	wantKey := encb64.StdEncoding.EncodeToString([]byte("/ytprof/llvm-symbolizer"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 RESOURCE key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}
}

// A RESOURCE() declared in a PROTO_LIBRARY body belongs to the C++ _CPP_PROTO
// submodule (MODULE_TAG=CPP_PROTO). Upstream's TObjCopyResourcePacker folds the
// owning submodule's MODULE_TAG into the objcopy output-name hash and stamps the
// objcopy node's module_tag with the lowercased tag (cpp_proto); the submodule's
// .global.a carries <MODULE_TAG>_global (cpp_proto_global). Before the fix the
// CPP-proto resource path folded in nothing (wrong hash, missing objcopy tag) and
// the global archive fell through to the generic `global` tag.
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

// A RESOURCE_FILES path may name an ordinary source file that lives at the
// arcadia root OUTSIDE the consuming module (e.g. yabs/server/libs/banner_flags
// embedding modadvert/dyn_disclaimers/disclaimers_config.pb.txt). res.py expands
// it into a payload member AND a resfs/src kv member
// (${rootrel;input=TEXT:"<path>"}); ymake resolves that input the same way as
// the payload — to $(S)/<path>, the source-root file — because the file exists
// there. Before the fix the resfs/src kv fallback naively joined module dir +
// path, fabricating a phantom $(S)/<module>/<path> input that sg7 then aborted
// while content-hashing. Both members must bind to the same root source path,
// with no producer dep for an ordinary source.
func TestGen_ResourceFilesRootRelativeSourceFromOtherModule(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/resource/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n",
	}
	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	// The embedded source lives at the arcadia root, NOT under the module dir.
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

	// resfs/file key (literal RESOURCE_FILES key, base64) is unchanged.
	wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/modadvert/dyn_disclaimers/disclaimers_config.pb.txt"))
	if !slices.Contains(prCmdArgStrings(objcopy), wantKey) {
		t.Fatalf("objcopy --keys missing base64 resfs/file key %q: %v", wantKey, prCmdArgStrings(objcopy))
	}

	// resfs/src kv command value carries the root source path, not the phantom.
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

	// Upstream's TObjCopyResourcePacker hashes RESOURCE() pair.Path raw, i.e.
	// '${BINDIR}/data.json', NOT the expanded '$(B)/db/data.json'. Pre-
	// expanding here drifts the objcopy_<hash> filename vs REF (caught on
	// sg5: yt/yql/.../yt/provider/objcopy_da30... was 0288...). Lock the
	// hash inputs we sort+md5 so a future "helpful" expansion regresses
	// fast.
	wantHashInputs := []string{
		"${BINDIR}/data.json",
		// base64 of "/data.json" — RESOURCE() Key is literal, not
		// resfs/file/-prefixed (unlike RESOURCE_FILES).
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
// artifact, not a source file. RESOURCE_FILES expands each file into a payload
// entry AND a resfs/src kv entry whose ${rootrel;input=TEXT:"..."} names the
// same file; both must resolve to $(B) with the producer dependency. Mirrors the
// sg7 unit yabs/models_services/feature_store/mappers/common/catalog (STDOUT_NOAUTO
// common.json -> RESOURCE_FILES). Before the fix the resfs/src input resolved to
// a phantom $(S)/db/common.json that does not exist and carries no producer edge.
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

	// (1) build-tree input, no phantom $(S) source for the generated json — both
	// the payload entry and the resfs/src kv entry point at $(B).
	if !nodeHasInput(objcopy, "$(B)/db/common.json") {
		t.Fatalf("objcopy inputs missing build-root common.json: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/db/common.json") {
		t.Fatalf("objcopy inputs still carry phantom source-root common.json: %#v", objcopy.flatInputs())
	}

	// (2) producer dependency: the objcopy node depends on the PR node that
	// produces common.json via STDOUT_NOAUTO.
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

	// (3) the expected RESOURCE_FILES key survives: resfs/file/<PREFIX><file>,
	// base64-encoded, present in the objcopy command.
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
// matches to RESOURCE_FILES with PREFIX p and STRIP ${ARCADIA_ROOT}/<moddir>/.
// The globbed paths keep the literal ${ARCADIA_ROOT} marker (upstream's GLOB
// runs before ${ARCADIA_ROOT} is bound), so the resfs/file key embeds it
// verbatim — exactly the models_meta/libs/cpp shape in sg7. This asserts the
// emitted objcopy hash/inputs match what the equivalent explicit RESOURCE_FILES
// produces, that the glob is .json-only and sorted, and that the module's
// .global.a links the objcopy.
func TestGen_AllResourceFilesGlobMatchesResourceFiles(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/ignore.txt"] = "not a resource\n"

	// Mirror the sg7 models_meta layout: the consuming module lives in a sibling
	// dir of the config dir, so STRIP=${ARCADIA_ROOT}/<moddir>/ does NOT strip and
	// the resfs/file key retains the literal ${ARCADIA_ROOT} marker from the glob.
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
		// STRIP=${ARCADIA_ROOT}/mod/libs/cpp/ is not a prefix of the cfg path, so
		// the whole marker-rooted path becomes the key tail.
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
		// --keys: base64 of the literal-marker key (shielded from command rendering).
		wantKey := encb64.StdEncoding.EncodeToString([]byte("resfs/file/" + prefix + "${ARCADIA_ROOT}/mod/cfg/" + f))
		if !slices.Contains(args, wantKey) {
			t.Fatalf("objcopy --keys missing base64 marker key for %q: %v", f, args)
		}
		// --kvs command form: the marker is rendered to $(S), the rootrel resolved.
		wantKv := "resfs/src/resfs/file/" + prefix + "$(S)/mod/cfg/" + f + "=mod/cfg/" + f
		if !slices.Contains(args, wantKv) {
			t.Fatalf("objcopy --kvs missing rendered resfs/src for %q (want %q): %v", f, wantKv, args)
		}
		if slices.Contains(args, "resfs/src/resfs/file/"+prefix+"${ARCADIA_ROOT}/mod/cfg/"+f+"=mod/cfg/"+f) {
			t.Fatalf("objcopy --kvs leaked the literal ${ARCADIA_ROOT} marker for %q: %v", f, args)
		}
	}

	// The library's global archive links the resource objcopy.
	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")
	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}
	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}

// ALL_RESOURCE_FILES(Ext Dirs...) with a RELATIVE DIR (e.g. the real
// `ALL_RESOURCE_FILES(j2 templates)`): upstream resolves the glob against the
// module dir, so the matches are ${ARCADIA_ROOT}/<moddir>/<dir>/<file>. Here the
// STRIP=${ARCADIA_ROOT}/<moddir>/ default IS a prefix of those paths, so it
// strips and the resfs/file key becomes the moddir-relative <dir>/<file> — no
// literal marker survives in the key. This exercises the relative-DIR path class
// the prior implementation silently dropped.
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
		// STRIP=${ARCADIA_ROOT}/mod/app/ IS a prefix here, so the key tail is the
		// moddir-relative templates/<file>; no marker survives in the key.
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

// ALL_RESOURCE_FILES_FROM_DIRS with a relative DIR carrying `..` segments (the
// real idm_syncer `../../configs/adminka/projects` shape): upstream globs the
// dir non-recursively against the module dir and reconstructs the `..`. The
// resfs/file key is PREFIX-joined to the ${ARCADIA_ROOT}-rooted match (STRIP at
// the module dir does not cover the parent-relative config dir, exactly like the
// models_meta sibling-dir case).
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
		// STRIP=${ARCADIA_ROOT}/base/tools/sync/ is not a prefix of the parent
		// config dir, so the whole marker-rooted path is the key tail.
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

// ALL_RESOURCE_FILES with a SOURCE-ROOTED DIR carrying a trailing slash (the real
// yabs/air shape `${ARCADIA_ROOT}/yabs/air/ssp/google/asas/pretargetings/`):
// upstream's TGlobPattern splits the pattern with SkipEmpty, so the empty trailing
// segment is dropped and each match reconstructs to a normalized
// ${ARCADIA_ROOT}/<arc-rel>/<file> with no double slash. The stored resource path
// (and therefore the objcopy hash and keys) must carry no `//`.
func TestGen_AllResourceFilesGlobSourceRootedTrailingSlash(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/b.json"] = "{\"b\":2}\n"

	// The DIR is source-rooted AND ends with a slash, exactly like the real
	// yabs/air/infra/solo/registry/alert/ssp usage.
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
		// No double slash: the trailing-slash DIR normalizes to ${ARCADIA_ROOT}/mod/cfg.
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

// ALL_RESOURCE_FILES with a DIR token that itself carries a trailing `*` wildcard
// segment (the real split_configs shape
// `ALL_RESOURCE_FILES(json PREFIX ${MODELS_PATH} ${ARCADIA_ROOT}/${MODELS_PATH}/*)`):
// the macro appends `/*.<ext>`, so the per-DIR glob pattern is `dir/*/*.json` — an
// interior `*` segment that expands to every immediate subdir, then `.json` files
// inside each. Upstream's TGlobPattern walks the sorted directory frontier segment
// by segment; the prior single-literal-directory modeling could not match an
// interior wildcard and dropped the entire match set (no objcopy node at all).
// This asserts the depth-2 matches feed the objcopy exactly like an equivalent
// explicit RESOURCE_FILES, that a depth-1 (`dir/top.json`) file is NOT matched by
// `*/*.json`, and that non-json files are excluded.
func TestGen_AllResourceFilesGlobDirWildcard(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")

	files["mod/cfg/sub1/a.json"] = "{\"a\":1}\n"
	files["mod/cfg/sub2/b.json"] = "{\"b\":2}\n"
	files["mod/cfg/sub1/skip.txt"] = "not a resource\n"
	files["mod/cfg/top.json"] = "{\"top\":0}\n" // depth-1: NOT matched by dir/*/*.json

	// DIR ends in `*`, exactly like ${ARCADIA_ROOT}/${MODELS_PATH}/* in split_configs.
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
		// STRIP=${ARCADIA_ROOT}/mod/libs/cpp/ is not a prefix of the cfg path, so
		// the whole marker-rooted path becomes the key tail.
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

	// The library's global archive links the dir/* resource objcopy as a member.
	globalAr := nodeByOutput(g, "$(B)/mod/libs/cpp/libmod-libs-cpp.global.a")
	if globalAr == nil {
		t.Fatal("graph is missing the global archive libmod-libs-cpp.global.a")
	}
	if !slices.Contains(prCmdArgStrings(globalAr), wantOutput) {
		t.Fatalf("global archive does not link the dir/* resource objcopy %q: %v", wantOutput, prCmdArgStrings(globalAr))
	}
}

// A build-generated PY_SRCS source (PY_SRCS(__init__.py) where __init__.py is
// the OUT_NOAUTO output of a RUN_PROGRAM) is packaged by upstream onpy_srcs
// exactly like a checked-in py: it flows through unit.onresource_files → an
// objcopy_<hash>.o resfs node embedding the generated .py and its .yapyc3 from
// $(B), with deps on both producers. It is NOT routed through the rescompiler
// _raw.auxcpp path (that path is for PY proto resources only). It is also
// EXCLUDED from the py/namespace resource, because is_extended_source_search
// requires is_arc_src(path) — a $(B) generated file is not an arc source — so a
// module whose only PY_SRCS is generated emits no namespace node at all.
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

	// Upstream packages the generated py via onresource_files → objcopy resfs. The
	// hash is over the same path/key/kvHash strings as a checked-in py (paths are
	// rootrel-independent of $S vs $B), so reuse buildPySrcEntriesFor's shape.
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

	// The objcopy embeds the generated .py and its bytecode from $(B), not $(S).
	if !nodeHasInput(objcopy, "$(B)/mod/__init__.py") {
		t.Fatalf("objcopy inputs missing build-root generated source: %#v", objcopy.flatInputs())
	}
	if !nodeHasInput(objcopy, "$(B)/mod/__init__.py.yapyc3") {
		t.Fatalf("objcopy inputs missing build-root bytecode: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(S)/mod/__init__.py") {
		t.Fatalf("objcopy inputs use a source path for the generated py: %#v", objcopy.flatInputs())
	}

	// The objcopy depends on both producers: the RUN_PROGRAM that emits __init__.py
	// and the py3cc that emits its .yapyc3.
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

	// No py/namespace resource: the only PY_SRCS is generated (not an arc source).
	for _, n := range g.Graph {
		for _, a := range prCmdArgStrings(n) {
			if strings.Contains(a, "py/namespace") && strings.Contains(a, "/mod=") {
				t.Fatalf("generated-only PY_SRCS emitted a namespace resource: %q", a)
			}
		}
	}

	// The module's global archive links the resfs objcopy.
	globalAr := mustNodeByOutput(t, g, "$(B)/mod/libpy3mod.global.a")
	if !slices.Contains(prCmdArgStrings(globalAr), wantObjcopy) {
		t.Fatalf("global archive does not link the generated-py objcopy %q: %v", wantObjcopy, prCmdArgStrings(globalAr))
	}
}

// Guard: an ordinary checked-in PY_SRCS module is unaffected by the generated-py
// objcopy routing. Its objcopy embeds the .py from $(S) (the source), and the
// module still emits a py/namespace resource (a checked-in py IS an arc source,
// so extended source search applies).
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

	// The checked-in .py resource binds to the source path, not a build path.
	if !nodeHasInput(objcopy, "$(S)/modc/foo.py") {
		t.Fatalf("checked-in objcopy inputs missing source foo.py: %#v", objcopy.flatInputs())
	}
	if nodeHasInput(objcopy, "$(B)/modc/foo.py") {
		t.Fatalf("checked-in objcopy inputs use a build path for a source py: %#v", objcopy.flatInputs())
	}

	// A checked-in py IS an arc source, so the namespace resource is emitted.
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

// A checked-in `.py` passed to PY_SRCS as an arcadia-root-relative token (the
// file lives at the root path it names, not under the module dir) keys its
// py/namespace resource at the upstream namespace root derived from the resolved
// rootrel_arc_src — `mod_root_path = rootrel[:-(len(token)+1)]` — which for a
// fully-root-relative token is the empty string: `py/namespace/<md5>/=<value>`.
// The namespace VALUE stays module-derived. Mirrors codegen_bin's
// PY_SRCS(yabs/.../bin/__main__.py).
func TestGen_RootRelativePySrcsNamespaceKeyedAtRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	// The source is checked in at the arcadia root path it names, NOT under mod/.
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

	// Upstream key: mod_list_md5 over the module name (ns + dotted token),
	// mod_root_path = "" (rootrel == token), value = module dotted path + ".".
	h := md5.New()
	h.Write([]byte("mod.other.place.thing"))
	nsMD5 := enchex.EncodeToString(h.Sum(nil))

	wantKeyRoot := "py/namespace/" + nsMD5 + "/"
	kvHash := wantKeyRoot + "=\"mod.\""
	kvCmd := wantKeyRoot + "=mod."

	wantHash := objcopyHash(nil, nil, []string{kvHash}, "mod", stringPtr("PY3"))
	wantObjcopy := "$(B)/mod/objcopy_" + wantHash + ".o"

	// The namespace objcopy command must key at the root (empty path), not the
	// module dir.
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

	// Output/member identity follows the resource key.
	objcopy := nodeByOutput(g, wantObjcopy)
	if objcopy == nil {
		t.Fatalf("graph is missing root-keyed namespace objcopy %q\nobjcopy nodes: %v", wantObjcopy, objcopyOutputs(g))
	}

	// The module's global archive links exactly that member.
	globalAr := mustNodeByOutput(t, g, "$(B)/mod/libpy3mod.global.a")
	if !slices.Contains(prCmdArgStrings(globalAr), wantObjcopy) {
		t.Fatalf("global archive does not link the root-keyed namespace objcopy %q: %v", wantObjcopy, prCmdArgStrings(globalAr))
	}
}

// Guard the token-form distinction: a swig-injected PY_SRCS source arrives as a
// full `${ARCADIA_BUILD_ROOT}/<full>.py` token (d.pySrcsFullName=true) and stays
// on the rescompiler _raw.auxcpp path — it must NOT be re-routed to objcopy resfs
// by the generated-py handling. (The bare-token RUN_PROGRAM case above is the one
// that goes through objcopy.)
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

	// The swig py is embedded through the rescompiler _raw.auxcpp path.
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

// A PY3_PROGRAM is a multimodule: the PROGRAM half (PY3_BIN) has
// .IGNORED=RESOURCE RESOURCE_FILES and ENABLE(PROCESS_PY_MAIN_ONLY), so the
// resfs objcopy a RESOURCE() produces is owned by the LIBRARY twin (PY3_BIN_LIB)
// and stamped module_tag=py3_bin_lib. Only PY_MAIN (program-side) carries
// module_tag=py3_bin. Before the fix emitResourceObjcopy.flush() stamped the
// RESOURCE objcopy with py3_bin for the tokPy3Program case, contradicting its own
// output hash (computed with the py3_bin_lib tag) and diverging from REF
// (representatives: ads/bsyeti, ads/caesar, bigrt .../codegen/objcopy_*.o).
func TestGen_Py3ProgramResourceObjcopyUsesLibTagPyMainKeepsBinTag(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":             "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/python/runtime_py3/main/ya.make":  "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
		"library/cpp/malloc/jemalloc/ya.make":      "LIBRARY()\nSRCS(je.cpp)\nEND()\n",
		"library/cpp/malloc/jemalloc/je.cpp":       "int je(){return 0;}\n",
		"library/cpp/malloc/api/ya.make":           "LIBRARY()\nSRCS(api.cpp)\nEND()\n",
		"library/cpp/malloc/api/api.cpp":           "int api(){return 0;}\n",
		"contrib/libs/jemalloc/ya.make":            "LIBRARY()\nSRCS(c.cpp)\nEND()\n",
		"contrib/libs/jemalloc/c.cpp":              "int c(){return 0;}\n",
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

// A PY3_PROGRAM's PROGRAM half (PY3_BIN) has .IGNORED=RESOURCE RESOURCE_FILES
// (conf/python.conf:350): the RESOURCE objcopy is owned by the PY3_BIN_LIB twin
// and reaches the program ONLY through .PEERDIRSELF=PY3_BIN_LIB's global archive,
// never as a direct LD member. The PROGRAM-side PY_MAIN objcopy, by contrast, is
// a genuine direct LD member. Before the fix emitResourceObjcopy emitted the
// RESOURCE objcopy on the PROGRAM side too, so the program's LD over-linked it as
// a coupled cmds+inputs member that upstream's reference lacks (representatives:
// ads/bsyeti, ads/caesar, apphost cow, bigrt .../codegen/codegen).
func TestGen_Py3ProgramLDDoesNotDirectlyLinkResourceObjcopy(t *testing.T) {
	files := map[string]string{
		"contrib/libs/python/ya.make":            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
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

	// (1) over-emission under test: the PROGRAM LD must NOT carry the LIBRARY-owned
	// RESOURCE objcopy as a direct member, in cmds or inputs, nor depend on it.
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
	// still packs it, so the resource reaches the binary through the peer global.
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
