package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	enchex "encoding/hex"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	genPy3RegScriptPath  = genPy3RegScriptVFS.string()
	genPy3RegScriptChunk = []VFS{genPy3RegScriptVFS}
	genPyRegScriptChunk  = []VFS{genPyRegScriptVFS}
	pyCodegenKV          = KV{P: pkPY, PC: pcYellow}
	yaConfFormulaRE      = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
	yaConfResourceRE     = regexp.MustCompile(`"resource"\s*:\s*"([^"]+)"`)
)

const b32Lower = "abcdefghijklmnopqrstuvwxyz234567"

const (
	pySourcePlain PySourceKind = iota + 1
	pySourceGenerated
	pySourceProtoInput
	pySourceProto
)

type PySourceKind uint8

type PySourceMeta struct {
	Module      STR
	Token       ANY
	ExtraInputs []VFS
	Group       int
	Kind        PySourceKind
}

func (e *EmitContext) addPyMeta(meta PySourceMeta) uint32 {
	e.pyMetas = append(e.pyMetas, meta)

	return uint32(len(e.pyMetas))
}

func pyResourceKeyPrefix(topLevel bool, namespace *ANY, modulePath string) string {
	if topLevel {
		return ""
	}

	if namespace != nil {
		return strings.ReplaceAll(strings.TrimSuffix(namespace.string(), "."), ".", "/") + "/"
	}

	return modulePath + "/"
}

func generatedPyResourceKey(modulePath string, d *ModuleData, srcRel string) string {
	return pyResourceKeyPrefix(d.pyTopLevel, nil, modulePath) + srcRel
}

type PySrc struct {
	Path        VFS
	Module      STR
	Token       ANY
	ExtraInputs []VFS
	Group       int
	Kind        PySourceKind
}

func resolvePySrcRel(fs FS, srcDirs []VFS, moduleVFS VFS, srcRel string) STR {
	return resolvePySrcRelKnown(fs, srcDirs, moduleVFS, srcRel, srcRel != "" && pathIsClean(srcRel))
}

func resolvePySrcRelKnown(fs FS, srcDirs []VFS, moduleVFS VFS, srcRel string, srcRelClean bool) STR {
	if srcRelClean {
		target := internStr(srcRel)

		for i := len(srcDirs) - 1; i >= 1; i-- {
			if resolved := fs.resolveSourceUnderClean(srcDirs[i].rel(), target, true); resolved != 0 {
				return resolved
			}
		}

		if resolved := fs.resolveSourceUnderClean(moduleVFS.rel(), target, true); resolved != 0 {
			return resolved
		}

		if resolved := fs.resolveSourceUnderClean(srcRootRel, target, true); resolved != 0 {
			return resolved
		}

		return internV(moduleVFS.relString(), "/", srcRel)
	}

	for i := len(srcDirs) - 1; i >= 1; i-- {
		if fs.isFile(srcDirs[i].rel(), srcRel) {
			return internV(srcDirs[i].relString(), "/", srcRel)
		}
	}

	return internV(moduleVFS.relString(), "/", srcRel)
}

func pySrcYapycSuffix(modulePath string) string {
	var pathBuf [192]byte

	b := append(pathBuf[:0], "$S/"...)

	b = append(b, modulePath...)

	sum := md5.Sum(b)

	var out [4]byte

	out[0] = b32Lower[sum[0]>>3]
	out[1] = b32Lower[(sum[0]<<2|sum[1]>>6)&31]
	out[2] = b32Lower[(sum[1]>>1)&31]
	out[3] = b32Lower[(sum[1]<<4|sum[2]>>4)&31]

	return internBytes(out[:]).string()
}

func (e *EmitContext) registerCollectPySrcs() {
	ctx, instance, d := e.ctx, e.instance, e.d
	module := instance.Path.relString()

	for gi, group := range d.pySrcGroups {
		var scope PyScope

		keyPrefix := ""

		for si, src := range group.Srcs {
			if si == 0 || group.Scopes[si] != scope {
				scope = group.Scopes[si]
				keyPrefix = pyResourceKeyPrefix(scope.TopLevel, scope.Namespace, module)
			}

			if d.usePython2 && extIsProto(src.relOrSelf().string()) {
				throwFmt("PY_SRCS: %s: .proto sources of a Python 2 module are not modelled", module)
			}

			if extIsProto(src.relOrSelf().string()) {
				e.enqueueSrc(SrcMeta{
					Source: src, Prio: stmtPrioDefault,
					PyMeta: e.addPyMeta(PySourceMeta{Group: gi, Kind: pySourceProtoInput}),
				})

				continue
			}

			token := src
			path := src.vfs()
			kind := pySourcePlain

			if path != 0 {
				if path.isBuild() {
					kind = pySourceGenerated
					token = internStr(trimModulePrefix(path.relString(), module)).any()
				}
			} else {
				path = build(module, "/", src.string())

				if e.codegen.lookupSplit(instance.Path, src) == nil {
					path = resolveSourceVFS(ctx, instance, src.string(), d.srcDirs)
				}
			}

			if d.usePython2 && kind == pySourceGenerated {
				throwFmt("PY_SRCS: %s: build-root sources of a Python 2 module are not modelled", module)
			}

			e.enqueueSrc(SrcMeta{
				Source: path.any(), Prio: stmtPrioDefault,
				PyMeta: e.addPyMeta(PySourceMeta{
					Module: e.pyModuleKey(keyPrefix, scope, token.string()),
					Token:  token,
					Group:  gi,
					Kind:   kind,
				}),
			})
		}
	}
}

func (e *EmitContext) pyModuleKey(keyPrefix string, scope PyScope, src string) STR {
	if !e.d.usePython2 {
		return internV(keyPrefix, src)
	}

	if isProgramModuleType(e.d.moduleStmt.Name) && (src == "__main__.py" || strings.HasSuffix(src, "/__main__.py")) {
		return strMain
	}

	return internStr(pythonModuleName(e.instance.Path.relString(), src, scope.TopLevel, scope.Namespace))
}

func (e *EmitContext) pyYapycExt() string {
	if e.d.usePython2 {
		return ".yapyc"
	}

	return ".yapyc3"
}

func (e *EmitContext) collectPySource(path VFS, meta PySourceMeta) {
	e.pySrcsReg = append(e.pySrcsReg, PySrc{
		Path: path, Module: meta.Module, Token: meta.Token,
		ExtraInputs: meta.ExtraInputs, Group: meta.Group, Kind: meta.Kind,
	})
}

func (e *EmitContext) pyYapycOutFor(ps PySrc) VFS {
	if ps.Kind == pySourceProto {
		rel := ps.Path.relString()
		token := strings.TrimPrefix(ps.Token.string(), "${ARCADIA_BUILD_ROOT}/")

		if strings.Contains(token, "/") {
			return build(rel, ".", pySrcYapycSuffix(e.instance.Path.relString()), ".yapyc3")
		}

		return build(rel, ".yapyc3")
	}

	module := e.instance.Path.relString()
	srcRel := ps.Token.string()

	if ps.Kind == pySourceGenerated {
		srcRel = strings.TrimPrefix(ps.Path.relString(), module+"/")
	}

	if strings.Contains(srcRel, "/") {
		return build(module, "/", srcRel, ".", e.d.pyYapycSuffix, e.pyYapycExt())
	}

	return build(module, "/", srcRel, e.pyYapycExt())
}

func (e *EmitContext) appendPyResEntries(out []PyGenResEntry, ps PySrc) []PyGenResEntry {
	d := e.d
	module := e.instance.Path.relString()

	switch ps.Kind {
	case pySourceProto:
		rel := ps.Path.relString()
		info := e.codegen.use(ps.Path)

		if info == nil {
			throwFmt("appendPyResEntries: unregistered proto producer for %q", ps.Path.string())
		}

		token := ps.Token.string()
		yapycOut := e.pyYapycOutFor(ps)

		if !strings.HasPrefix(token, "${ARCADIA_BUILD_ROOT}/") {
			return append(out,
				PyGenResEntry{token: token, key: ps.Module, path: ps.Path},
				PyGenResEntry{token: e.resStr2(token, strings.TrimPrefix(yapycOut.relString(), rel)), key: ps.Module, yapyc: true, path: yapycOut})
		}

		return append(out,
			PyGenResEntry{token: token, key: ps.Module, path: ps.Path, buildInputs: ps.ExtraInputs},
			PyGenResEntry{token: e.resStr2("${ARCADIA_BUILD_ROOT}/", yapycOut.relString()), key: ps.Module, yapyc: true, path: yapycOut})
	case pySourceGenerated:
		info := e.codegen.use(ps.Path)

		if info == nil {
			throwFmt("appendPyResEntries: unregistered producer for %q", ps.Path.string())
		}

		out = append(out, PyGenResEntry{token: ps.Token.string(), key: ps.Module, path: ps.Path})

		if !d.pyBuildNoPYC {
			yapycOut := e.pyYapycOutFor(ps)

			out = append(out, PyGenResEntry{token: e.resStr2("${ARCADIA_BUILD_ROOT}/", yapycOut.relString()), key: ps.Module, yapyc: true, path: yapycOut})
		}

		return out
	}

	srcRel := ps.Token.string()
	suffix := e.pyYapycExt()

	if strings.Contains(srcRel, "/") {
		suffix = "." + d.pyYapycSuffix + suffix
	}

	resolvedRel := resolvePySrcRel(e.ctx.fs, d.srcDirs, e.instance.Path, srcRel)
	genInfo := e.codegen.useSplit(e.instance.Path, ps.Token)
	pySource := resolvedRel.source()

	if genInfo != nil {
		pySource = build(module, "/", srcRel)
	}

	if !d.pyBuildNoPY {
		out = append(out, PyGenResEntry{token: srcRel, key: ps.Module, path: pySource})
	}

	if !d.pyBuildNoPYC {
		entry := PyGenResEntry{token: e.resStr2(srcRel, suffix), key: ps.Module, yapyc: true, path: build(module, "/", srcRel, suffix)}

		out = append(out, entry)
	}

	return out
}

func (e *EmitContext) hasPySrcKind(kind PySourceKind) bool {
	for _, ps := range e.pySrcsReg {
		if ps.Kind == kind {
			return true
		}
	}

	return false
}

func (e *EmitContext) hasEnginePySrcs() bool {
	return e.hasPySrcKind(pySourcePlain) || e.hasPySrcKind(pySourceGenerated)
}

func (e *EmitContext) emitPyBytecode(moduleSources bool) {
	ctx, _, d := e.ctx, e.instance, e.d
	hasProto := e.hasPySrcKind(pySourceProto)
	hasEngine := moduleSources && !d.pyBuildNoPYC && e.hasEnginePySrcs()

	if !hasProto && !hasEngine {
		return
	}

	tool := e.pyccTool()

	if hasProto {
		for _, ps := range e.pySrcsReg {
			if ps.Kind == pySourceProto {
				e.emitPyYapyc(ps, tool)
			}
		}
	}

	if !hasEngine {
		return
	}

	ctx.tool(argToolsRescompiler)
	ctx.tool(argToolsRescompressor)
	ctx.tool(argToolsArchiver)

	for _, ps := range e.pySrcsReg {
		if ps.Kind == pySourceProto {
			continue
		}

		e.emitPyYapyc(ps, tool)
	}
}

type PyccTool struct {
	refs      []NodeRef
	head      []ANY
	binaries  []VFS
	resources []STR
}

func (e *EmitContext) pyccTool() PyccTool {
	ctx := e.ctx
	na := ctx.na

	if e.d.usePython2 {
		path := argToolsPy2cc

		if e.d.useLightPy2cc {
			path = argToolsPy2ccStage0
		}

		ref, binary := ctx.tool(path)

		return PyccTool{refs: na.refList(ref), head: na.anyList(binary.any()), binaries: na.vfsList(binary)}
	}

	py3ccLDRef, py3ccBinary := ctx.tool(argToolsPy3cc)
	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	return PyccTool{
		refs:      na.refList(py3ccLDRef, py3ccSlowLDRef),
		head:      ctx.py3ccHead(py3ccBinary, py3ccSlowBin),
		binaries:  na.vfsList(py3ccBinary, py3ccSlowBin),
		resources: usesPython3,
	}
}

func (e *EmitContext) emitPyYapyc(ps PySrc, tool PyccTool) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	srcAbs := ps.Path
	toolRefs := tool.refs
	env := envVarsVCSPyHash
	outputPath := e.pyYapycOutFor(ps)

	if e.codegen.lookup(outputPath) != nil {
		return
	}

	var cmdArgs ArgChunks
	var nodeInputs InputChunks
	var generatorRefs []NodeRef
	var closure Closure

	if ps.Kind == pySourceProto {
		info := e.codegen.use(ps.Path)

		if info == nil {
			throwFmt("emitPyYapyc: unregistered proto producer for %q", ps.Path.string())
		}

		token := strings.TrimPrefix(ps.Token.string(), "${ARCADIA_BUILD_ROOT}/")

		cmdArgs = na.chunkList(tool.head,
			na.anyList(internV(token, "-").any(), ps.Path.any(), outputPath.any()))

		if len(ps.ExtraInputs) > 0 {
			nodeInputs = na.inputList(tool.binaries, na.vfsList(ps.Path), ps.ExtraInputs)
		} else {
			nodeInputs = na.inputList(tool.binaries, na.vfsList(ps.Path))
		}
	} else {
		var genInfo *GeneratedFileInfo
		var moduleName string

		if ps.Kind == pySourceGenerated {
			genInfo = e.codegen.use(ps.Path)

			if genInfo == nil {
				throwFmt("emitPyYapyc: unregistered generated producer for %q", ps.Path.string())
			}

			moduleName = e.resStr2(srcAbs.relString(), "-")
		} else {
			genInfo = e.codegen.useSplit(instance.Path, ps.Token)

			if genInfo != nil {
				moduleName = e.resStr2(ps.Token.string(), "-")
			} else {
				moduleName = e.resStr2(srcAbs.relString(), "-")
			}
		}

		cmdArgs = na.chunkList(tool.head,
			na.anyList(internStr(moduleName).any(), srcAbs.any(), outputPath.any()))

		if genInfo != nil {
			closure = e.inputClosure(srcAbs)

			buckets := closure.bucketList()
			chunks := na.inputs.alloc(3 + len(buckets))[:0]

			chunks = append(chunks, tool.binaries, na.vfsList(srcAbs))
			chunks = append(chunks, buckets...)

			if main := genInfo.ProducerMainOut; main != 0 && main != srcAbs {
				chunks = append(chunks, na.vfsList(main))
			}

			na.inputs.commit(len(chunks))
			nodeInputs = chunks[:len(chunks):len(chunks)]
		} else {
			nodeInputs = na.inputList(tool.binaries, na.srcChunk(srcAbs))
		}

		generatorRefs = toolRefs
	}

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:            env,
		Inputs:         nodeInputs,
		InputClosure:   closure,
		Outputs:        na.vfsList(outputPath),
		KV:             &pyCodegenKV,
		ForeignDepRefs: toolRefs,
		Resources:      tool.resources,
	}

	pyRef := e.emitNode(node)

	e.register(GeneratedFileInfo{
		OutputPath:    outputPath,
		ProducerRef:   pyRef,
		GeneratorRefs: generatorRefs,
	})
}

func (e *EmitContext) emitPySrcObjcopy() *ObjcopyEmitResult {
	_, instance, d := e.ctx, e.instance, e.d

	if len(d.pySrcs) == 0 {
		return nil
	}

	if d.unit.Tag == 0 {
		return nil
	}

	if d.unit.Tag == unitTagPy3Bin {
		return nil
	}

	namespaceEnabled := !d.noExtendedPySearch &&
		!strings.HasPrefix(instance.Path.relString(), "contrib/python") &&
		!strings.HasPrefix(instance.Path.relString(), "contrib/tools/python3") &&
		pyNamespaceUnitType(d.unit.Type)

	res := &ObjcopyEmitResult{}

	for gi, group := range d.pySrcGroups {
		if namespaceEnabled {
			nsRefs, nsOuts := e.emitPyNamespaceForGroup(group)

			res.Refs = append(res.Refs, nsRefs...)
			res.Outputs = append(res.Outputs, nsOuts...)
		}

		entries := e.resEntries[:0]

		for _, ps := range e.pySrcsReg {
			if ps.Kind != pySourcePlain || ps.Group != gi {
				continue
			}

			entries = e.appendPyResEntries(entries, ps)
		}

		e.resEntries = retainMaxLen(e.resEntries, entries)

		if len(entries) > 0 {
			groupRefs, groupOuts := e.packResources(ResourcePack{DontCompress: true, Tag: d.unit.HashTag, Items: e.pySrcResourceItems(entries)})

			res.Refs = append(res.Refs, groupRefs...)
			res.Outputs = append(res.Outputs, groupOuts...)
		}

		if d.moduleStmt.Name != tokProtoLibrary {
			protoRefs, protoOuts := e.emitPyProtoGroupResources(gi)

			res.Refs = append(res.Refs, protoRefs...)
			res.Outputs = append(res.Outputs, protoOuts...)
		}
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}

func (e *EmitContext) collectPyProtoGroupResEntries(group int) []PyGenResEntry {
	entries := e.resEntries[:0]

	for _, ps := range e.pySrcsReg {
		if ps.Kind == pySourceProto && ps.Group == group {
			entries = e.appendPyResEntries(entries, ps)
		}
	}

	e.resEntries = retainMaxLen(e.resEntries, entries)

	return entries
}

func (e *EmitContext) pyProtoGroups() []int {
	var groups []int

	for _, ps := range e.pySrcsReg {
		if ps.Kind == pySourceProto && !slices.Contains(groups, ps.Group) {
			groups = append(groups, ps.Group)
		}
	}

	return groups
}

func (e *EmitContext) packPyProtoResEntries(entries []PyGenResEntry) ([]NodeRef, []VFS) {
	if len(entries) == 0 {
		return nil, nil
	}

	return e.packResources(ResourcePack{DontCompress: true,
		Tag: e.d.unit.HashTag, Items: e.pyGenResourceItems(entries),
		RawCompile: CompileSpec{
			ForceCxx: true,
			ObjSuf:   pyProtoAuxObjSuf(e.d),
			CFlags:   e.ctx.na.anyList(argX.any(), argC.any()),
		},
	})
}

func (e *EmitContext) emitPyProtoGroupResources(group int) ([]NodeRef, []VFS) {
	return e.packPyProtoResEntries(e.collectPyProtoGroupResEntries(group))
}

func (e *EmitContext) emitPyNamespaceForGroup(group PySrcGroup) ([]NodeRef, []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	h := md5.New()
	nsRoots := make(map[string][]string, len(group.Srcs))
	hasPy := false

	for si, src := range group.Srcs {
		srcRel := src.string()

		if !extIsPy(srcRel) {
			continue
		}

		hasPy = true

		scope := group.Scopes[si]
		nsPrefix := strings.ReplaceAll(instance.Path.relString(), "/", ".") + "."

		if scope.Namespace != nil {
			nsPrefix = strings.TrimSuffix(scope.Namespace.string(), ".") + "."
		}

		nsValue := nsPrefix

		if scope.TopLevel {
			nsPrefix = ""
			nsValue = "."
		}

		h.Write([]byte(nsPrefix + strings.ReplaceAll(strings.TrimSuffix(srcRel, ".py"), "/", ".")))

		if reg.lookupSplit(instance.Path, src) != nil {
			continue
		}

		resolvedRel := resolvePySrcRel(ctx.fs, d.srcDirs, instance.Path, srcRel).string()
		root := resolvedRel[:max(len(resolvedRel)-len(srcRel)-1, 0)]

		if !slices.Contains(nsRoots[root], nsValue) {
			nsRoots[root] = append(nsRoots[root], nsValue)
		}
	}

	if !hasPy || len(nsRoots) == 0 {
		return nil, nil
	}

	modListMD5 := enchex.EncodeToString(h.Sum(nil))
	keyPaths := make([]string, 0, len(nsRoots))

	for keyPath := range nsRoots {
		keyPaths = append(keyPaths, keyPath)
	}

	sort.Strings(keyPaths)

	kvsHash := make([]string, 0, len(keyPaths))
	kvsCmd := make([]STR, 0, len(keyPaths))

	for _, keyPath := range keyPaths {
		key := "py/namespace/" + modListMD5 + "/" + keyPath
		values := nsRoots[keyPath]

		sort.Strings(values)

		nsValue := strings.Join(values, ":")

		kvsHash = append(kvsHash, key+"=\""+nsValue+"\"")
		kvsCmd = append(kvsCmd, internStr(key+"="+nsValue))
	}

	return e.emitKvOnlyResource(d.unit.Tag, kvsHash, kvsCmd)
}

func (e *EmitContext) emitPyMainObjcopy() ([]NodeRef, []VFS) {
	_, _, d := e.ctx, e.instance, e.d

	if d.pyMain == nil {
		return nil, nil
	}

	kv := "PY_MAIN=" + d.pyMain.string()

	return e.emitKvOnlyResource(d.unit.Tag, []string{kv}, []STR{internStr(kv)})
}

func (e *EmitContext) emitNoCheckImportsObjcopy() ([]NodeRef, []VFS) {
	_, _, d := e.ctx, e.instance, e.d

	if len(d.noCheckImports) == 0 {
		return nil, nil
	}

	value := strings.Join(anyStrs(d.noCheckImports), " ")
	sum := md5.Sum([]byte(value))
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))

	b32 = strings.TrimRight(b32, "=")

	key := "py/no_check_imports/" + b32
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return e.emitKvOnlyResource(d.unit.Tag, []string{kvHash}, []STR{internStr(kvCmd)})
}

func (e *EmitContext) emitYaConfJSONObjcopy() ([]NodeRef, []VFS) {
	ctx, _, d := e.ctx, e.instance, e.d

	if len(d.yaConfJSON) == 0 {
		return nil, nil
	}

	type yaConfResource struct {
		sourcePath string
		keyPath    string
		hashPath   string
	}

	var resources []yaConfResource

	for _, file := range d.yaConfJSON {
		resources = append(resources, yaConfResource{
			sourcePath: file.string(),
			keyPath:    "ya.conf.json",
			hashPath:   "ya.conf.json",
		})

		formulas := yaConfFormulaResources(ctx.fs, file.string())

		sort.Strings(formulas)

		for _, formula := range formulas {
			resources = append(resources, yaConfResource{
				sourcePath: formula,
				keyPath:    formula,
				hashPath:   formula,
			})
		}
	}

	outRefs := make([]NodeRef, 0, len(resources))
	outPaths := make([]VFS, 0, len(resources))
	moduleTag := d.unit.HashTag

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := internStr("resfs/src/" + key + "=" + res.sourcePath)
		input := source(res.sourcePath)

		refs, outs := e.packResources(ResourcePack{DontCompress: true, Tag: moduleTag, Items: []ResourceItem{
			{Path: "-", Key: kvHash, Cmd: kvCmd},
			{Path: res.hashPath, Key: key, SourceInput: input},
		}})

		outRefs = append(outRefs, refs...)
		outPaths = append(outPaths, outs...)
	}

	return outRefs, outPaths
}

func (e *EmitContext) emitGeneratedPyAuxChunks() (refs []NodeRef, outs []VFS) {
	d := e.d

	for _, ps := range e.pySrcsReg {
		if ps.Kind != pySourceGenerated {
			continue
		}

		r, o := e.packResources(ResourcePack{DontCompress: true,
			Tag: d.unit.HashTag, Items: e.pyGenResourceItems(e.appendPyResEntries(e.resEntries[:0], ps)),
			RawCompile: CompileSpec{
				ForceCxx: true,
				CFlags:   e.ctx.na.anyList(argX.any(), argC.any()),
			},
		})

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	return refs, outs
}

func pyProtoAuxObjSuf(d *ModuleData) STR {
	if d.unit.Tag == unitTagPy3Proto {
		return tagPy3
	}

	return d.unit.ObjSuf
}

func (e *EmitContext) emitPyRegister() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.pyRegister) == 0 {
		return
	}

	for i, arg := range d.pyRegister {
		priorShort := make(map[string]struct{}, i)

		for j := 0; j < i; j++ {
			if !d.pyRegisterExplicit[j] {
				continue
			}

			prior := d.pyRegister[j]
			priorStr := prior.string()

			priorShort[priorStr[strings.LastIndexByte(priorStr, '.')+1:]] = struct{}{}
		}

		regCpp := arg.string() + ".reg3.cpp"
		script, scriptChunk := genPy3RegScriptVFS, genPy3RegScriptChunk

		if d.usePython2 {
			regCpp = arg.string() + ".reg.cpp"
			script, scriptChunk = genPyRegScriptVFS, genPyRegScriptChunk
		}

		regCppVFS := build(instance.Path.relString(), "/", regCpp)
		env := envVarsVCS

		pyCmdArgs := na.anyList(
			d.tc.Python3.any(),
			script.any(),
			arg.any(),
			regCppVFS.any(),
		)

		pyNode := Node{
			Platform:  ctx.target,
			Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(pyCmdArgs), Env: env}),
			Env:       env,
			Inputs:    na.inputList(scriptChunk),
			Outputs:   na.vfsList(regCppVFS),
			KV:        &pyCodegenKV,
			Resources: usesPython3,
		}

		pyRef := e.emitNode(pyNode)
		envCFlags := na.anys.alloc(len(d.cc.CFlags))[:0]

		for _, f := range d.cc.CFlags {
			if short, ok := pyInitDefineShortname(f.string()); ok {
				if _, keep := priorShort[short]; !keep {
					continue
				}
			}

			envCFlags = append(envCFlags, f)
		}

		na.anys.commit(len(envCFlags))

		spec := CompileSpec{ObjSuf: d.unit.ObjSuf, EnvCFlags: envCFlags[:len(envCFlags):len(envCFlags)]}

		if len(d.cythonCpp) > 0 {
			spec.EnvAddIncl = appendCythonCCAddIncl(na, d.cc.AddIncl, d.cythonNumpyBeforeInclude)
		}

		e.register(GeneratedFileInfo{
			OutputPath:  regCppVFS,
			ProducerRef: pyRef,
		})

		e.enqueueSrc(SrcMeta{
			Source: regCppVFS.any(), Prio: stmtPrioDefault, Global: true,
			Compile: spec,
		})
	}
}

func pyInitDefineShortname(flag string) (string, bool) {
	for _, pfx := range []string{"-DPyInit_", "-Dinit_module_"} {
		if strings.HasPrefix(flag, pfx) {
			rest := flag[len(pfx):]

			if eq := strings.IndexByte(rest, '='); eq >= 0 {
				return rest[:eq], true
			}

			return rest, true
		}
	}

	if rest, ok := strings.CutPrefix(flag, "-Dinit"); ok {
		if short, value, ok := strings.Cut(rest, "="); ok && strings.HasPrefix(value, "init") {
			return short, true
		}
	}

	return "", false
}

type PyGenResEntry struct {
	token       string
	key         STR
	yapyc       bool
	path        VFS
	buildInputs []VFS
}

func (e *EmitContext) pySrcResourceItems(entries []PyGenResEntry) []ResourceItem {
	if !e.d.usePython2 {
		return e.pyGenResourceItems(entries)
	}

	items := e.resItems[:0]
	buf := e.resStrBuf

	for _, en := range entries {
		keyStart := len(buf)

		if en.yapyc {
			buf = append(buf, "/py_code/"...)
		} else {
			buf = append(buf, "/py_modules/"...)
		}

		buf = append(buf, en.key.sharedString()...)

		key := bytesString(buf[keyStart:])
		fileItem := ResourceItem{Path: en.token, Key: key, ExtraBuilds: en.buildInputs}

		fileItem.setInput(en.path)
		items = append(items, fileItem)

		if en.yapyc {
			continue
		}

		hashStart := len(buf)

		buf = append(buf, "resfs/src/"...)
		buf = append(buf, key...)
		buf = append(buf, "=${rootrel;context=TEXT;input=TEXT:\""...)
		buf = append(buf, en.token...)
		buf = append(buf, "\"}"...)

		kvHash := bytesString(buf[hashStart:])
		cmdStart := len(buf)

		buf = append(buf, "resfs/src/"...)
		buf = append(buf, key...)
		buf = append(buf, '=')
		buf = append(buf, en.path.relString()...)

		cmd := internBytes(buf[cmdStart:])

		buf = buf[:cmdStart]

		kvItem := ResourceItem{Path: "-", Key: kvHash, Cmd: cmd, ExtraBuilds: en.buildInputs}

		kvItem.setInput(en.path)
		items = append(items, kvItem)
	}

	e.resStrBuf = buf
	e.resItems = retainMaxLen(e.resItems, items)

	return items
}

func (e *EmitContext) pyGenResourceItems(entries []PyGenResEntry) []ResourceItem {
	items := e.resItems[:0]
	buf := e.resStrBuf
	need := 2 * len(entries)

	if cap(items) < need {
		items = make([]ResourceItem, 0, need)
	}

	for _, en := range entries {
		keyStart := len(buf)

		buf = append(buf, "resfs/file/py/"...)
		buf = append(buf, en.key.sharedString()...)

		if en.yapyc {
			buf = append(buf, ".yapyc3"...)
		}

		key := bytesString(buf[keyStart:])
		hashStart := len(buf)

		buf = append(buf, "resfs/src/"...)
		buf = append(buf, key...)
		buf = append(buf, "=${rootrel;context=TEXT;input=TEXT:\""...)
		buf = append(buf, en.token...)
		buf = append(buf, "\"}"...)

		kvHash := bytesString(buf[hashStart:])
		cmdStart := len(buf)

		buf = append(buf, "resfs/src/"...)
		buf = append(buf, key...)
		buf = append(buf, '=')
		buf = append(buf, en.path.relString()...)

		cmd := internBytes(buf[cmdStart:])

		buf = buf[:cmdStart]

		kvItem := ResourceItem{Path: "-", Key: kvHash, Cmd: cmd, ExtraBuilds: en.buildInputs}
		fileItem := ResourceItem{Path: en.token, Key: key, ExtraBuilds: en.buildInputs}

		kvItem.setInput(en.path)
		fileItem.setInput(en.path)
		items = append(items, kvItem, fileItem)
	}

	e.resStrBuf = buf
	e.resItems = retainMaxLen(e.resItems, items)

	return items
}

func yaConfFormulaResources(fs FS, confPath string) []string {
	raw := concatChunks(fs.read(confPath))

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

		if !fs.isFile(srcRootRel, path) {
			continue
		}

		seen[path] = struct{}{}
		out = append(out, path)
	}

	return out
}

func pyNamespaceUnitType(t TOK) bool {
	switch t {
	case tokPy3Test, tokPy3Library, tokPy3ProgramBin, tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}
