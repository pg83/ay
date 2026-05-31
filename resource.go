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
	objcopyScriptVFS     = Intern("$(S)/build/scripts/objcopy.py")
	objcopyScriptPath    = objcopyScriptVFS.String()
	rescompressorBinVFS  = Intern("$(B)/tools/rescompressor/rescompressor")
	rescompilerBinVFS    = Intern("$(B)/tools/rescompiler/rescompiler")
	rescompressorBinPath = rescompressorBinVFS.String()
	rescompilerBinPath   = rescompilerBinVFS.String()
	yaConfFormulaRE      = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
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

func expandResourceFiles(args []string) []resourceEntry {
	prefix := ""
	prefixToStrip := ""
	dest := ""

	out := make([]resourceEntry, 0, len(args))
	i := 0

	for i < len(args) {
		tok := args[i]

		switch tok {
		case "DONT_COMPRESS":
			i++
		case "PREFIX":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: PREFIX is the last token; expected a prefix value")
			}

			prefix = args[i+1]
			dest = ""
			i += 2
		case "DEST":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: DEST is the last token; expected a dest value")
			}

			dest = args[i+1]
			prefix = ""
			i += 2
		case "STRIP":
			if i+1 >= len(args) {
				ThrowFmt("RESOURCE_FILES: STRIP is the last token; expected a prefix-to-strip value")
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
			out = append(out, resourceEntry{Path: "-", Key: srcKv})
			out = append(out, resourceEntry{Path: path, Key: fileKey})
		}
	}

	return out
}

func resourceModuleTag(modName string) *string {
	switch modName {
	case "PY3_LIBRARY", "PY3_PROGRAM_BIN", "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
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
func resourceBinTagForData(d *moduleData) *string {
	if d == nil || d.moduleStmt == nil {
		return nil
	}

	if d.moduleStmt.Name == "PY3_PROGRAM" {
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
func resourceLibTagForData(d *moduleData) *string {
	if d == nil || d.moduleStmt == nil {
		return nil
	}

	if d.moduleStmt.Name == "PY3_PROGRAM" || d.programPairedLib {
		return stringPtr("PY3_BIN_LIB")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

func prResourceExtraInputs(d *moduleData, output string) []VFS {
	if d == nil || d.prOutputInputs == nil {
		return nil
	}

	inputs := d.prOutputInputs[output]
	out := make([]VFS, 0, len(inputs))

	for _, p := range inputs {
		if p.IsSource() {
			out = append(out, p)
		}
	}

	return out
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
	raw := fs.Read(confPath)

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

	return out
}

type pySrcEntry struct {
	pathHash  string
	pathInput VFS
	key       string
	kvHash    string
	kvCmd     string
	inputDep  VFS

	extraSrcInput *VFS
}

func buildPySrcEntries(d *moduleData, modulePath string) []pySrcEntry {
	return buildPySrcEntriesFor(d, modulePath, d.pySrcs, d.pyTopLevel, d.pyNamespace)
}

func buildPySrcEntriesFor(d *moduleData, modulePath string, srcs []string, topLevel bool, namespace *string) []pySrcEntry {
	if len(srcs) == 0 {
		return nil
	}

	actualUnit := modulePath

	if d.srcDir != nil {
		actualUnit = *d.srcDir
	}

	keyPrefix := ""

	if !topLevel {
		if namespace != nil {
			keyPrefix = strings.ReplaceAll(strings.TrimSuffix(*namespace, "."), ".", "/") + "/"
		} else {
			keyPrefix = modulePath + "/"
		}
	}

	out := make([]pySrcEntry, 0, len(srcs)*2)

	for _, srcRel := range srcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		if d.pyGeneratedSrcs[srcRel] != nil {
			continue
		}

		suffix := ".yapyc3"

		if strings.Contains(srcRel, "/") {
			suffix = "." + pySrcYapycSuffix(modulePath) + ".yapyc3"
		}

		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyPathInput := Source(actualUnit + "/" + srcRel)

			if d.pyGeneratedSrcs[srcRel] != nil {
				pyPathInput = Build(modulePath + "/" + srcRel)
			}

			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + actualUnit + "/" + srcRel

			if d.pyGeneratedSrcs[srcRel] != nil {
				pyKvCmd = "resfs/src/" + pyKey + "=" + modulePath + "/" + srcRel
			}

			out = append(out, pySrcEntry{
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
			ypPathInput := Build(modulePath + "/" + srcRel + suffix)

			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix
			extraSrcInput := vfsPtr(Source(actualUnit + "/" + srcRel))

			if d.pyGeneratedSrcs[srcRel] != nil {
				ypKvCmd = "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix
				extraSrcInput = vfsPtr(Build(modulePath + "/" + srcRel))
			}

			out = append(out, pySrcEntry{
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

type pySrcChunk struct {
	paths    []string
	keys     []string
	kvsHash  []string
	kvsCmd   []string
	pathInps []string

	inps []VFS
}

func chunkPySrcEntries(entries []pySrcEntry) []pySrcChunk {
	if len(entries) == 0 {
		return nil
	}

	chunks := make([]pySrcChunk, 0)
	cur := pySrcChunk{}
	cmdLen := 0
	inpsSeen := make(map[VFS]struct{})
	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = pySrcChunk{}
		cmdLen = 0
		inpsSeen = make(map[VFS]struct{})
	}

	addInps := func(e pySrcEntry) {
		if _, ok := inpsSeen[e.pathInput]; !ok {
			cur.inps = append(cur.inps, e.pathInput)
			inpsSeen[e.pathInput] = struct{}{}
		}

		if e.extraSrcInput == nil {
			return
		}

		if _, ok := inpsSeen[*e.extraSrcInput]; !ok {
			cur.inps = append(cur.inps, *e.extraSrcInput)
			inpsSeen[*e.extraSrcInput] = struct{}{}
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
		cur.pathInps = append(cur.pathInps, e.pathInput.String())
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
