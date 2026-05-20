package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
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

// TestNoCheckImportsPathidLib2Py verifies the upstream
// `_common.pathid` derivation (build/plugins/_common.py:37): the
// lower-cased unpadded base32 of md5(value-bytes).  The pathid feeds
// the `py/no_check_imports/<pathid>=<value>` kv emitted for
// `contrib/tools/python3/lib2/py`.  Args are joined by ' ' in
// declaration order; pathid is computed on that joined string.
func TestNoCheckImportsPathidLib2Py(t *testing.T) {
	// Declaration order copied verbatim from
	// /home/pg/monorepo/yatool/contrib/tools/python3/lib2/py/ya.make:13-36.
	imports := []string{
		"_ios_support",
		"_pyrepl.*",
		"antigravity",
		"asyncio.unix_events",
		"asyncio.windows_events",
		"asyncio.windows_utils",
		"ctypes.wintypes",
		"curses.*",
		"dbm.gnu",
		"dbm.ndbm",
		"dbm.sqlite3",
		"encodings.mbcs",
		"encodings.oem",
		"lzma",
		"multiprocessing.popen_fork",
		"multiprocessing.popen_forkserver",
		"multiprocessing.popen_spawn_posix",
		"multiprocessing.popen_spawn_win32",
		"sqlite3.*",
		"turtle",
		"pty",
		"tty",
	}

	value := strings.Join(imports, " ")
	sum := md5.Sum([]byte(value))
	got := strings.TrimRight(strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:])), "=")
	want := "2fepmfaacurvvaalmzqchmko4a"
	if got != want {
		t.Fatalf("no_check_imports pathid for lib2/py: got %q, want %q", got, want)
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

// TestChunkPySrcEntriesLibInputsAggregate asserts the per-chunk `inps`
// list carries each yapyc3 entry's pathInput AND its corresponding .py
// source file from $(S), AND that the chunk-straddle entry
// (synchronize.py.3kp2.yapyc3 in Lib's first chunk) lands in BOTH the
// straddled chunks' inps lists. PR-M3-py-objcopy-aggregation reproduction:
// REF objcopy_0299ac47a... has 13 yapyc3 + 13 .py in inputs[] while
// cmd_args --inputs only carries the 12 non-straddler yapyc3s.
func TestChunkPySrcEntriesLibInputsAggregate(t *testing.T) {
	srcs := parsePySrcsTopLevel(t, "/home/pg/monorepo/yatool/contrib/tools/python3/Lib/ya.make")
	d := &moduleData{
		pySrcs:       srcs,
		pyBuildNoPY:  true,
		pyBuildNoPYC: false,
		pyTopLevel:   true,
		moduleStmt:   &ModuleStmt{Name: "PY3_LIBRARY"},
	}
	entries := buildPySrcEntries(d, "contrib/tools/python3/Lib")
	chunks := chunkPySrcEntries(entries)
	// Locate the chunk whose hash matches REF objcopy_0299ac47a...
	var found *pySrcChunk
	for i := range chunks {
		if objcopyHash(chunks[i].paths, chunks[i].keys, chunks[i].kvsHash, "contrib/tools/python3/Lib", stringPtr("PY3")) == "0299ac47a84f85e85182c986c0" {
			found = &chunks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("chunk with hash 0299ac47a... not found")
	}

	// Expected inputs[].yapyc3 (13 entries, sorted): popen_fork → synchronize.
	expectedYapyc := []string{
		"$(B)/contrib/tools/python3/Lib/multiprocessing/popen_fork.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/popen_forkserver.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/popen_spawn_posix.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/popen_spawn_win32.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/process.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/queues.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/reduction.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/resource_sharer.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/resource_tracker.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/shared_memory.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/sharedctypes.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/spawn.py.3kp2.yapyc3",
		"$(B)/contrib/tools/python3/Lib/multiprocessing/synchronize.py.3kp2.yapyc3",
	}
	expectedPy := []string{
		"$(S)/contrib/tools/python3/Lib/multiprocessing/popen_fork.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/popen_forkserver.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/popen_spawn_posix.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/popen_spawn_win32.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/process.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/queues.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/reduction.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/resource_sharer.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/resource_tracker.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/shared_memory.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/sharedctypes.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/spawn.py",
		"$(S)/contrib/tools/python3/Lib/multiprocessing/synchronize.py",
	}

	inSet := func(xs []VFS) map[string]struct{} {
		m := make(map[string]struct{}, len(xs))
		for _, x := range xs {
			m[x.String()] = struct{}{}
		}
		return m
	}
	have := inSet(found.inps)
	for _, x := range expectedYapyc {
		if _, ok := have[x]; !ok {
			t.Errorf("chunk inps missing yapyc3 %s", x)
		}
	}
	for _, x := range expectedPy {
		if _, ok := have[x]; !ok {
			t.Errorf("chunk inps missing .py source %s", x)
		}
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

// TestChunkPySrcEntriesLibByteExact asserts the chunker reproduces all 40
// `contrib/tools/python3/Lib` objcopy hashes byte-exact. PYBUILD_NO_PY +
// TOP_LEVEL: only yapyc3 entries; ~1070 HandleResource calls partitioned
// by the upstream per-call flush check.
func TestChunkPySrcEntriesLibByteExact(t *testing.T) {
	srcs := parsePySrcsTopLevel(t, "/home/pg/monorepo/yatool/contrib/tools/python3/Lib/ya.make")
	if len(srcs) < 100 {
		t.Fatalf("Lib PY_SRCS sources: got %d, want >=100", len(srcs))
	}

	d := &moduleData{
		pySrcs:       srcs,
		pyBuildNoPY:  true,
		pyBuildNoPYC: false,
		pyTopLevel:   true,
		moduleStmt:   &ModuleStmt{Name: "PY3_LIBRARY"},
	}
	entries := buildPySrcEntries(d, "contrib/tools/python3/Lib")
	if got := len(entries); got != len(srcs) {
		t.Fatalf("entries: got %d, want %d (yapyc3-only)", got, len(srcs))
	}

	chunks := chunkPySrcEntries(entries)
	if got := len(chunks); got != 40 {
		t.Fatalf("chunks: got %d, want 40", got)
	}

	got := make(map[string]bool, len(chunks))
	for _, ch := range chunks {
		got[objcopyHash(ch.paths, ch.keys, ch.kvsHash, "contrib/tools/python3/Lib", stringPtr("PY3"))] = true
	}
	want := []string{
		"0299ac47a84f85e85182c986c0", "05979015660e1af70ece36165c",
		"11c61d8656927e1fccc4212ada", "1202ed6a0e8e3b2d359cb6d3e9",
		"1c71017d56edd49dcb2fb1088b", "21b20daa2b50d85b05965bdc75",
		"27da8af7ae3ae340fcda08860f", "2ceb19c23d17d75ac5ca165f20",
		"2fc8b4197f65a787bdd2597408", "30464705211b7c791d23eed930",
		"32686b3f020463c86f23f2a829", "4af385fc380e1af7ebf895736d",
		"5175eb2f01e92afe893c2bf4c3", "531c86b56da731fb80e24240a6",
		"55c0325e1249d4958ca205a6b2", "5d72aa577098be7ceba340cf85",
		"7144745455d80aefa950e10598", "73223fa093c3296e5aeadc102e",
		"740a86012a279af14779c6856b", "8326aa293d5a5fd19532e2bafe",
		"83c2bc2455632eec41a952064c", "8589214338039c8242f06d7314",
		"8c333af33364075ce74eff17b7", "91d960b29f1a0bf036aa5b385a",
		"94fec750f6169bdf2e38d578a6", "967503cafc8921aeb7db2668bf",
		"9b8efcce0b985d2b0f68128cd4", "a3583880282ad020aa2bc04e6c",
		"a5d68f9819f515e4dd6b416a17", "abdebce1fa345366c70dcfd146",
		"b022af5d9f8bbb9471f30e866f", "b64feb08a40c6c2a17e2cfb6b9",
		"be0d7f509c8e5f6acd543f9179", "d7c38a0cf645a73ba16d2c454d",
		"ddc61536e781b25eb66c0659c2", "e038ca7b8e62ed332a4089ac4c",
		"ea85523f41943a2463f563fb3c", "f013e8aed589f96a377abab1a3",
		"f420a5e6d68d30965a190cb303", "fc7b70c76a3cced7a694cc68d2",
	}
	for _, h := range want {
		if !got[h] {
			t.Errorf("REF hash %q not produced by chunker", h)
		}
	}
}

// TestChunkPySrcEntriesLib2PyByteExact asserts the chunker reproduces all
// 37 PY_SRCS-derived `contrib/tools/python3/lib2/py` objcopy hashes
// byte-exact. PYBUILD_NO_PYC + TOP_LEVEL: only raw .py entries. REF carries
// 38 unique hashes for this module; the extra one is the
// `py/no_check_imports/...` kv-only chunk emitted by emitNoCheckImportsObjcopy.
func TestChunkPySrcEntriesLib2PyByteExact(t *testing.T) {
	srcs := parsePySrcsTopLevel(t, "/home/pg/monorepo/yatool/contrib/tools/python3/lib2/py/ya.make")
	if len(srcs) < 100 {
		t.Fatalf("lib2/py PY_SRCS sources: got %d, want >=100", len(srcs))
	}

	d := &moduleData{
		pySrcs:       srcs,
		pyBuildNoPY:  false,
		pyBuildNoPYC: true,
		pyTopLevel:   true,
		moduleStmt:   &ModuleStmt{Name: "PY3_LIBRARY"},
	}
	entries := buildPySrcEntries(d, "contrib/tools/python3/lib2/py")
	if got := len(entries); got != len(srcs) {
		t.Fatalf("entries: got %d, want %d (raw-only)", got, len(srcs))
	}

	chunks := chunkPySrcEntries(entries)
	if got := len(chunks); got != 37 {
		t.Fatalf("chunks: got %d, want 37", got)
	}

	got := make(map[string]bool, len(chunks))
	for _, ch := range chunks {
		got[objcopyHash(ch.paths, ch.keys, ch.kvsHash, "contrib/tools/python3/lib2/py", stringPtr("PY3"))] = true
	}
	want := []string{
		"08e492c72f789cad4a22ae67dd", "167456915a92189129fdafa2de",
		"16e5ce133e9775c2c148c9231b", "179953ace18d40e8153f1fb874",
		"23641f71141ebf2d5af7fbbe0c", "2f1ef348f3028b8ea83c753021",
		"2fdb00ea1502e08b07bc82c4f8", "3d4a3b1f2b375c3e6499a4f84b",
		"41773fc867bf716c6ce06c1a20", "42d2e9662f9f01ffe9c728ab8c",
		"573633c1d2393a8524ddffa962", "5acb90cb752582d16fec5bd72c",
		"6935af4a041ecc793e86ca8057", "71402f9072f6d0782f2691f74a",
		"781c564bd80d409d66b74539ac", "89b5fc40101299792f1f67fabe",
		"8e53cf53a209227ce0d4926427", "8fc893e1520d8e0823ab4d734c",
		"94088e7d14c554b1f6be3d26de", "9e9ab965bc76a44907df4e61de",
		"a8581a1d236599e2e3a0d545a2", "aecc3d79b064a6a2e2435c7825",
		"aedb0d7f2d2bc113662e333589", "b1b86cdfed1b49b9beed002616",
		"c4538c66ee54d0482932722b72", "d21e4223ab7183654db952a37c",
		"d24b9e2c30aa8dca75ddd87463", "d3a3e0d31624153b2c0b6fe21d",
		"d468b91b4e8e4881e0e74339e4", "e08409807ec134b5669d24886e",
		"e916b14a4bc0f9c00963cebc62", "eb4086c4f3447232347cb9ecfa",
		"ef01b616e685d2bcb829871002", "f58682abe69e949f5b47310a50",
		"f7b6999469ac522538f867a66a", "f7c4a8009081e5419a8527652e",
		"f919532da43f6bb6835b033be6",
	}
	missing := 0
	for _, h := range want {
		if !got[h] {
			missing++
			t.Errorf("REF hash %q not produced by chunker", h)
		}
	}
	if missing > 0 {
		t.Fatalf("%d REF hashes missing from chunker output", missing)
	}
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
