package main

import (
	"crypto/md5"
	"encoding/base64"
	enchex "encoding/hex"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestObjcopyHashCerts verifies the upstream
// `TObjCopyResourcePacker::GetHashForOutput` derivation against the
// REF sg2.json sample for the certs/ module's RESOURCE invocation.
// Worked example documented in
// `docs/drafts/20260511-2200-pr-resource-objcopy-research.md`.
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

// TestObjcopyHashRapidjson verifies the hash derivation for the
// `devtools/ymake/contrib/python-rapidjson` RESOURCE_FILES expansion
// — the keys are base64-padded `resfs/file/...` strings, the kvs
// preserve the literal `${rootrel;context=TEXT;input=TEXT:"..."}`
// placeholder, and MODULE_TAG is "PY3" for PY3_LIBRARY.
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

// TestExpandResourceFilesRapidjson verifies the upstream
// `build/plugins/res.py:onresource_files` expansion against the same
// rapidjson REF sample.  PREFIX is folded into the per-path key; the
// resulting pair list is consumed by emitResourceObjcopy and feeds
// the hash test above.
func TestExpandResourceFilesRapidjson(t *testing.T) {
	args := []string{
		"PREFIX", "devtools/ymake/contrib/python-rapidjson/",
		".dist-info/METADATA",
		".dist-info/top_level.txt",
		"rapidjson/license.txt",
		"rapidjson/readme.md",
	}

	got := expandResourceFiles(args)

	want := []resourceEntry{
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

// TestPyNamespaceModListMD5RuntimePy3 verifies the upstream
// `pybuild.py:560` streaming-md5 derivation against REF sg2.json for the
// library/python/runtime_py3 PY3_LIBRARY: PY_SRCS(entry_points.py) with
// the default `ns = upath_dotted + '.'`. The resulting hex digest is
// the `<md5>` slot of the `py/namespace/<md5>/...` resource key.
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

// TestPyNamespaceModListMD5SymbolsModule verifies the same derivation
// for library/python/symbols/module (PY23_LIBRARY, PY_SRCS(__init__.py)).
// Confirms that __init__.py keeps the `__init__` literal in the dotted
// mod name (pybuild.py strips the `.py` extension but does not collapse
// the package-init suffix).
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

// TestPyNamespaceModListMD5Py3ccSlow verifies the same derivation for
// tools/py3cc/slow (PY3_PROGRAM_BIN via INCLUDE(bin/ya.make),
// PY_SRCS(MAIN main.py)).  Establishes that the MAIN modifier does
// not change the mod_name (only emits an additional PY_MAIN= kv).
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

// TestPyNamespaceObjcopyHashRuntimePy3 verifies the kv_only objcopy
// hash for library/python/runtime_py3 against REF:
//
//	output: $(B)/library/python/runtime_py3/objcopy_3b0561f75631281b973aa8b64e.o
//	kv (hash, quoted):    py/namespace/<md5>/<path>="<ns>"
//	kv (cmd_args, unquoted): py/namespace/<md5>/<path>=<ns>
//
// PY3_LIBRARY → MODULE_TAG = "PY3". The hash uses the quoted form per
// pybuild.py:593; cmd_args uses the unquoted form (RUN_PYTHON3 template
// strips the outer quotes).
func TestPyNamespaceObjcopyHashRuntimePy3(t *testing.T) {
	kv := `py/namespace/bd17cfe3d9af11d01ff7b15ebc3786a7/library/python/runtime_py3="library.python.runtime_py3."`

	got := objcopyHash(nil, nil, []string{kv}, "library/python/runtime_py3", stringPtr("PY3"))
	want := "3b0561f75631281b973aa8b64e"
	if got != want {
		t.Fatalf("runtime_py3 namespace objcopy hash: got %q, want %q", got, want)
	}
}

// TestNoCheckImportsObjcopyHashLib2Py verifies the kv_only objcopy
// hash for contrib/tools/python3/lib2/py against REF:
//
//	output: $(B)/contrib/tools/python3/lib2/py/objcopy_cd47bcaec327e5eb9db4641ec8.o
//	kv (hash):    py/no_check_imports/<pathid>="<value>"
//
// PY3_LIBRARY (with ENABLE(PYBUILD_NO_PYC)) → MODULE_TAG = "PY3".
func TestNoCheckImportsObjcopyHashLib2Py(t *testing.T) {
	value := "_ios_support _pyrepl.* antigravity asyncio.unix_events asyncio.windows_events asyncio.windows_utils ctypes.wintypes curses.* dbm.gnu dbm.ndbm dbm.sqlite3 encodings.mbcs encodings.oem lzma multiprocessing.popen_fork multiprocessing.popen_forkserver multiprocessing.popen_spawn_posix multiprocessing.popen_spawn_win32 sqlite3.* turtle pty tty"
	kv := `py/no_check_imports/2fepmfaacurvvaalmzqchmko4a="` + value + `"`

	got := objcopyHash(nil, nil, []string{kv}, "contrib/tools/python3/lib2/py", stringPtr("PY3"))
	want := "cd47bcaec327e5eb9db4641ec8"
	if got != want {
		t.Fatalf("contrib/tools/python3/lib2/py no_check_imports objcopy hash: got %q, want %q", got, want)
	}
}

// TestPyMainObjcopyHashPy3ccSlow verifies the kv_only objcopy hash for
// tools/py3cc/slow's PY_MAIN= kv against REF:
//
//	output: $(B)/tools/py3cc/slow/objcopy_4b1c18d0dc6973976969ad23be.o
//	kv:     PY_MAIN=tools.py3cc.slow.main:main
//
// PY3_PROGRAM_BIN → MODULE_TAG = "PY3".
func TestPyMainObjcopyHashPy3ccSlow(t *testing.T) {
	kv := "PY_MAIN=tools.py3cc.slow.main:main"

	got := objcopyHash(nil, nil, []string{kv}, "tools/py3cc/slow", stringPtr("PY3"))
	want := "4b1c18d0dc6973976969ad23be"
	if got != want {
		t.Fatalf("tools/py3cc/slow PY_MAIN objcopy hash: got %q, want %q", got, want)
	}
}

// TestPySrcObjcopyHashRuntimePy3RawEntryPoints verifies the objcopy hash
// for the single-entry raw `.py` chunk of library/python/runtime_py3.
// PYBUILD_NO_PYC is on, namespace is non-TOP_LEVEL (default upath
// prefix). REF:
//
//	output: $(B)/library/python/runtime_py3/objcopy_84a3659770bdea15f8ae77837d.o
//	key:    resfs/file/py/library/python/runtime_py3/entry_points.py
//	kv:     resfs/src/<key>=${rootrel;context=TEXT;input=TEXT:"entry_points.py"}
//	paths:  [entry_points.py]   (the raw PY_SRCS argument, not srcRel+suffix)
//
// The hash uses the placeholder kv form; cmd_args use the expanded form.
func TestPySrcObjcopyHashRuntimePy3RawEntryPoints(t *testing.T) {
	d := &moduleData{
		pySrcs:       []string{"entry_points.py"},
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: "PY3_LIBRARY"},
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

// TestPySrcObjcopyHashPy3ccSlowMain verifies the single-entry raw .py
// chunk for tools/py3cc/slow (PY3_PROGRAM_BIN, PYBUILD_NO_PYC, MAIN main.py).
// REF: objcopy_c3a5182796bc68c054c676bcc0.o
func TestPySrcObjcopyHashPy3ccSlowMain(t *testing.T) {
	d := &moduleData{
		pySrcs:       []string{"main.py"},
		pyBuildNoPYC: true,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: "PY3_PROGRAM_BIN"},
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

// TestPySrcObjcopyHashSymbolsModuleDualEntry verifies the dual-entry
// (raw .py + .yapyc3) chunk for library/python/symbols/module.
// PY23_LIBRARY (MODULE_TAG=PY3), no PYBUILD_NO_*, NOT TOP_LEVEL.
// REF: objcopy_c325f0009e9625395005936d90.o
func TestPySrcObjcopyHashSymbolsModuleDualEntry(t *testing.T) {
	d := &moduleData{
		pySrcs:       []string{"__init__.py"},
		pyBuildNoPYC: false,
		pyBuildNoPY:  false,
		pyTopLevel:   false,
		moduleStmt:   &ModuleStmt{Name: "PY23_LIBRARY"},
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

// TestChunkPySrcEntriesEmptyReturnsNil ensures the chunker degenerates
// cleanly on empty input — no allocations, returns nil. PR-M3-resource-
// objcopy-C guard for modules where PYBUILD_NO_PY + PYBUILD_NO_PYC are
// both set (an unobserved combo that produces zero entries).
func TestChunkPySrcEntriesEmptyReturnsNil(t *testing.T) {
	if got := chunkPySrcEntries(nil); got != nil {
		t.Fatalf("chunkPySrcEntries(nil): got %+v, want nil", got)
	}
}

// parsePySrcsTopLevel pulls the `.py` source list out of a `PY_SRCS(TOP_LEVEL ...)`
// block in an upstream ya.make. PR-M3-resource-objcopy-chunker-precision
// uses this to feed the actual contrib/tools/python3 source lists into the
// chunker for byte-exact validation against the REF graph.
func parsePySrcsTopLevel(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	re := regexp.MustCompile(`(?s)PY_SRCS\(\s*TOP_LEVEL\s*(.*?)\)`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatalf("no PY_SRCS(TOP_LEVEL ...) block in %s", path)
	}
	out := make([]string, 0, 600)
	for _, line := range strings.Split(string(m[1]), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasSuffix(s, ".py") {
			out = append(out, s)
		}
	}
	return out
}

func TestEmitPySrcObjcopyShellinghamTailOmitsBareKvs(t *testing.T) {
	d := &moduleData{
		pySrcs: []string{
			"shellingham/__init__.py",
			"shellingham/_core.py",
			"shellingham/nt.py",
			"shellingham/posix/__init__.py",
			"shellingham/posix/_core.py",
			"shellingham/posix/proc.py",
			"shellingham/posix/ps.py",
		},
		pyBuildNoPY:  false,
		pyBuildNoPYC: false,
		pyTopLevel:   true,
		moduleStmt:   &ModuleStmt{Name: "PY3_LIBRARY"},
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

	ctx := &genCtx{emit: NewBufferedEmitter()}
	instance := ModuleInstance{
		Path:     "contrib/python/shellingham",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}
	res := emitPySrcObjcopy(ctx, instance, d, NodeRef{}, NodeRef{})
	if res == nil {
		t.Fatal("emitPySrcObjcopy returned nil")
	}

	emit := ctx.emit.(*BufferedEmitter)
	if got := len(emit.nodes); got != 2 {
		t.Fatalf("emitted nodes: got %d, want 2", got)
	}

	tail := emit.nodes[1]
	if got := tail.Outputs[0].String(); got != "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o" {
		t.Fatalf("tail output = %q, want %q", got, "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o")
	}

	wantArgs := []string{
		testTargetP.Tools.Python3,
		objcopyScriptPath,
		"--compiler", testTargetP.Tools.CXX,
		"--objcopy", testTargetP.Tools.Objcopy,
		"--compressor", rescompressorBinPath,
		"--rescompiler", rescompilerBinPath,
		"--output_obj", "$(B)/contrib/python/shellingham/objcopy_e79ae9e993a07f847435dcf3c2.o",
		"--target", testTargetP.Triple,
		"--inputs", "$(B)/contrib/python/shellingham/shellingham/posix/ps.py.yjsy.yapyc3",
		"--keys", "cmVzZnMvZmlsZS9weS9zaGVsbGluZ2hhbS9wb3NpeC9wcy5weS55YXB5YzM=",
	}
	gotArgs := tail.Cmds[0].CmdArgs
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("tail cmd args mismatch:\n got: %v\nwant: %v", gotArgs, wantArgs)
	}
	if contains(gotArgs, "--kvs") {
		t.Fatalf("tail cmd args unexpectedly contain --kvs: %v", gotArgs)
	}
}

// TestRootrelInputPath pins the extractor that recovers the input=TEXT
// path P from a `resfs/src/...=${rootrel;context=TEXT;input=TEXT:"P"}`
// kv. emitResourceObjcopy folds P into a chunk-straddle node's inputs[]
// (the upstream input=TEXT semantics), so the extractor must return P for
// RESOURCE_FILES srcKvs and (",false) for every marker-less / malformed kv.
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
