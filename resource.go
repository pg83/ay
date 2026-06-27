package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	objcopyScriptPath    = objcopyScriptVFS.string()
	rescompressorBinPath = rescompressorBinVFS.string()
	rescompilerBinPath   = rescompilerBinVFS.string()
	yaConfFormulaRE      = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
	yaConfResourceRE     = regexp.MustCompile(`"resource"\s*:\s*"([^"]+)"`)
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

func expandAllResourceFiles(fs FS, modulePath string, env Environment, stmt *AllResourceFilesStmt) []ResourceEntry {
	prefix := ""
	strip := ""

	var ext string
	var dirs []STR

	i := 0

	if !stmt.FromDirs {
		ext = expandStmtTokenSTR(stmt.Args[0], env).string()
		i = 1
	}

	for i < len(stmt.Args) {
		switch stmt.Args[i] {
		case kwPREFIX:
			if i+1 < len(stmt.Args) {
				prefix = expandStmtTokenSTR(stmt.Args[i+1], env).string()
			}

			i += 2
		case kwSTRIP:
			if i+1 < len(stmt.Args) {
				strip = expandStmtTokenSTR(stmt.Args[i+1], env).string()
			}

			i += 2
		default:
			dirs = append(dirs, stmt.Args[i])
			i++
		}
	}

	suffix := ""

	if !stmt.FromDirs {
		suffix = "." + ext
	}

	rfArgs := make([]string, 0, len(dirs)*8)

	if prefix != "" {
		rfArgs = append(rfArgs, "PREFIX", prefix)
	}

	rfArgs = append(rfArgs, "STRIP", "${ARCADIA_ROOT}/"+modulePath+"/"+strip)

	for _, dir := range dirs {
		rel, ok := allResourceDir(modulePath, expandStmtTokenSTR(dir, env).string())

		if !ok {
			continue
		}

		for _, match := range globMatch(fs, rel+"/*"+suffix) {
			rfArgs = append(rfArgs, "${ARCADIA_ROOT}/"+match)
		}
	}

	return expandResourceFiles(rfArgs)
}

func allResourceDir(modulePath, dir string) (rel string, ok bool) {
	switch {
	case strings.HasPrefix(dir, "$(S)/"):
		rel = dir[len("$(S)/"):]
	case strings.HasPrefix(dir, "${ARCADIA_ROOT}/"):
		rel = dir[len("${ARCADIA_ROOT}/"):]
	case strings.HasPrefix(dir, "$") || strings.HasPrefix(dir, "/"):
		return "", false
	default:
		rel = modulePath + "/" + dir
	}

	rel = path.Clean(rel)

	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}

	return rel, true
}

func globMatch(fs FS, pattern string) []string {
	segs := strings.Split(pattern, "/")
	dirs := []string{""}

	var matches []string

	for i, seg := range segs {
		last := i == len(segs)-1
		wild := strings.ContainsAny(seg, "*?")

		var next []string

		for _, d := range dirs {
			if !wild {
				child := path.Join(d, seg)
				present, isDir := fs.exists(srcRootVFS, child)

				if !present {
					continue
				}

				if last {
					if !isDir {
						matches = append(matches, child)
					}
				} else if isDir {
					next = append(next, child)
				}

				continue
			}

			view := fs.listdir(source(d))
			entries := append([]uint32(nil), view.names...)

			sort.Slice(entries, func(a, b int) bool {
				return STR(entries[a]>>1).string() < STR(entries[b]>>1).string()
			})

			for _, packed := range entries {
				name := STR(packed >> 1).string()

				if !globSegMatch(seg, name) {
					continue
				}

				isDir := packed&1 != 0
				child := path.Join(d, name)

				if last {
					if !isDir {
						matches = append(matches, child)
					}
				} else if isDir {
					next = append(next, child)
				}
			}
		}

		dirs = next
	}

	return matches
}

func globSegMatch(pat, name string) bool {
	var px, nx, starPx, starNx int
	starPx = -1

	for nx < len(name) {
		if px < len(pat) && (pat[px] == '?' || pat[px] == name[nx]) {
			px++
			nx++
		} else if px < len(pat) && pat[px] == '*' {
			starPx = px
			starNx = nx
			px++
		} else if starPx != -1 {
			px = starPx + 1
			starNx++
			nx = starNx
		} else {
			return false
		}
	}

	for px < len(pat) && pat[px] == '*' {
		px++
	}

	return px == len(pat)
}

func renderResourceKvCmd(kv string) string {
	kv = strings.ReplaceAll(kv, "${ARCADIA_ROOT}/", "$(S)/")
	kv = strings.ReplaceAll(kv, "${ARCADIA_BUILD_ROOT}/", "$(B)/")

	return kv
}

func resourceModuleTag(modName TOK) *string {
	switch modName {
	case tokPy3Library, tokPy3ProgramBin, tokPy23Library, tokPy23NativeLibrary:
		return stringPtr("PY3")
	}

	return nil
}

func resourceBinTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program {
		return stringPtr("PY3_BIN")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

func resourceLibTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program || d.programPairedLib {
		return stringPtr("PY3_BIN_LIB")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

func rootrelExpand(kv string, resolved string) string {
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

	return kv[:idx] + resolved + tail[end+len("\"}"):]
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
	pathHash    string
	pathInput   VFS
	key         string
	kvHash      string
	kvCmd       string
	inputDep    VFS
	extraInputs []VFS
}

func resolvePySrcRel(fs FS, srcDirs []VFS, modulePath, srcRel string) string {
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if fs.isFile(srcDirs[i], srcRel) {
			return srcDirs[i].rel() + "/" + srcRel
		}
	}

	if srcRel != "" && pathIsClean(srcRel) &&
		!fs.isFile(dirKey(modulePath), srcRel) && fs.isFile(srcRootVFS, srcRel) {
		return srcRel
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

	fullName := make(map[string]bool, len(d.pySrcs))

	for i, s := range d.pySrcs {
		if i < len(d.pySrcsFullName) && d.pySrcsFullName[i] {
			fullName[s.string()] = true
		}
	}

	out := make([]PySrcEntry, 0, len(srcs)*2)

	for _, srcRel := range srcs {
		if strings.HasSuffix(srcRel, ".pyi") {
			continue
		}

		suffix := ".yapyc3"

		if strings.Contains(srcRel, "/") {
			suffix = "." + pySrcYapycSuffix(modulePath) + ".yapyc3"
		}

		resolvedRel := resolvePySrcRel(fs, d.srcDirs, modulePath, srcRel)
		genInfo := reg.lookupSplit(dirKey(modulePath), internStr(srcRel))
		generated := genInfo != nil

		if generated && fullName[srcRel] {
			continue
		}

		pySource := source(resolvedRel)

		if generated {
			pySource = build(modulePath, "/", srcRel)
			resolvedRel = modulePath + "/" + srcRel
		}

		srcEdge := pySource
		copyStaged := generated && genInfo.SourcePath != 0 && genInfo.SourcePath.isSource()

		if copyStaged {
			srcEdge = genInfo.SourcePath
		}

		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + resolvedRel

			var pyExtra []VFS

			if copyStaged {
				pyExtra = []VFS{srcEdge}
			}

			out = append(out, PySrcEntry{
				pathHash:    srcRel,
				pathInput:   pySource,
				key:         pyKey,
				kvHash:      pyKvHash,
				kvCmd:       pyKvCmd,
				inputDep:    pySource,
				extraInputs: pyExtra,
			})
		}

		if !d.pyBuildNoPYC {
			ypKey := "resfs/file/py/" + keyPrefix + srcRel + ".yapyc3"
			ypPathInput := build(modulePath, "/", srcRel, suffix)
			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix

			out = append(out, PySrcEntry{
				pathHash:    srcRel + suffix,
				pathInput:   ypPathInput,
				key:         ypKey,
				kvHash:      ypKvHash,
				kvCmd:       ypKvCmd,
				inputDep:    ypPathInput,
				extraInputs: []VFS{srcEdge},
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
	inps     []VFS
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

		for _, v := range e.extraInputs {
			if deduper.add(v) {
				cur.inps = append(cur.inps, v)
			}
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
