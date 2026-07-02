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

type PySrcEntry struct {
	pathHash    string
	pathInput   VFS
	key         string
	kvHash      string
	kvCmd       string
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

func pySrcYapycSuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}

func buildPySrcEntriesFor(reg *CodegenRegistry, fs FS, d *ModuleData, modulePath string, srcs []string, topLevel bool, namespace *STR) []PySrcEntry {
	keyPrefix := pyResourceKeyPrefix(topLevel, namespace, modulePath)
	fullName := make(map[string]bool, len(d.pySrcs))

	for i, s := range d.pySrcs {
		if i < len(d.pySrcsFullName) && d.pySrcsFullName[i] {
			fullName[s.string()] = true
		}
	}

	out := make([]PySrcEntry, 0, len(srcs)*2)

	for _, srcRel := range srcs {
		suffix := ".yapyc3"

		if strings.Contains(srcRel, "/") {
			suffix = "." + d.pyYapycSuffix + ".yapyc3"
		}

		resolvedRel := resolvePySrcRel(fs, d.srcDirs, modulePath, srcRel)
		genInfo := reg.lookupSplit(dirKey(modulePath), internStr(srcRel))
		generated := genInfo != nil
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
				extraInputs: []VFS{srcEdge},
			})
		}
	}

	return out
}

func (e *EmitContext) emitPySrcs() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.pySrcs) == 0 {
		return
	}

	if d.pyBuildNoPYC {
		return
	}

	py3ccLDRef, py3ccBinary := ctx.tool(argToolsPy3cc)
	py3ccSlowLDRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	ctx.tool(argToolsRescompiler)
	ctx.tool(argToolsRescompressor)
	ctx.tool(argToolsArchiver)

	py3ccToolsChunk := []VFS{py3ccBinary, py3ccSlowBin}
	py3ccArgHead := []STR{(py3ccBinary).str(), argSlowPy3cc.str(), (py3ccSlowBin).str()}
	reg := e.codegen

	for i, srcRel := range d.pySrcs {
		if extIsPyi(srcRel.string()) {
			continue
		}

		genInfo := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		var generatedInputs []VFS

		if genInfo != nil {
			generatedInputs = genInfo.SourceInputs
		}

		srcAbs := resolveSourceVFS(ctx, instance, srcRel.string(), d.srcDirs)
		moduleName := srcAbs.rel() + "-"

		if genInfo != nil {
			srcAbs = build(instance.Path.rel(), "/", srcRel.string())

			if i < len(d.pySrcsFullName) && d.pySrcsFullName[i] {
				moduleName = srcAbs.rel() + "-"
			} else {
				moduleName = srcRel.string() + "-"
			}
		}

		var outputPath VFS

		if strings.Contains(srcRel.string(), "/") {
			outputPath = build(instance.Path.rel(), "/", srcRel.string(), ".", d.pyYapycSuffix, ".yapyc3")
		} else {
			outputPath = build(instance.Path.rel(), "/", srcRel.string(), ".yapyc3")
		}

		cmdArgs := na.chunkList(py3ccArgHead, na.strList(internStr(moduleName),
			(srcAbs).str(),
			(outputPath).str()))

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}
		nodeInputs := na.inputList(py3ccToolsChunk, na.srcChunk(srcAbs))

		var inputs []VFS

		if genInfo != nil {
			inputs = []VFS{srcAbs}
			inputs = append(inputs, generatedInputs...)
			inputs = append(inputs, py3ccBinary, py3ccSlowBin)

			if len(inputs) > 4 {
				toolA := inputs[len(inputs)-2]
				toolB := inputs[len(inputs)-1]

				copy(inputs[4:], inputs[2:len(inputs)-2])
				inputs[2] = toolA
				inputs[3] = toolB
			}

			nodeInputs = na.inputList(inputs)
		}

		node := &Node{
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

		pyRef := ctx.emit.emit(node)

		reg.register(&GeneratedFileInfo{
			OutputPath:     outputPath,
			ProducerRef:    pyRef,
			GeneratorRefs:  toolRefs,
			ParsedIncludes: nil,
		})
	}
}

func (e *EmitContext) emitResourceObjcopy() *ObjcopyEmitResult {
	_, instance, d := e.ctx, e.instance, e.d
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	out := &ObjcopyEmitResult{}

	if nodeRes := e.emitPyMainObjcopy(); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if nodeRes := e.emitNoCheckImportsObjcopy(); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	for _, nodeRes := range e.emitYaConfJSONObjcopy() {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 {
		trailStart := len(out.Refs)
		srcRes := e.emitPySrcObjcopy()

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

		out.PySrcTrailCount = len(out.Refs) - trailStart

		return out
	}

	moduleTag := resourceLibTagForData(d)

	if cfModuleTag(d, instance) == tagCppProto {
		s := strCPPProto.string()

		moduleTag = &s
	}

	py3BinProgramSide := d.moduleStmt.Name == tokPy3Program && !d.programPairedLib

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(d.resources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	trailStart := len(out.Refs)
	srcRes := e.emitPySrcObjcopy()

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(d.pyPyiResources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}

func (e *EmitContext) emitPySrcObjcopy() *ObjcopyEmitResult {
	ctx, instance, d := e.ctx, e.instance, e.d

	if len(d.pySrcs) == 0 {
		return nil
	}

	if resourceLibTagForData(d) == nil {
		return nil
	}

	if d.moduleStmt.Name == tokPy3Program && !d.programPairedLib {
		return nil
	}

	groups := d.pySrcGroups

	if len(groups) == 0 {
		groups = []PySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	namespaceEnabled := !d.noExtendedPySearch &&
		!strings.HasPrefix(instance.Path.rel(), "contrib/python") &&
		!strings.HasPrefix(instance.Path.rel(), "contrib/tools/python3") &&
		resourceModuleTag(d.moduleStmt.Name) != nil

	moduleTag := resourceLibTagForData(d)
	res := &ObjcopyEmitResult{}

	for _, group := range groups {
		if namespaceEnabled {
			if nsRes := e.emitPyNamespaceForGroup(group); nsRes != nil {
				res.Refs = append(res.Refs, nsRes.Ref)
				res.Outputs = append(res.Outputs, nsRes.Out)
			}
		}

		entries := buildPySrcEntriesFor(e.codegen, ctx.fs, d, instance.Path.rel(), strStrings(group.Srcs), group.TopLevel, group.Namespace)

		if len(entries) == 0 {
			continue
		}

		items := make([]ResourceItem, 0, 2*len(entries))

		for _, en := range entries {
			items = append(items,
				ResourceItem{Path: "-", Key: en.kvHash, Cmd: en.kvCmd, Input: en.pathInput, Extra: en.extraInputs},
				ResourceItem{Path: en.pathHash, Key: en.key, Input: en.pathInput, Extra: en.extraInputs})
		}

		groupRefs, groupOuts := e.packResources(ResourcePack{Tag: moduleTag, Items: items})

		res.Refs = append(res.Refs, groupRefs...)
		res.Outputs = append(res.Outputs, groupOuts...)
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}

func (e *EmitContext) emitPyNamespaceForGroup(group PySrcGroup) *ObjcopyEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	pySources := make([]string, 0, len(group.Srcs))
	arcSources := make([]string, 0, len(group.Srcs))

	for _, srcRel := range group.Srcs {
		if !extIsPy(srcRel.string()) {
			continue
		}

		pySources = append(pySources, srcRel.string())

		if reg.lookupSplit(dirKey(instance.Path.rel()), srcRel) == nil {
			arcSources = append(arcSources, srcRel.string())
		}
	}

	if len(pySources) == 0 || len(arcSources) == 0 {
		return nil
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
		resolvedRel := resolvePySrcRel(ctx.fs, d.srcDirs, instance.Path.rel(), srcRel)
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

	return e.emitKvOnlyResource(resourceLibTagForData(d), kvsHash, kvsCmd)
}

func (e *EmitContext) emitPyMainObjcopy() *ObjcopyEmit {
	_, _, d := e.ctx, e.instance, e.d

	if d.pyMain == nil {
		return nil
	}

	kv := "PY_MAIN=" + d.pyMain.string()

	return e.emitKvOnlyResource(resourceBinTagForData(d), []string{kv}, []string{kv})
}

func (e *EmitContext) emitNoCheckImportsObjcopy() *ObjcopyEmit {
	_, _, d := e.ctx, e.instance, e.d

	if len(d.noCheckImports) == 0 {
		return nil
	}

	value := strings.Join(strStrings(d.noCheckImports), " ")
	sum := md5.Sum([]byte(value))
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))

	b32 = strings.TrimRight(b32, "=")

	key := "py/no_check_imports/" + b32
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value

	return e.emitKvOnlyResource(resourceBinTagForData(d), []string{kvHash}, []string{kvCmd})
}

func (e *EmitContext) emitYaConfJSONObjcopy() []*ObjcopyEmit {
	ctx, _, d := e.ctx, e.instance, e.d

	if len(d.yaConfJSON) == 0 {
		return nil
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

	out := make([]*ObjcopyEmit, 0, len(resources))
	moduleTag := resourceLibTagForData(d)

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + res.sourcePath
		input := source(res.sourcePath)

		refs, outs := e.packResources(ResourcePack{Tag: moduleTag, Items: []ResourceItem{
			{Path: "-", Key: kvHash, Cmd: kvCmd},
			{Path: res.hashPath, Key: key, Input: input},
		}})

		for i, ref := range refs {
			out = append(out, &ObjcopyEmit{Ref: ref, Out: outs[i]})
		}
	}

	return out
}

func (e *EmitContext) emitGeneratedPyAuxChunks() *PyGenResourcesResult {
	_, instance, d := e.ctx, e.instance, e.d

	if len(d.pySrcs) == 0 {
		return nil
	}

	reg := e.codegen

	var entries []PyGenResEntry

	for i, srcRel := range d.pySrcs {
		info := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		if info == nil {
			continue
		}

		if i >= len(d.pySrcsFullName) || !d.pySrcsFullName[i] {
			continue
		}

		genInputs := info.SourceInputs
		src := build(instance.Path.rel(), "/", srcRel.string())

		entries = append(entries, PyGenResEntry{
			token:  "${ARCADIA_BUILD_ROOT}/" + src.rel(),
			key:    generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()),
			path:   src,
			inputs: genInputs,
		})

		if !d.pyBuildNoPYC {
			suffix := ".yapyc3"

			if strings.Contains(srcRel.string(), "/") {
				suffix = "." + d.pyYapycSuffix + ".yapyc3"
			}

			yp := build(instance.Path.rel(), "/", srcRel.string(), suffix)

			entries = append(entries, PyGenResEntry{
				token:  "${ARCADIA_BUILD_ROOT}/" + yp.rel(),
				key:    generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()+".yapyc3"),
				path:   yp,
				inputs: genInputs,
			})
		}
	}

	return e.emitPyGenResources(entries, "PY3", func(aux VFS, inputs []VFS, ref NodeRef) []VFS {
		return e.rawAuxInputClosure(aux, pyProtoSourceInputs(inputs), ref)
	})
}

func (e *EmitContext) rawAuxInputClosure(aux VFS, seed []VFS, ref NodeRef) []VFS {
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
		ParsedIncludes: emits,
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(aux.str()), ForceCxx: true, CFlags: concat(psc, []ARG{argX, argC})},
	})

	closure := walkClosure(e.scanner, aux, d.cc.ScanCfg)

	if len(closure) == 0 {
		return nil
	}

	return closure
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

		pyNode := &Node{
			Platform:     ctx.target,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(pyCmdArgs), Env: env}),
			Env:          env,
			Inputs:       na.inputList(genPy3RegScriptChunk),
			Outputs:      na.vfsList(regCppVFS),
			KV:           &pyCodegenKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}

		pyRef := ctx.emit.emit(pyNode)
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

type PyGenResourcesResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitPyGenResources(entries []PyGenResEntry, hashTag string, closure func(aux VFS, inputs []VFS, ref NodeRef) []VFS) *PyGenResourcesResult {
	if len(entries) == 0 {
		return nil
	}

	refs, outs := e.packResources(ResourcePack{Tag: stringPtr(hashTag), Items: pyGenResourceItems(entries), RawClosure: closure})

	if len(refs) == 0 {
		return nil
	}

	return &PyGenResourcesResult{Refs: refs, Outputs: outs}
}

type PyGenYapycResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitPyGenYapyc(pyOutputs []VFS, tokens []string, producerRef NodeRef, sourceInputs []VFS) *PyGenYapycResult {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin := py3ccToolRefs(ctx, instance)
	suffix := pySrcYapycSuffix(instance.Path.rel())
	res := &PyGenYapycResult{}

	for i, pyOut := range pyOutputs {
		uniq := ""

		if strings.Contains(tokens[i], "/") {
			uniq = "." + suffix
		}

		out := build(pyOut.rel(), uniq, ".yapyc3")

		cmdArgs := []STR{
			(py3ccBinary).str(),
			argSlowPy3cc.str(),
			(py3ccSlowBin).str(),
			internV(tokens[i], "-"),
			(pyOut).str(),
			(out).str(),
		}

		deps := []NodeRef{producerRef}
		toolRefs := depRefs(py3ccRef, py3ccSlowRef)
		nodeInputs := na.inputList(na.vfsList(py3ccBinary, py3ccSlowBin, pyOut), sourceInputs)

		if i > 0 {
			nodeInputs = append(nodeInputs, []VFS{pyOutputs[0]})
		}

		node := &Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}}}),
			Env:          EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envPYTHONHASHSEED, Value: strZero}},
			Inputs:       nodeInputs,
			Outputs:      na.vfsList(out),
			KV:           &pyCodegenKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
			Resources:    usesPython3,
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = toolRefs
		}

		ref := ctx.emit.emit(node)

		e.codegen.register(&GeneratedFileInfo{OutputPath: out, ProducerRef: ref})

		res.Refs = append(res.Refs, ref)
		res.Outputs = append(res.Outputs, out)
	}

	return res
}

func pyProtoSourceInputs(inputs []VFS) []VFS {
	out := make([]VFS, 0, len(inputs))

	deduper.reset()

	for _, input := range inputs {
		if !input.isSource() {
			continue
		}

		if !deduper.add(input) {
			continue
		}

		out = append(out, input)
	}

	return out
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

func py3ccToolRefs(ctx *GenCtx, instance ModuleInstance) (NodeRef, NodeRef, VFS, VFS) {
	py3ccRef, py3ccBinary := ctx.tool(argToolsPy3cc)
	py3ccSlowRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)

	return py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin
}

func protoPathID(path string) string {
	sum := md5.Sum([]byte(path))
	encoded := enc32.StdEncoding.EncodeToString(sum[:])

	encoded = strings.ToLower(encoded)

	return strings.TrimRight(encoded, "=")
}
