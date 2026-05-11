package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	"encoding/base64"
	enchex "encoding/hex"
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
	moduleTag := ""

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

	got := objcopyHash(paths, keysB64, kvs, "devtools/ymake/contrib/python-rapidjson", "PY3")
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
//	output: $(BUILD_ROOT)/library/python/runtime_py3/objcopy_3b0561f75631281b973aa8b64e.o
//	kv (hash, quoted):    py/namespace/<md5>/<path>="<ns>"
//	kv (cmd_args, unquoted): py/namespace/<md5>/<path>=<ns>
//
// PY3_LIBRARY → MODULE_TAG = "PY3". The hash uses the quoted form per
// pybuild.py:593; cmd_args uses the unquoted form (RUN_PYTHON3 template
// strips the outer quotes).
func TestPyNamespaceObjcopyHashRuntimePy3(t *testing.T) {
	kv := `py/namespace/bd17cfe3d9af11d01ff7b15ebc3786a7/library/python/runtime_py3="library.python.runtime_py3."`

	got := objcopyHash(nil, nil, []string{kv}, "library/python/runtime_py3", "PY3")
	want := "3b0561f75631281b973aa8b64e"
	if got != want {
		t.Fatalf("runtime_py3 namespace objcopy hash: got %q, want %q", got, want)
	}
}

// TestNoCheckImportsObjcopyHashLib2Py verifies the kv_only objcopy
// hash for contrib/tools/python3/lib2/py against REF:
//
//	output: $(BUILD_ROOT)/contrib/tools/python3/lib2/py/objcopy_cd47bcaec327e5eb9db4641ec8.o
//	kv (hash):    py/no_check_imports/<pathid>="<value>"
//
// PY3_LIBRARY (with ENABLE(PYBUILD_NO_PYC)) → MODULE_TAG = "PY3".
func TestNoCheckImportsObjcopyHashLib2Py(t *testing.T) {
	value := "_ios_support _pyrepl.* antigravity asyncio.unix_events asyncio.windows_events asyncio.windows_utils ctypes.wintypes curses.* dbm.gnu dbm.ndbm dbm.sqlite3 encodings.mbcs encodings.oem lzma multiprocessing.popen_fork multiprocessing.popen_forkserver multiprocessing.popen_spawn_posix multiprocessing.popen_spawn_win32 sqlite3.* turtle pty tty"
	kv := `py/no_check_imports/2fepmfaacurvvaalmzqchmko4a="` + value + `"`

	got := objcopyHash(nil, nil, []string{kv}, "contrib/tools/python3/lib2/py", "PY3")
	want := "cd47bcaec327e5eb9db4641ec8"
	if got != want {
		t.Fatalf("contrib/tools/python3/lib2/py no_check_imports objcopy hash: got %q, want %q", got, want)
	}
}

// TestPyMainObjcopyHashPy3ccSlow verifies the kv_only objcopy hash for
// tools/py3cc/slow's PY_MAIN= kv against REF:
//
//	output: $(BUILD_ROOT)/tools/py3cc/slow/objcopy_4b1c18d0dc6973976969ad23be.o
//	kv:     PY_MAIN=tools.py3cc.slow.main:main
//
// PY3_PROGRAM_BIN → MODULE_TAG = "PY3".
func TestPyMainObjcopyHashPy3ccSlow(t *testing.T) {
	kv := "PY_MAIN=tools.py3cc.slow.main:main"

	got := objcopyHash(nil, nil, []string{kv}, "tools/py3cc/slow", "PY3")
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
	// /home/pg/monorepo/yatool_orig/contrib/tools/python3/lib2/py/ya.make:13-36.
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
