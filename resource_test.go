package main

import (
	"crypto/md5"
	"encoding/base64"
	enchex "encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestObjcopyHashCerts(t *testing.T) {
	paths := []string{"cacert.pem"}
	keysB64 := []string{base64.StdEncoding.EncodeToString([]byte("/builtin/cacert"))}
	kvs := []string{}
	unitPath := "certs"
	var moduleTag *string

	got := objcopyHash(paths, keysB64, kvs, unitPath, moduleTag)
	want := "c27c99b2d9d5eade92fd72d0aa"
	if got != want {
		t.Fatalf("certs objcopy hash: got %q, want %q", got, want)
	}
}

func TestObjcopyHashRapidjson(t *testing.T) {
	paths := []string{
		".dist-info/METADATA",
		".dist-info/top_level.txt",
		"rapidjson/license.txt",
		"rapidjson/readme.md",
	}
	prefix := "devtools/ymake/contrib/python-rapidjson/"
	keysRaw := make([]string, len(paths))
	kvs := make([]string, len(paths))
	for i, p := range paths {
		fileKey := "resfs/file/" + prefix + p
		keysRaw[i] = fileKey
		kvs[i] = "resfs/src/" + fileKey + "=${rootrel;context=TEXT;input=TEXT:\"" + p + "\"}"
	}
	keysB64 := make([]string, len(keysRaw))
	for i, k := range keysRaw {
		keysB64[i] = base64.StdEncoding.EncodeToString([]byte(k))
	}

	got := objcopyHash(paths, keysB64, kvs, "devtools/ymake/contrib/python-rapidjson", stringPtr("PY3"))
	want := "55c44b1fdbfda511798cd895e2"
	if got != want {
		t.Fatalf("rapidjson objcopy hash: got %q, want %q", got, want)
	}
}

func TestExpandResourceFilesRapidjson(t *testing.T) {
	args := []string{
		"PREFIX", "devtools/ymake/contrib/python-rapidjson/",
		".dist-info/METADATA",
		".dist-info/top_level.txt",
		"rapidjson/license.txt",
		"rapidjson/readme.md",
	}

	got := expandResourceFiles(args)

	want := []ResourceEntry{
		{Path: "-", Key: "resfs/src/resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/METADATA=${rootrel;context=TEXT;input=TEXT:\".dist-info/METADATA\"}"},
		{Path: ".dist-info/METADATA", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/METADATA"},
		{Path: "-", Key: "resfs/src/resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/top_level.txt=${rootrel;context=TEXT;input=TEXT:\".dist-info/top_level.txt\"}"},
		{Path: ".dist-info/top_level.txt", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/.dist-info/top_level.txt"},
		{Path: "-", Key: "resfs/src/resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/license.txt=${rootrel;context=TEXT;input=TEXT:\"rapidjson/license.txt\"}"},
		{Path: "rapidjson/license.txt", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/license.txt"},
		{Path: "-", Key: "resfs/src/resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/readme.md=${rootrel;context=TEXT;input=TEXT:\"rapidjson/readme.md\"}"},
		{Path: "rapidjson/readme.md", Key: "resfs/file/devtools/ymake/contrib/python-rapidjson/rapidjson/readme.md"},
	}

	if len(got) != len(want) {
		t.Fatalf("expanded entries: got %d, want %d", len(got), len(want))
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

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

func TestPyNamespaceObjcopyHashRuntimePy3(t *testing.T) {
	kv := `py/namespace/bd17cfe3d9af11d01ff7b15ebc3786a7/library/python/runtime_py3="library.python.runtime_py3."`

	got := objcopyHash(nil, nil, []string{kv}, "library/python/runtime_py3", stringPtr("PY3"))
	want := "3b0561f75631281b973aa8b64e"
	if got != want {
		t.Fatalf("runtime_py3 namespace objcopy hash: got %q, want %q", got, want)
	}
}

func TestNoCheckImportsObjcopyHashLib2Py(t *testing.T) {
	value := "_ios_support _pyrepl.* antigravity asyncio.unix_events asyncio.windows_events asyncio.windows_utils ctypes.wintypes curses.* dbm.gnu dbm.ndbm dbm.sqlite3 encodings.mbcs encodings.oem lzma multiprocessing.popen_fork multiprocessing.popen_forkserver multiprocessing.popen_spawn_posix multiprocessing.popen_spawn_win32 sqlite3.* turtle pty tty"
	kv := `py/no_check_imports/2fepmfaacurvvaalmzqchmko4a="` + value + `"`

	got := objcopyHash(nil, nil, []string{kv}, "contrib/tools/python3/lib2/py", stringPtr("PY3"))
	want := "cd47bcaec327e5eb9db4641ec8"
	if got != want {
		t.Fatalf("contrib/tools/python3/lib2/py no_check_imports objcopy hash: got %q, want %q", got, want)
	}
}

func TestPyMainObjcopyHashPy3ccSlow(t *testing.T) {
	kv := "PY_MAIN=tools.py3cc.slow.main:main"

	got := objcopyHash(nil, nil, []string{kv}, "tools/py3cc/slow", stringPtr("PY3"))
	want := "4b1c18d0dc6973976969ad23be"
	if got != want {
		t.Fatalf("tools/py3cc/slow PY_MAIN objcopy hash: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashRuntimePy3RawEntryPoints(t *testing.T) {
	d := &ModuleData{
		pySrcs:       STRS("entry_points.py"),
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy3Library},
	}
	entries := buildPySrcEntries(d, "library/python/runtime_py3")
	if len(entries) != 1 {
		t.Fatalf("entries: got %d, want 1", len(entries))
	}
	if entries[0].pathHash != "entry_points.py" {
		t.Errorf("pathHash: got %q, want %q", entries[0].pathHash, "entry_points.py")
	}
	expectedKey := "resfs/file/py/library/python/runtime_py3/entry_points.py"
	if entries[0].key != expectedKey {
		t.Errorf("key: got %q, want %q", entries[0].key, expectedKey)
	}
	expectedKv := "resfs/src/" + expectedKey + "=${rootrel;context=TEXT;input=TEXT:\"entry_points.py\"}"
	if entries[0].kvHash != expectedKv {
		t.Errorf("kvHash: got %q, want %q", entries[0].kvHash, expectedKv)
	}

	chunks := chunkPySrcEntries(entries)
	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	ch := chunks[0]
	got := objcopyHash(ch.paths, ch.keys, ch.kvsHash, "library/python/runtime_py3", stringPtr("PY3"))
	want := "84a3659770bdea15f8ae77837d"
	if got != want {
		t.Fatalf("runtime_py3 entry_points objcopy hash: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashPy3ccSlowMain(t *testing.T) {
	d := &ModuleData{
		pySrcs:       STRS("main.py"),
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy3ProgramBin},
	}
	entries := buildPySrcEntries(d, "tools/py3cc/slow")
	chunks := chunkPySrcEntries(entries)
	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	got := objcopyHash(chunks[0].paths, chunks[0].keys, chunks[0].kvsHash, "tools/py3cc/slow", stringPtr("PY3"))
	want := "c3a5182796bc68c054c676bcc0"
	if got != want {
		t.Fatalf("py3cc/slow main.py objcopy hash: got %q, want %q", got, want)
	}
}

func TestPySrcObjcopyHashSymbolsModuleDualEntry(t *testing.T) {
	d := &ModuleData{
		pySrcs:       STRS("__init__.py"),
		pyBuildNoPYC: false,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: tokPy23Library},
	}
	entries := buildPySrcEntries(d, "library/python/symbols/module")
	if len(entries) != 2 {
		t.Fatalf("entries: got %d, want 2 (yapyc3 + raw)", len(entries))
	}
	chunks := chunkPySrcEntries(entries)
	if len(chunks) != 1 {
		t.Fatalf("chunks: got %d, want 1", len(chunks))
	}
	got := objcopyHash(chunks[0].paths, chunks[0].keys, chunks[0].kvsHash, "library/python/symbols/module", stringPtr("PY3"))
	want := "c325f0009e9625395005936d90"
	if got != want {
		t.Fatalf("symbols/module __init__.py objcopy hash: got %q, want %q", got, want)
	}
}

func TestChunkPySrcEntriesEmptyReturnsNil(t *testing.T) {
	if got := chunkPySrcEntries(nil); got != nil {
		t.Fatalf("chunkPySrcEntries(nil): got %+v, want nil", got)
	}
}

func TestEmitPySrcObjcopyShellinghamTailOmitsBareKvs(t *testing.T) {
	d := &ModuleData{
		tc: testToolchain(),
		pySrcs: STRS(
			"shellingham/__init__.py",
			"shellingham/_core.py",
			"shellingham/nt.py",
			"shellingham/posix/__init__.py",
			"shellingham/posix/_core.py",
			"shellingham/posix/proc.py",
			"shellingham/posix/ps.py",
		),
		pyBuildNoPY:  false,
		pyBuildNoPYC: false,
		pyTopLevel:   true,
		moduleStmt:   &ModuleStmt{Name: tokPy3Library},
	}

	entries := buildPySrcEntries(d, "contrib/python/shellingham")
	chunks := chunkPySrcEntries(entries)
	if got := len(chunks); got != 2 {
		t.Fatalf("chunks: got %d, want 2", got)
	}
	if got := len(chunks[1].kvsCmd); got != 0 {
		t.Fatalf("tail chunk kvsCmd len: got %d, want 0", got)
	}
	if got := len(chunks[1].paths); got != 1 {
		t.Fatalf("tail chunk paths len: got %d, want 1", got)
	}

	em := newBufferedEmitter()
	ctx := &GenCtx{emit: em, na: em.nodeArenas(), target: testTargetP, fs: newMemFS(nil)}
	wireTestScanners(ctx)
	instance := ModuleInstance{
		Path:     source("contrib/python/shellingham"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}
	res := emitPySrcObjcopy(ctx, instance, d, &ObjcopyEmitCtx{blocks: composeObjcopyArgBlocks(d.tc, testTargetP), na: ctx.na})
	if res == nil {
		t.Fatal("emitPySrcObjcopy returned nil")
	}

	emit := ctx.emit.(*BufferedEmitter)
	if got := len(emit.nodes); got != 2 {
		t.Fatalf("emitted nodes: got %d, want 2", got)
	}

	tail := emit.nodes[1]
	if got := tail.Outputs[0].string(); got != "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o" {
		t.Fatalf("tail output = %q, want %q", got, "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o")
	}

	wantArgs := []string{
		testToolchain().Python3.string(),
		objcopyScriptPath,
		"--compiler", testToolchain().CXX.string(),
		"--objcopy", testToolchain().Objcopy.string(),
		"--compressor", rescompressorBinPath,
		"--rescompiler", rescompilerBinPath,
		"--output_obj", "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o",
		"--target", testTargetP.Triple,
		"--inputs", "$(B)/contrib/python/shellingham/shellingham/posix/ps.py.yjsy.yapyc3",
		"--keys", "cmVzZnMvZmlsZS9weS9zaGVsbGluZ2hhbS9wb3NpeC9wcy5weS55YXB5YzM=",
	}
	gotArgs := tail.Cmds[0].CmdArgs.flat()
	if !reflect.DeepEqual(strStrs(gotArgs), wantArgs) {
		t.Fatalf("tail cmd args mismatch:\n got: %v\nwant: %v", gotArgs, wantArgs)
	}
	if contains(gotArgs, "--kvs") {
		t.Fatalf("tail cmd args unexpectedly contain --kvs: %v", gotArgs)
	}
}

func TestRootrelInputPath(t *testing.T) {
	cases := []struct {
		name   string
		kv     string
		want   string
		wantOK bool
	}{
		{
			name:   "resource_files srcKv",
			kv:     "resfs/src/resfs/file/contrib/python/pytz/py3/pytz/zoneinfo/Asia/Muscat=${rootrel;context=TEXT;input=TEXT:\"pytz/zoneinfo/Asia/Muscat\"}",
			want:   "pytz/zoneinfo/Asia/Muscat",
			wantOK: true,
		},
		{
			name:   "py_main kv (no marker)",
			kv:     "PY_MAIN=tools.x.main:main",
			want:   "",
			wantOK: false,
		},
		{
			name:   "namespace kv (no marker)",
			kv:     "py/namespace/contrib/python/pytz/py3=\"pytz.\"",
			want:   "",
			wantOK: false,
		},
		{
			name:   "malformed marker (no closing)",
			kv:     "resfs/src/x=${rootrel;context=TEXT;input=TEXT:\"pytz/zoneinfo/Asia/Muscat",
			want:   "",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := rootrelInputPath(tc.kv)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("rootrelInputPath(%q) = (%q, %v), want (%q, %v)", tc.kv, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestResolvePySrcRel_RootRelativeProto(t *testing.T) {
	fs := newMemFS(map[string]string{
		"market/idx/datacamp/proto/api/ExportMessage.proto":       "",
		"market/idx/datacamp/proto/external/ExportCategory.proto": "",
	})
	moduleDir := "market/idx/datacamp/proto/external"
	srcDirs := []VFS{dirKey(moduleDir)}

	// Root-relative proto resolves at the source root.
	got := resolvePySrcRel(fs, srcDirs, moduleDir, "market/idx/datacamp/proto/api/ExportMessage.proto")
	if want := "market/idx/datacamp/proto/api/ExportMessage.proto"; got != want {
		t.Fatalf("root-relative proto: got %s, want %s", got, want)
	}

	// A proto under the module dir resolves there.
	got = resolvePySrcRel(fs, srcDirs, moduleDir, "market/idx/datacamp/proto/external/ExportCategory.proto")
	if want := "market/idx/datacamp/proto/external/ExportCategory.proto"; got != want {
		t.Fatalf("root-relative proto under module: got %s, want %s", got, want)
	}
}

// A dirty (non-clean) srcRel must NOT be source-root bound: the fallback applies
// only to clean paths, so it falls through to the module-relative join.
func TestResolvePySrcRel_DirtyPathNotRootBound(t *testing.T) {
	fs := newMemFS(map[string]string{
		"root.proto": "",
	})
	moduleDir := "pkg/sub"
	srcDirs := []VFS{dirKey(moduleDir)}

	got := resolvePySrcRel(fs, srcDirs, moduleDir, "../root.proto")
	if want := "pkg/sub/../root.proto"; got != want {
		t.Fatalf("dirty srcRel must not source-root bind: got %s, want %s", got, want)
	}
}

func buildPySrcEntries(d *ModuleData, modulePath string) []PySrcEntry {
	return buildPySrcEntriesFor(newCodegenRegistry(), newMemFS(nil), d, modulePath, strStrings(d.pySrcs), d.pyTopLevel, d.pyNamespace)
}
