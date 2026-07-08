package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestPyNamespaceModListMD5RuntimePy3(t *testing.T) {
	ns := "library.python.runtime_py3."
	pys := []string{"entry_points.py"}

	h := md5.New()

	for _, srcRel := range pys {
		modName := strings.TrimSuffix(srcRel, ".py")
		modName = strings.ReplaceAll(modName, "/", ".")
		h.Write([]byte(ns + modName))
	}

	got := enchex.EncodeToString(h.Sum(nil))
	want := "bd17cfe3d9af11d01ff7b15ebc3786a7"

	if got != want {
		t.Fatalf("mod_list_md5 runtime_py3: got %q, want %q", got, want)
	}
}

func TestPyNamespaceModListMD5SymbolsModule(t *testing.T) {
	ns := "library.python.symbols.module."
	pys := []string{"__init__.py"}

	h := md5.New()

	for _, srcRel := range pys {
		modName := strings.TrimSuffix(srcRel, ".py")
		modName = strings.ReplaceAll(modName, "/", ".")
		h.Write([]byte(ns + modName))
	}

	got := enchex.EncodeToString(h.Sum(nil))
	want := "fe680e9ad9bf330ffcdf61616377919b"

	if got != want {
		t.Fatalf("mod_list_md5 symbols/module: got %q, want %q", got, want)
	}
}

func TestPyNamespaceModListMD5Py3ccSlow(t *testing.T) {
	ns := "tools.py3cc.slow."
	pys := []string{"main.py"}

	h := md5.New()

	for _, srcRel := range pys {
		modName := strings.TrimSuffix(srcRel, ".py")
		modName = strings.ReplaceAll(modName, "/", ".")
		h.Write([]byte(ns + modName))
	}

	got := enchex.EncodeToString(h.Sum(nil))
	want := "6a808dea4f9b84e552eba97b43845111"

	if got != want {
		t.Fatalf("mod_list_md5 tools/py3cc/slow: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashRuntimePy3RawEntryPoints(t *testing.T) {
	d := &ModuleData{
		tc:           testToolchain(),
		pySrcs:       anysOf("entry_points.py"),
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy3Library},
	}
	entries := buildPySrcEntries(d, "library/python/runtime_py3")

	if len(entries) != 1 {
		t.Fatalf("entries: got %d, want 1", len(entries))
	}

	if entries[0].token != "entry_points.py" {
		t.Errorf("token: got %q, want %q", entries[0].token, "entry_points.py")
	}

	expectedKey := "library/python/runtime_py3/entry_points.py"

	if entries[0].key != expectedKey {
		t.Errorf("key: got %q, want %q", entries[0].key, expectedKey)
	}

	items := pyGenResourceItems(entries)
	expectedKv := "resfs/src/resfs/file/py/" + expectedKey + "=${rootrel;context=TEXT;input=TEXT:\"entry_points.py\"}"

	if items[0].Key != expectedKv {
		t.Errorf("kvHash: got %q, want %q", items[0].Key, expectedKv)
	}

	nodes := runPySrcBatcher(t, d, "library/python/runtime_py3")

	if len(nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(nodes))
	}

	got := nodes[0].Outputs[0].string()
	want := "$(B)/library/python/runtime_py3/objcopy_84a3659770bdea15f8ae77837d.o"

	if got != want {
		t.Fatalf("runtime_py3 entry_points objcopy output: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashPy3ccSlowMain(t *testing.T) {
	d := &ModuleData{
		tc:           testToolchain(),
		pySrcs:       anysOf("main.py"),
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy3ProgramBin},
	}
	nodes := runPySrcBatcher(t, d, "tools/py3cc/slow")

	if len(nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(nodes))
	}

	got := nodes[0].Outputs[0].string()
	want := "$(B)/tools/py3cc/slow/objcopy_c3a5182796bc68c054c676bcc0.o"

	if got != want {
		t.Fatalf("py3cc/slow main.py objcopy output: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashSymbolsModuleDualEntry(t *testing.T) {
	d := &ModuleData{
		tc:           testToolchain(),
		pySrcs:       anysOf("__init__.py"),
		pyBuildNoPYC: false,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy23Library},
	}
	entries := buildPySrcEntries(d, "library/python/symbols/module")

	if len(entries) != 2 {
		t.Fatalf("entries: got %d, want 2 (yapyc3 + raw)", len(entries))
	}

	nodes := runPySrcBatcher(t, d, "library/python/symbols/module")

	if len(nodes) != 1 {
		t.Fatalf("nodes: got %d, want 1", len(nodes))
	}

	got := nodes[0].Outputs[0].string()
	want := "$(B)/library/python/symbols/module/objcopy_c325f0009e9625395005936d90.o"

	if got != want {
		t.Fatalf("symbols/module __init__.py objcopy output: got %q, want %q", got, want)
	}
}

func TestEmitPySrcObjcopyShellinghamTailOmitsBareKvs(t *testing.T) {
	d := &ModuleData{
		tc: testToolchain(),
		pySrcs: anysOf(
			"shellingham/__init__.py",
			"shellingham/_core.py",
			"shellingham/nt.py",
			"shellingham/posix/__init__.py",
			"shellingham/posix/_core.py",
			"shellingham/posix/proc.py",
			"shellingham/posix/ps.py",
		),
		pyBuildNoPY:   false,
		pyBuildNoPYC:  false,
		pyTopLevel:    true,
		pyYapycSuffix: pySrcYapycSuffix("contrib/python/shellingham"),
		moduleStmt:    &ModuleStmt{Name: tokPy3Library},
		unit:          resolveModuleUnit(tokPy3Library, KindLib, LangCPP),
	}

	em := newStreamingEmitter(nil)
	ctx := &GenCtx{emit: em, na: em.nodeArenas(), target: testTargetP, fs: newMemFS(nil)}
	wireTestScanners(ctx)
	instance := ModuleInstance{
		Path:     source("contrib/python/shellingham"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}
	seedResourceTools(ctx)

	e := newEmitContext(ctx, instance, d, nil)

	e.registerCollectPySrcs()

	res := e.emitPySrcObjcopy()

	if res == nil {
		t.Fatal("emitPySrcObjcopy returned nil")
	}

	emit := ctx.emit

	if got := emit.nodes.len(); got != 2 {
		t.Fatalf("emitted nodes: got %d, want 2", got)
	}

	tail := emit.nodes.s[1]

	if got := tail.Outputs[0].string(); got != "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o" {
		t.Fatalf("tail output = %q, want %q", got, "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o")
	}

	wantArgs := []string{
		testToolchain().Python3.string(),
		objcopyScriptVFS.string(),
		"--compiler", testToolchain().CXX.string(),
		"--objcopy", testToolchain().Objcopy.string(),
		"--compressor", rescompressorBinVFS.string(),
		"--rescompiler", rescompilerBinVFS.string(),
		"--output_obj", "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o",
		"--target", testTargetP.Triple,
		"--inputs", "$(B)/contrib/python/shellingham/shellingham/posix/ps.py.yjsy.yapyc3",
		"--keys", "cmVzZnMvZmlsZS9weS9zaGVsbGluZ2hhbS9wb3NpeC9wcy5weS55YXB5YzM=",
	}
	gotArgs := tail.Cmds[0].CmdArgs.flat()

	if !reflect.DeepEqual(genericStrs(gotArgs), wantArgs) {
		t.Fatalf("tail cmd args mismatch:\n got: %v\nwant: %v", gotArgs, wantArgs)
	}

	if contains(gotArgs, "--kvs") {
		t.Fatalf("tail cmd args unexpectedly contain --kvs: %v", gotArgs)
	}
}

func TestResolvePySrcRel_RootRelativeProto(t *testing.T) {
	fs := newMemFS(map[string]string{
		"market/idx/datacamp/proto/api/ExportMessage.proto":       "",
		"market/idx/datacamp/proto/external/ExportCategory.proto": "",
	})
	moduleDir := "market/idx/datacamp/proto/external"
	srcDirs := []VFS{dirKey(moduleDir).source()}

	got := resolvePySrcRel(fs, srcDirs, source(moduleDir), "market/idx/datacamp/proto/api/ExportMessage.proto")

	if want := "market/idx/datacamp/proto/api/ExportMessage.proto"; got.string() != want {
		t.Fatalf("root-relative proto: got %s, want %s", got.string(), want)
	}

	got = resolvePySrcRel(fs, srcDirs, source(moduleDir), "market/idx/datacamp/proto/external/ExportCategory.proto")

	if want := "market/idx/datacamp/proto/external/ExportCategory.proto"; got.string() != want {
		t.Fatalf("root-relative proto under module: got %s, want %s", got.string(), want)
	}
}

func TestResolvePySrcRel_DirtyPathNotRootBound(t *testing.T) {
	fs := newMemFS(map[string]string{
		"root.proto": "",
	})
	moduleDir := "pkg/sub"
	srcDirs := []VFS{dirKey(moduleDir).source()}

	got := resolvePySrcRel(fs, srcDirs, source(moduleDir), "../root.proto")

	if want := "pkg/sub/../root.proto"; got.string() != want {
		t.Fatalf("dirty srcRel must not source-root bind: got %s, want %s", got.string(), want)
	}
}

func pySrcTestEmitContext(d *ModuleData, modulePath string) (*EmitContext, *GenCtx) {
	em := newStreamingEmitter(nil)
	ctx := &GenCtx{emit: em, na: em.nodeArenas(), target: testTargetP, fs: newMemFS(nil)}

	wireTestScanners(ctx)

	instance := ModuleInstance{
		Path:     source(modulePath),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	return newEmitContext(ctx, instance, d, nil), ctx
}

func buildPySrcEntries(d *ModuleData, modulePath string) []PyGenResEntry {
	e, _ := pySrcTestEmitContext(d, modulePath)

	e.registerCollectPySrcs()

	var entries []PyGenResEntry

	for _, ps := range e.pySrcsReg {
		entries = append(entries, e.appendPyResEntries(nil, ps)...)
	}

	return entries
}

func runPySrcBatcher(t *testing.T, d *ModuleData, modulePath string) []*Node {
	t.Helper()

	e, ctx := pySrcTestEmitContext(d, modulePath)

	seedResourceTools(ctx)
	e.registerCollectPySrcs()

	var entries []PyGenResEntry

	for _, ps := range e.pySrcsReg {
		entries = append(entries, e.appendPyResEntries(nil, ps)...)
	}

	e.packResources(ResourcePack{Tag: unitTagPy3, Items: pyGenResourceItems(entries)})

	return ctx.emit.nodes.s
}

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
	wantHash := objcopyHash(paths, keysB64, kvsHash, "mod", unitTagPy3)
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

	producer := mustNodeByOutput(t, g, "$(B)/mod/__init__.py")
	bytecode := mustNodeByOutput(t, g, "$(B)/mod/__init__.py.yapyc3")
	deps := graphDeps(g, objcopy)

	if !slices.Contains(deps, producer.Ref) {
		t.Fatalf("objcopy deps %v missing RUN_PROGRAM producer ref %d", deps, producer.Ref)
	}

	if !slices.Contains(deps, bytecode.Ref) {
		t.Fatalf("objcopy deps %v missing py3cc bytecode producer ref %d", deps, bytecode.Ref)
	}

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		o := n.Outputs[0].string()

		if strings.HasPrefix(o, "$(B)/mod/") && strings.Contains(o, "_raw.auxcpp") {
			t.Fatalf("generated PY_SRCS produced a rescompiler aux node %q; want objcopy resfs", o)
		}
	}

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
	wantHash := objcopyHash([]string{"foo.py", "foo.py.yapyc3"}, keysB64, kvsHash, "modc", unitTagPy3)
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

	wantHash := objcopyHash(nil, nil, []string{kvHash}, "mod", unitTagPy3)
	wantObjcopy := "$(B)/mod/objcopy_" + wantHash + ".o"

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
}

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

	resOut := resObjcopy.Outputs[0].string()

	if nodeHasInput(ld, resOut) {
		t.Errorf("LD inputs over-link the LIBRARY-owned RESOURCE objcopy %q: %v", resOut, vfsStringsT3(ld.flatInputs()))
	}

	if slices.Contains(prCmdArgStrings(ld), strings.TrimPrefix(resOut, "$(B)/")) {
		t.Errorf("LD cmds over-link the LIBRARY-owned RESOURCE objcopy %q", resOut)
	}

	if depsContain(graphDeps(g, ld), resObjcopy.Ref) {
		t.Errorf("graphDeps(LD) %v over-includes the RESOURCE objcopy ref %d", graphDeps(g, ld), resObjcopy.Ref)
	}

	mainOut := mainObjcopy.Outputs[0].string()

	if !nodeHasInput(ld, mainOut) {
		t.Errorf("LD inputs missing the PROGRAM-side PY_MAIN objcopy %q: %v", mainOut, vfsStringsT3(ld.flatInputs()))
	}

	if !depsContain(graphDeps(g, ld), mainObjcopy.Ref) {
		t.Errorf("graphDeps(LD) %v missing the PY_MAIN objcopy ref %d", graphDeps(g, ld), mainObjcopy.Ref)
	}

	var global *Node

	for _, n := range g.Graph {
		if len(n.Outputs) == 0 {
			continue
		}

		if strings.HasSuffix(n.Outputs[0].string(), ".global.a") && nodeHasInput(n, resOut) {
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

func TestEmitPyRegister_ProducerEmittedAtTargetPlatform(t *testing.T) {
	emit := newStreamingEmitter(nil)
	ctx := &GenCtx{
		emit:   emit,
		na:     emit.nodeArenas(),
		host:   testHostP,
		target: testTargetP,
	}
	wireTestScanners(ctx)
	d := &ModuleData{pyRegister: sTRS("_sqlite3")}
	hostInst := ModuleInstance{
		Path:     source("contrib/tools/python3/Modules/_sqlite"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testHostP,
	}
	targetInst := hostInst
	targetInst.Platform = testTargetP

	newEmitContext(ctx, hostInst, d, nil).emitPyRegister(false)
	newEmitContext(ctx, targetInst, d, nil).emitPyRegister(false)

	wantOutput := "$(B)/contrib/tools/python3/Modules/_sqlite/_sqlite3.reg3.cpp"
	var pyNodes []*Node

	for _, n := range emit.nodes.s {
		if len(n.Outputs) == 1 && n.Outputs[0].string() == wantOutput {
			pyNodes = append(pyNodes, n)
		}
	}

	if len(pyNodes) != 2 {
		t.Fatalf("emitted %d PY producers, want 2 (one per instance)", len(pyNodes))
	}

	for _, n := range pyNodes {
		if string(n.Platform.Target) != string(testTargetP.Target) {
			t.Errorf("PY node platform = %q, want %q (target)", n.Platform.Target, testTargetP.Target)
		}
	}
}

func TestGen_GeneratedPySrcsBytecodeNamingAndSourceInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, "mod/gen/bin", "gen")

	writeTestModuleFile(files, "other/other.h", "#pragma once\n")

	writeTestModuleFile(files, "mod/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PY_SRCS(__init__.py)
RUN_PROGRAM(
    mod/gen/bin
        --save_file_path __init__.py
    IN_NOPARSE gen.h
    OUT_NOAUTO __init__.py
)
END()
`)
	writeTestModuleFile(files, "mod/gen.h", "#pragma once\n#include <other/other.h>\n")

	g := testGen(newMemFS(files), "mod")

	bc := mustNodeByOutput(t, g, "$(B)/mod/__init__.py.yapyc3")
	args := bc.Cmds[0].CmdArgs.flat()

	if indexOfArg(args, "__init__.py-") < 0 {
		t.Fatalf("py3cc cmd missing generated source-name arg %q: %v", "__init__.py-", genericStrs(args))
	}

	if indexOfArg(args, "mod/__init__.py-") >= 0 {
		t.Fatalf("py3cc cmd uses module-rooted source name, want raw token: %v", genericStrs(args))
	}

	producer := mustNodeByOutput(t, g, "$(B)/mod/__init__.py")
	foundDep := false

	for _, d := range graphDeps(g, bc) {
		if d == producer.Ref {
			foundDep = true

			break
		}
	}

	if !foundDep {
		t.Fatalf("bytecode deps %v do not include producer ref %d", graphDeps(g, bc), producer.Ref)
	}

	if !nodeHasInput(bc, "$(S)/mod/gen.h") {
		t.Fatalf("bytecode inputs missing direct generator source gen.h: %#v", bc.flatInputs())
	}

	if !nodeHasInput(bc, "$(S)/other/other.h") {
		t.Fatalf("bytecode inputs missing transitive generator closure other/other.h: %#v", bc.flatInputs())
	}
}

func seedResourceTools(ctx *GenCtx) {
	dummy := build("dummy-tool")

	for _, tool := range []ARG{argToolsRescompiler, argToolsRescompressor} {
		ctx.tools.put(tool, &ModuleEmitResult{LDPath: &dummy})
	}
}
