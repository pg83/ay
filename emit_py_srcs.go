package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	enchex "encoding/hex"
	"regexp"
	"sort"
	"strings"
)

var (
	genPy3RegScriptPath  = genPy3RegScriptVFS.string()
	genPy3RegScriptChunk = []VFS{genPy3RegScriptVFS}
	pyCodegenKV          = KV{P: pkPY, PC: pcYellow}
	yaConfFormulaRE      = regexp.MustCompile(`"formula"\s*:\s*"([^"]+\.json)"`)
	yaConfResourceRE     = regexp.MustCompile(`"resource"\s*:\s*"([^"]+)"`)
)

const (
	pyGroupGenAux = -2
	pyGroupProto  = -1
)

func pyResourceKeyPrefix(topLevel bool, namespace *STR, modulePath string) string {
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
	Path   VFS
	Module STR
	Token  STR
	Group  int
}

func resolvePySrcRel(fs FS, srcDirs []VFS, moduleVFS VFS, srcRel string) STR {
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if fs.isFile(srcDirs[i], srcRel) {
			return internV(srcDirs[i].rel(), "/", srcRel)
		}
	}

	if srcRel != "" && pathIsClean(srcRel) &&
		!fs.isFile(moduleVFS, srcRel) && fs.isFile(srcRootVFS, srcRel) {
		return internStr(srcRel)
	}

	return internV(moduleVFS.rel(), "/", srcRel)
}

func pySrcYapycSuffix(modulePath string) string {
	return pathIDBase32("$S/" + modulePath)[:4]
}

func (e *EmitContext) collectPyGroups() []PySrcGroup {
	d := e.d
	groups := d.pySrcGroups

	if len(groups) == 0 && len(d.pySrcs) > 0 {
		groups = []PySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	return groups
}

func (e *EmitContext) registerCollectPySrcs() {
	ctx, instance, d := e.ctx, e.instance, e.d
	module := instance.Path.rel()

	for gi, group := range e.collectPyGroups() {
		keyPrefix := pyResourceKeyPrefix(group.TopLevel, group.Namespace, module)

		for _, srcRel := range group.Srcs {
			if extIsProto(srcRel.string()) {
				e.emitPyProtoSource(srcRel)

				continue
			}

			path := build(module, "/", srcRel.string())

			if e.codegen.lookupSplit(instance.Path, srcRel) == nil {
				path = resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
			}

			e.pySrcsReg = append(e.pySrcsReg, PySrc{
				Path:   path,
				Module: internV(keyPrefix, srcRel.string()),
				Token:  srcRel,
				Group:  gi,
			})
		}
	}
}

func (e *EmitContext) pyYapycOutFor(ps PySrc) VFS {
	module := e.instance.Path.rel()
	srcRel := ps.Token.string()

	if ps.Group == pyGroupGenAux {
		srcRel = strings.TrimPrefix(ps.Path.rel(), module+"/")
	}

	if strings.Contains(srcRel, "/") {
		return build(module, "/", srcRel, ".", e.d.pyYapycSuffix, ".yapyc3")
	}

	return build(module, "/", srcRel, ".yapyc3")
}

func (e *EmitContext) pyResEntriesFor(ps PySrc) []PyGenResEntry {
	d := e.d
	module := e.instance.Path.rel()
	key := ps.Module.string()

	switch ps.Group {
	case pyGroupGenAux:
		info := e.codegen.mustInfo(ps.Path, "pyResEntriesFor")
		out := []PyGenResEntry{{token: ps.Token.string(), key: key, path: ps.Path, inputs: info.SourceInputs}}

		if !d.pyBuildNoPYC {
			yapycOut := e.pyYapycOutFor(ps)

			out = append(out, PyGenResEntry{token: "${ARCADIA_BUILD_ROOT}/" + yapycOut.rel(), key: key + ".yapyc3", path: yapycOut, inputs: info.SourceInputs})
		}

		return out
	}

	srcRel := ps.Token.string()
	suffix := ".yapyc3"

	if strings.Contains(srcRel, "/") {
		suffix = "." + d.pyYapycSuffix + ".yapyc3"
	}

	resolvedRel := resolvePySrcRel(e.ctx.fs, d.srcDirs, e.instance.Path, srcRel)
	genInfo := e.codegen.lookupSplit(e.instance.Path, ps.Token)
	pySource := source(resolvedRel.string())

	if genInfo != nil {
		pySource = build(module, "/", srcRel)
	}

	srcEdge := pySource
	copyStaged := genInfo != nil && genInfo.SourcePath != 0 && genInfo.SourcePath.isSource()

	if copyStaged {
		srcEdge = genInfo.SourcePath
	}

	out := make([]PyGenResEntry, 0, 2)

	if !d.pyBuildNoPY {
		var pyExtra []VFS

		if copyStaged {
			pyExtra = []VFS{srcEdge}
		}

		out = append(out, PyGenResEntry{token: srcRel, key: key, path: pySource, inputs: pyExtra})
	}

	if !d.pyBuildNoPYC {
		out = append(out, PyGenResEntry{token: srcRel + suffix, key: key + ".yapyc3", path: build(module, "/", srcRel, suffix), inputs: []VFS{srcEdge}})
	}

	return out
}

func (e *EmitContext) hasEnginePySrcs() bool {
	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupProto {
			return true
		}
	}

	return false
}

func (e *EmitContext) emitPyBytecode() {
	ctx, _, d := e.ctx, e.instance, e.d

	if d.pyBuildNoPYC || !e.hasEnginePySrcs() {
		return
	}

	py3ccLDRef, py3ccBinary := ctx.tool(argToolsPy3cc)
	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	ctx.tool(argToolsRescompiler)
	ctx.tool(argToolsRescompressor)
	ctx.tool(argToolsArchiver)

	for _, ps := range e.pySrcsReg {
		if ps.Group == pyGroupProto || extIsPyi(ps.Token.string()) {
			continue
		}

		e.emitEnginePyYapyc(ps, py3ccLDRef, py3ccSlowLDRef, py3ccBinary, py3ccSlowBin)
	}
}

func (e *EmitContext) emitEnginePyYapyc(ps PySrc, py3ccLDRef, py3ccSlowLDRef NodeRef, py3ccBinary, py3ccSlowBin VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	srcAbs := ps.Path

	var genInfo *GeneratedFileInfo
	var moduleName string

	if ps.Group == pyGroupGenAux {
		genInfo = e.codegen.mustInfo(ps.Path, "emitEnginePyYapyc")
		moduleName = srcAbs.rel() + "-"
	} else {
		genInfo = e.codegen.lookupSplit(instance.Path, ps.Token)

		if genInfo != nil {
			moduleName = ps.Token.string() + "-"
		} else {
			moduleName = srcAbs.rel() + "-"
		}
	}

	outputPath := e.pyYapycOutFor(ps)

	cmdArgs := na.chunkList([]STR{(py3ccBinary).str(), argSlowPy3cc.str(), (py3ccSlowBin).str()},
		na.strList(internStr(moduleName), (srcAbs).str(), (outputPath).str()))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}
	nodeInputs := na.inputList([]VFS{py3ccBinary, py3ccSlowBin}, na.srcChunk(srcAbs))

	var inputs []VFS

	if genInfo != nil {
		inputs = []VFS{srcAbs}
		inputs = append(inputs, genInfo.SourceInputs...)
		inputs = append(inputs, py3ccBinary, py3ccSlowBin)

		nodeInputs = na.inputList(inputs)
	}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:          env,
		Inputs:       nodeInputs,
		Outputs:      na.vfsList(outputPath),
		KV:           &pyCodegenKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}

	toolRefs := depRefs(py3ccLDRef, py3ccSlowLDRef)

	if genInfo != nil {
		if extras := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, inputs); len(extras) > 0 {
			node.DepRefs = append(node.DepRefs, extras...)
		}
	}

	node.ForeignDepRefs = toolRefs

	pyRef := ctx.emit.emitNode(node)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    outputPath,
		ProducerRef:   pyRef,
		GeneratorRefs: toolRefs,
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
		!strings.HasPrefix(instance.Path.rel(), "contrib/python") &&
		!strings.HasPrefix(instance.Path.rel(), "contrib/tools/python3") &&
		pyNamespaceUnitType(d.unit.Type)

	res := &ObjcopyEmitResult{}

	for gi, group := range e.collectPyGroups() {
		if namespaceEnabled {
			nsRefs, nsOuts := e.emitPyNamespaceForGroup(group)

			res.Refs = append(res.Refs, nsRefs...)
			res.Outputs = append(res.Outputs, nsOuts...)
		}

		var entries []PyGenResEntry

		for _, ps := range e.pySrcsReg {
			if ps.Group != gi {
				continue
			}

			entries = append(entries, e.pyResEntriesFor(ps)...)
		}

		if len(entries) == 0 {
			continue
		}

		groupRefs, groupOuts := e.packResources(ResourcePack{Tag: d.unit.Tag, Items: pyGenResourceItems(entries)})

		res.Refs = append(res.Refs, groupRefs...)
		res.Outputs = append(res.Outputs, groupOuts...)
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}

func (e *EmitContext) emitPyNamespaceForGroup(group PySrcGroup) ([]NodeRef, []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	pySources := make([]string, 0, len(group.Srcs))
	arcSources := make([]string, 0, len(group.Srcs))

	for _, srcRel := range group.Srcs {
		if !extIsPy(srcRel.string()) {
			continue
		}

		pySources = append(pySources, srcRel.string())

		if reg.lookupSplit(instance.Path, srcRel) == nil {
			arcSources = append(arcSources, srcRel.string())
		}
	}

	if len(pySources) == 0 || len(arcSources) == 0 {
		return nil, nil
	}

	nsPrefix := strings.ReplaceAll(instance.Path.rel(), "/", ".") + "."

	if group.Namespace != nil {
		nsPrefix = strings.TrimSuffix(group.Namespace.string(), ".") + "."
	}

	nsValue := nsPrefix

	if group.TopLevel {
		nsPrefix = ""
		nsValue = "."
	}

	h := md5.New()

	for _, srcRel := range pySources {
		modName := strings.TrimSuffix(srcRel, ".py")

		modName = strings.ReplaceAll(modName, "/", ".")

		mod := nsPrefix + modName

		h.Write([]byte(mod))
	}

	modListMD5 := enchex.EncodeToString(h.Sum(nil))
	nsRoots := make(map[string]struct{}, len(arcSources))

	for _, srcRel := range arcSources {
		resolvedRel := resolvePySrcRel(ctx.fs, d.srcDirs, instance.Path, srcRel).string()
		end := len(resolvedRel) - len(srcRel) - 1

		if end < 0 {
			end = 0
		}

		nsRoots[resolvedRel[:end]] = struct{}{}
	}

	keyPaths := make([]string, 0, len(nsRoots))

	for keyPath := range nsRoots {
		keyPaths = append(keyPaths, keyPath)
	}

	sort.Strings(keyPaths)

	kvsHash := make([]string, 0, len(keyPaths))
	kvsCmd := make([]string, 0, len(keyPaths))

	for _, keyPath := range keyPaths {
		key := "py/namespace/" + modListMD5 + "/" + keyPath

		kvsHash = append(kvsHash, key+"=\""+nsValue+"\"")
		kvsCmd = append(kvsCmd, key+"="+nsValue)
	}

	return e.emitKvOnlyResource(d.unit.Tag, kvsHash, kvsCmd)
}

func (e *EmitContext) emitPyMainObjcopy() ([]NodeRef, []VFS) {
	_, _, d := e.ctx, e.instance, e.d

	if d.pyMain == nil {
		return nil, nil
	}

	kv := "PY_MAIN=" + d.pyMain.string()

	return e.emitKvOnlyResource(d.unit.Tag, []string{kv}, []string{kv})
}

func (e *EmitContext) emitNoCheckImportsObjcopy() ([]NodeRef, []VFS) {
	_, _, d := e.ctx, e.instance, e.d

	if len(d.noCheckImports) == 0 {
		return nil, nil
	}

	value := strings.Join(strStrings(d.noCheckImports), " ")
	sum := md5.Sum([]byte(value))
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))

	b32 = strings.TrimRight(b32, "=")

	key := "py/no_check_imports/" + b32
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return e.emitKvOnlyResource(d.unit.Tag, []string{kvHash}, []string{kvCmd})
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
	moduleTag := d.unit.Tag

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := internV("resfs/src/", key, "=", res.sourcePath).string()
		input := source(res.sourcePath)

		refs, outs := e.packResources(ResourcePack{Tag: moduleTag, Items: []ResourceItem{
			{Path: "-", Key: kvHash, Cmd: kvCmd},
			{Path: res.hashPath, Key: key, Input: input},
		}})

		outRefs = append(outRefs, refs...)
		outPaths = append(outPaths, outs...)
	}

	return outRefs, outPaths
}

func (e *EmitContext) emitGeneratedPyAuxChunks() (refs []NodeRef, outs []VFS) {
	d := e.d

	for _, ps := range e.pySrcsReg {
		if ps.Group != pyGroupGenAux {
			continue
		}

		r, o := e.packResources(ResourcePack{Tag: d.unit.Tag, Items: pyGenResourceItems(e.pyResEntriesFor(ps)), RawClosure: func(aux VFS, inputs []VFS, ref NodeRef) Closure {
			return e.rawAuxInputClosure(aux, dedupSourceVFS(inputs, nil), ref)
		}})

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	return refs, outs
}

func (e *EmitContext) rawAuxInputClosure(aux VFS, seed []VFS, ref NodeRef) Closure {
	ctx, _, d := e.ctx, e.instance, e.d
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	emits := make([]IncludeDirective, 0, len(seed))

	for _, v := range seed {
		emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	var psc []ARG

	if p := d.perSrcCFlagsFor(aux.str()); p != nil {
		psc = *p
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     aux,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{rescompilerRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(aux.str()), ForceCxx: true, CFlags: concat(psc, []ARG{argX, argC})},
	})

	return walkClosure(e.scanner, aux, d.cc.ScanCfg)
}

type PyRegisterResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitPyRegister(py3Suffix bool) *PyRegisterResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.pyRegister) == 0 {
		return nil
	}

	res := &PyRegisterResult{}

	for i, arg := range d.pyRegister {
		priorShort := make(map[string]struct{}, i)

		for j := 0; j < i; j++ {
			if j < len(d.pyRegisterExplicit) && !d.pyRegisterExplicit[j] {
				continue
			}

			prior := d.pyRegister[j]
			priorStr := prior.string()

			priorShort[priorStr[strings.LastIndexByte(priorStr, '.')+1:]] = struct{}{}
		}

		regCpp := arg.string() + ".reg3.cpp"
		regCppVFS := build(instance.Path.rel(), "/", regCpp)
		regCppAbs := regCppVFS.string()
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		pyCmdArgs := []STR{
			d.tc.Python3,
			(genPy3RegScriptVFS).str(),
			internStr(arg.string()),
			internStr(regCppAbs),
		}

		pyNode := Node{
			Platform:     ctx.target,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(pyCmdArgs), Env: env}),
			Env:          env,
			Inputs:       na.inputList(genPy3RegScriptChunk),
			Outputs:      na.vfsList(regCppVFS),
			KV:           &pyCodegenKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		pyRef := ctx.emit.emitNode(pyNode)
		envCFlags := make([]ARG, 0, len(d.cc.CFlags))

		for _, f := range d.cc.CFlags {
			if short, ok := pyInitDefineShortname(f.string()); ok {
				if _, keep := priorShort[short]; !keep {
					continue
				}
			}

			envCFlags = append(envCFlags, f)
		}

		spec := &CompileSpec{Py3Suffix: py3Suffix, EnvCFlags: envCFlags}

		if len(d.cythonCpp) > 0 {
			spec.EnvAddIncl = appendCythonCCAddIncl(d.cc.AddIncl, d.cythonNumpyBeforeInclude)
		}

		e.codegen.register(&GeneratedFileInfo{
			OutputPath:    regCppVFS,
			ProducerRef:   pyRef,
			ClosureLeaves: []VFS{genPy3RegScriptVFS},
			Compile:       spec,
		})

		regRef, regOut := e.emitCC(regCppVFS)

		res.Refs = append(res.Refs, regRef)
		res.Outputs = append(res.Outputs, regOut)
	}

	return res
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

	return "", false
}

type PyGenResEntry struct {
	token  string
	key    string
	path   VFS
	inputs []VFS
}

func pyGenResourceItems(entries []PyGenResEntry) []ResourceItem {
	items := make([]ResourceItem, 0, 2*len(entries))

	for _, en := range entries {
		key := "resfs/file/py/" + en.key
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + en.token + "\"}"
		kvCmd := internV("resfs/src/", key, "=", en.path.rel()).string()

		items = append(items,
			ResourceItem{Path: "-", Key: kvHash, Cmd: kvCmd, Input: en.path, Extra: en.inputs},
			ResourceItem{Path: en.token, Key: key, Input: en.path, Extra: en.inputs})
	}

	return items
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

func pathIDBase32(path string) string {
	sum := md5.Sum([]byte(path))
	encoded := enc32.StdEncoding.EncodeToString(sum[:])

	encoded = strings.ToLower(encoded)

	return strings.TrimRight(encoded, "=")
}

func pyNamespaceUnitType(t TOK) bool {
	switch t {
	case tokPy3Library, tokPy3ProgramBin, tokPy23Library, tokPy23NativeLibrary:
		return true
	}

	return false
}
