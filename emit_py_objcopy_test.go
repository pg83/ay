package main

import (
	encb64 "encoding/base64"
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

func objcopyOutputs(g *Graph) []string {
	var out []string
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && strings.Contains(n.Outputs[0].string(), "/objcopy_") {
			out = append(out, n.Outputs[0].string())
		}
	}
	return out
}
