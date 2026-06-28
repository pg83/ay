package main

import (
	"crypto/md5"
	enchex "encoding/hex"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	objcopyScriptPath           = objcopyScriptVFS.string()
	rescompressorBinPath        = rescompressorBinVFS.string()
	rescompilerBinPath          = rescompilerBinVFS.string()
	yaConfFormulaRE             = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
	yaConfResourceRE            = regexp.MustCompile(`"resource"\s*:\s*"([^"]+)"`)
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
)

const (
	hashLen    = 26
	rootCmdLen = 200
	maxCmdLen  = 8000
)

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

type ObjcopyArgBlocks struct {
	pre  []STR
	post []STR
}

type ObjcopyEmitCtx struct {
	rescompilerLDRef   NodeRef
	rescompressorLDRef NodeRef
	blocks             ObjcopyArgBlocks
	na                 *NodeArenas
}

func newObjcopyEmitCtx(ctx *GenCtx, d *ModuleData, p *Platform) *ObjcopyEmitCtx {
	oc := &ObjcopyEmitCtx{na: ctx.na}

	oc.rescompilerLDRef, _ = ctx.tool(argToolsRescompiler)
	oc.rescompressorLDRef, _ = ctx.tool(argToolsRescompressor)
	oc.blocks = composeObjcopyArgBlocks(d.tc, p)

	return oc
}

func composeObjcopyArgBlocks(tc ModuleToolchain, p *Platform) ObjcopyArgBlocks {
	return ObjcopyArgBlocks{
		pre: []STR{
			tc.Python3,
			internStr(objcopyScriptPath),
			argCompiler.str(), tc.CXX,
			argObjcopy.str(), tc.Objcopy,
			argCompressor.str(), internStr(rescompressorBinPath),
			argRescompiler.str(), internStr(rescompilerBinPath),
			argOutputObj.str(),
		},
		post: []STR{argTarget.str(), internStr(p.Triple)},
	}
}

func objcopyCmdArgs(oc *ObjcopyEmitCtx, outputObj VFS, payload []STR) ArgChunks {
	return oc.na.chunkList(oc.blocks.pre, oc.na.strList((outputObj).str()), oc.blocks.post, payload)
}

type resolvedResource struct {
	Input           VFS
	ProducerRef     NodeRef
	ProducerMainOut VFS
	SourceInputs    []VFS
	SourceClosure   []VFS
}

func resolveResourceInput(ctx *GenCtx, instance ModuleInstance, rawPath string, fallback VFS) resolvedResource {
	output := resourceOutputVFS(instance.Path.rel(), rawPath)

	if info := ctx.codegenFor(instance).lookup(output); info != nil {
		return resolvedResource{
			Input:           output,
			ProducerRef:     info.ProducerRef,
			ProducerMainOut: info.ProducerMainOut,
			SourceInputs:    info.SourceInputs,
			SourceClosure:   info.ProducerSourceClosure,
		}
	}

	return resolvedResource{Input: fallback}
}

type objcopyNode struct {
	moduleTag  *string
	kv         *KV
	hashPaths  []string
	keysB64    []string
	kvsHash    []string
	kvsCmd     []string
	pathInputs []VFS
	inputs     InputChunks
	deps       []NodeRef
}

func buildObjcopyNode(ctx *GenCtx, instance ModuleInstance, oc *ObjcopyEmitCtx, n objcopyNode) (NodeRef, VFS) {
	na := oc.na
	hash := objcopyHash(n.hashPaths, n.keysB64, n.kvsHash, instance.Path.rel(), n.moduleTag)
	outputObj := build(instance.Path.rel(), "/objcopy_", hash, ".o")
	payload := make([]STR, 0, 2+len(n.pathInputs)+len(n.keysB64)+1+len(n.kvsCmd))

	if len(n.hashPaths) > 0 {
		payload = append(payload, argInputs.str())

		for _, p := range n.pathInputs {
			payload = append(payload, (p).str())
		}

		payload = append(payload, argKeys.str())
		payload = appendInternStrs(payload, n.keysB64)
	}

	if len(n.kvsCmd) > 0 {
		payload = append(payload, argKvs.str())
		payload = appendInternStrs(payload, n.kvsCmd)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Env: env}),
		Env:          env,
		Inputs:       n.inputs,
		Outputs:      na.vfsList(outputObj),
		KV:           n.kv,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesPython3Clang,
		DepRefs:      n.deps,
	}

	return ctx.emit.emit(node), outputObj
}

type auxChunk struct {
	hashInputs []string
	cmdArgs    []string
	inputs     []VFS
	deps       []NodeRef
}

func chunkAuxEntries(entries []PyProtoAuxEntry) []auxChunk {
	var chunks []auxChunk

	cur := auxChunk{}
	cmdLen := 0

	deduper.reset()

	depSeen := map[NodeRef]struct{}{}

	addInput := func(v VFS) {
		if !deduper.add(v) {
			return
		}

		cur.inputs = append(cur.inputs, v)
	}

	addDep := func(ref NodeRef) {
		if ref == (NodeRef(0)) {
			return
		}

		if _, ok := depSeen[ref]; ok {
			return
		}

		depSeen[ref] = struct{}{}
		cur.deps = append(cur.deps, ref)
	}

	flush := func() {
		if cmdLen == 0 {
			return
		}

		chunks = append(chunks, cur)
		cur = auxChunk{}
		cmdLen = 0
		deduper.reset()
		depSeen = map[NodeRef]struct{}{}
	}

	for _, e := range entries {
		key := "resfs/file/py/" + e.key
		arcBuildPath := "${ARCADIA_BUILD_ROOT}/" + e.path.rel()
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + arcBuildPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + e.path.rel()

		cur.hashInputs = append(cur.hashInputs, "-", kvHash)
		cur.cmdArgs = append(cur.cmdArgs, "-", kvCmd)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len("-") + len(kvHash)

		if cmdLen >= maxCmdLen {
			flush()
		}

		cur.hashInputs = append(cur.hashInputs, arcBuildPath, "-"+key)
		cur.cmdArgs = append(cur.cmdArgs, e.path.string(), "-"+key)
		addInput(e.path)

		for _, input := range e.inputs {
			addInput(input)
		}

		addDep(e.producer)
		cmdLen += rootCmdLen + len(arcBuildPath) + len(key)

		if cmdLen >= maxCmdLen {
			flush()
		}
	}

	flush()

	return chunks
}
