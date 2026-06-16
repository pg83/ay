package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"regexp"
	"sort"
	"strings"
)

var (
	objcopyScriptPath    = objcopyScriptVFS.string()
	rescompressorBinPath = rescompressorBinVFS.string()
	rescompilerBinPath   = rescompilerBinVFS.string()
	yaConfFormulaRE      = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
	// A simple_tools `"resource": "<name>"` entry (no formula, e.g. yq) maps to
	// build/external_resources/<name>/resources.json — embedded like a formula.
	yaConfResourceRE = regexp.MustCompile(`"resource"\s*:\s*"([^"]+)"`)
)

const hashLen = 26

const rootCmdLen = 200

const maxCmdLen = 8000

func objcopyHash(paths []string, keysB64 []string, kvs []string, unitPath string, moduleTag *string) string {
	list := make([]string, 0, len(paths)+len(keysB64)+len(kvs)+1)
	list = append(list, paths...)
	list = append(list, keysB64...)
	list = append(list, kvs...)
	list = append(list, "$S/"+unitPath)

	sort.Strings(list)

	stringify := strings.Join(list, ",")

	if moduleTag != nil {
		stringify += *moduleTag
	}

	sum := md5.Sum([]byte(stringify))

	return strings.ToLower(enchex.EncodeToString(sum[:]))[:hashLen]
}

func expandResourceFiles(args []string) []ResourceEntry {
	prefix := ""
	prefixToStrip := ""
	dest := ""

	out := make([]ResourceEntry, 0, len(args))
	i := 0

	for i < len(args) {
		tok := args[i]

		switch tok {
		case "DONT_COMPRESS":
			i++
		case "PREFIX":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: PREFIX is the last token; expected a prefix value")
			}

			prefix = args[i+1]
			dest = ""
			i += 2
		case "DEST":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: DEST is the last token; expected a dest value")
			}

			dest = args[i+1]
			prefix = ""
			i += 2
		case "STRIP":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: STRIP is the last token; expected a prefix-to-strip value")
			}

			prefixToStrip = args[i+1]
			i += 2
		default:
			path := tok
			i++

			keyTail := path

			if prefixToStrip != "" && strings.HasPrefix(keyTail, prefixToStrip) {
				keyTail = keyTail[len(prefixToStrip):]
			}

			var computedKey string

			if dest != "" {
				computedKey = dest
			} else {
				computedKey = prefix + keyTail
			}

			fileKey := "resfs/file/" + computedKey
			srcKv := "resfs/src/" + fileKey + "=${rootrel;context=TEXT;input=TEXT:\"" + path + "\"}"
			out = append(out, ResourceEntry{Path: "-", Key: srcKv})
			out = append(out, ResourceEntry{Path: path, Key: fileKey})
		}
	}

	return out
}

func resourceModuleTag(modName TOK) *string {
	switch modName {
	case tokPy3Library, tokPy3ProgramBin, tokPy23Library, tokPy23NativeLibrary:
		return stringPtr("PY3")
	}

	return nil
}

// resourceBinTagForData returns the MODULE_TAG used by PROGRAM-side resource
// objcopy emissions (PY_MAIN, NO_CHECK_IMPORTS). For PY3_PROGRAM that's
// "PY3_BIN" — upstream's PY3_BIN submodule has MODULE_TAG=PY3_BIN auto-set by
// lang/confreader.cpp:847-848 since _PY_PROGRAM() doesn't override it (unlike
// _ARCADIA_PYTHON3_ADDINCL which sets PY3; the submodule-name default is set
// AFTER body execution, so it wins).
func resourceBinTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program {
		return stringPtr("PY3_BIN")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

// resourceLibTagForData returns the MODULE_TAG used by LIBRARY-side resource
// objcopy emissions (RESOURCE/RESOURCE_FILES, pysrc bytecode, py/namespace).
// For PY3_PROGRAM's KindLib twin (the PY3_BIN_LIB submodule) it's
// "PY3_BIN_LIB"; for real PY3_LIBRARY etc. it's "PY3". For the PY3_PROGRAM
// PROGRAM-side path (when the LIBRARY-twin has already emitted the same
// objcopy), returning "PY3_BIN_LIB" lets Emitter dedup the duplicate so the
// PROGRAM's LD reuses the LIBRARY's emission rather than producing a tagged
// twin with a non-REF hash.
func resourceLibTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program || d.programPairedLib {
		return stringPtr("PY3_BIN_LIB")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

func expandRootrel(kv string, unitPath string) string {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""
	idx := strings.Index(kv, marker)

	if idx < 0 {
		return kv
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")

	if end < 0 {
		return kv
	}

	innerPath := tail[:end]
	expanded := unitPath + "/" + innerPath

	return kv[:idx] + expanded + tail[end+len("\"}"):]
}

func rootrelInputPath(kv string) (string, bool) {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""
	idx := strings.Index(kv, marker)

	if idx < 0 {
		return "", false
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")

	if end < 0 {
		return "", false
	}

	return tail[:end], true
}

func yaConfFormulaResources(fs FS, confPath string) []string {
	raw := fs.read(confPath)

	var out []string
	seen := map[string]struct{}{}

	for _, m := range yaConfFormulaRE.FindAllSubmatch(raw, -1) {
		formula := string(m[1])

		if _, dup := seen[formula]; dup {
			continue
		}

		seen[formula] = struct{}{}
		out = append(out, formula)
	}

	// A "resource": "<name>" entry without its own formula (e.g. yq) embeds
	// build/external_resources/<name>/resources.json when that file exists.
	for _, m := range yaConfResourceRE.FindAllSubmatch(raw, -1) {
		path := "build/external_resources/" + string(m[1]) + "/resources.json"

		if _, dup := seen[path]; dup {
			continue
		}

		if !fs.isFile(srcRootVFS, path) {
			continue
		}

		seen[path] = struct{}{}
		out = append(out, path)
	}

	return out
}

type PySrcEntry struct {
	pathHash  string
	pathInput VFS
	key       string
	kvHash    string
	kvCmd     string
	inputDep  VFS

	extraSrcInput *VFS
}

// resolvePySrcRel searches the SRCDIR path (reverse, later declaration wins) for
// srcRel and returns the resolved source rel; the module dir is the fallback —
// the same search resolveSourceVFS does for SRCS.
func resolvePySrcRel(fs FS, srcDirs []VFS, modulePath, srcRel string) string {
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if fs.isFile(srcDirs[i], srcRel) {
			return srcDirs[i].rel() + "/" + srcRel
		}
	}

	return modulePath + "/" + srcRel
}

func buildPySrcEntriesFor(reg *CodegenRegistry, fs FS, d *ModuleData, modulePath string, srcs []string, topLevel bool, namespace *STR) []PySrcEntry {
	if len(srcs) == 0 {
		return nil
	}

	keyPrefix := ""

	if !topLevel {
		if namespace != nil {
			keyPrefix = strings.ReplaceAll(strings.TrimSuffix(namespace.string(), "."), ".", "/") + "/"
		} else {
			keyPrefix = modulePath + "/"
		}
	}

	out := make([]PySrcEntry, 0, len(srcs)*2)

	for _, srcRel := range srcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		// A build-generated .py (e.g. SWIG output) is resourced by
		// emitGeneratedPyAuxChunks, not here.
		if reg.lookupSplit(dirKey(modulePath), internStr(srcRel)) != nil {
			continue
		}

		suffix := ".yapyc3"

		if strings.Contains(srcRel, "/") {
			suffix = "." + pySrcYapycSuffix(modulePath) + ".yapyc3"
		}

		// SRCDIR is a source-search path, not a module relocation: keys stay
		// srcRel-based, only the source-side path resolves through srcDirs.
		resolvedRel := resolvePySrcRel(fs, d.srcDirs, modulePath, srcRel)

		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyPathInput := source(resolvedRel)
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + resolvedRel

			out = append(out, PySrcEntry{
				pathHash:  srcRel,
				pathInput: pyPathInput,
				key:       pyKey,
				kvHash:    pyKvHash,
				kvCmd:     pyKvCmd,
				inputDep:  pyPathInput,
			})
		}

		if !d.pyBuildNoPYC {
			ypKey := "resfs/file/py/" + keyPrefix + srcRel + ".yapyc3"
			ypPathInput := build(modulePath + "/" + srcRel + suffix)

			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix
			extraSrcInput := vfsPtr(source(resolvedRel))

			out = append(out, PySrcEntry{
				pathHash:      srcRel + suffix,
				pathInput:     ypPathInput,
				key:           ypKey,
				kvHash:        ypKvHash,
				kvCmd:         ypKvCmd,
				inputDep:      ypPathInput,
				extraSrcInput: extraSrcInput,
			})
		}
	}

	return out
}

type PySrcChunk struct {
	paths    []string
	keys     []string
	kvsHash  []string
	kvsCmd   []string
	pathInps []VFS

	inps []VFS
}

func chunkPySrcEntries(entries []PySrcEntry) []PySrcChunk {
	if len(entries) == 0 {
		return nil
	}

	chunks := make([]PySrcChunk, 0)
	cur := PySrcChunk{}
	cmdLen := 0
	deduper.reset()
	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = PySrcChunk{}
		cmdLen = 0
		deduper.reset()
	}

	addInps := func(e PySrcEntry) {
		if deduper.add(e.pathInput) {
			cur.inps = append(cur.inps, e.pathInput)
		}

		if e.extraSrcInput == nil {
			return
		}

		if deduper.add(*e.extraSrcInput) {
			cur.inps = append(cur.inps, *e.extraSrcInput)
		}
	}

	for _, e := range entries {
		cur.kvsHash = append(cur.kvsHash, e.kvHash)
		cur.kvsCmd = append(cur.kvsCmd, e.kvCmd)
		addInps(e)
		cmdLen += rootCmdLen + len(e.kvHash)

		if cmdLen >= maxCmdLen {
			flush()
		}

		kb64 := encb64.StdEncoding.EncodeToString([]byte(e.key))
		cur.paths = append(cur.paths, e.pathHash)
		cur.keys = append(cur.keys, kb64)
		cur.pathInps = append(cur.pathInps, e.pathInput)
		addInps(e)
		cmdLen += rootCmdLen + len(e.pathHash) + len(kb64)

		if cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	return chunks
}

func pySrcYapycSuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}
