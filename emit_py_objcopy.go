package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

var (
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
)

type ObjcopyEmitResult struct {
	Refs            []NodeRef
	Outputs         []VFS
	PySrcTrailCount int
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

	if info := codegenRegForInstance(ctx, instance).lookup(output); info != nil {
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

func emitResourceObjcopy(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	in ModuleCCInputs,
) *ObjcopyEmitResult {
	na := ctx.na

	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	oc := newObjcopyEmitCtx(ctx, d, instance.Platform)
	out := &ObjcopyEmitResult{}

	if nodeRes := emitPyMainObjcopy(ctx, instance, d, oc); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if nodeRes := emitNoCheckImportsObjcopy(ctx, instance, d, oc); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	for _, nodeRes := range emitYaConfJSONObjcopy(ctx, instance, d, oc) {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 {
		trailStart := len(out.Refs)
		srcRes := emitPySrcObjcopy(ctx, instance, d, oc)

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

		out.PySrcTrailCount = len(out.Refs) - trailStart

		return out
	}

	bad := []string{"${ARCADIA_BUILD_ROOT}", "${ARCADIA_SOURCE_ROOT}", "conftest.py"}
	contains := func(s string) bool {
		for _, b := range bad {
			if strings.Contains(s, b) {
				return true
			}
		}

		return false
	}

	type acc struct {
		paths         []string
		pathInputs    []VFS
		kvInputs      []VFS
		closureInputs []VFS

		mainOuts []VFS

		srcAttrInputs []VFS
		keys          []string
		kvs           []string
		kvsCmd        []string
		cmdLen        int
	}
	cur := acc{}
	moduleTag := resourceLibTagForData(d)

	cppProtoSubmodule := cfModuleTag(d, instance) == tagCppProto

	if cppProtoSubmodule {
		s := strCPPProto.string()
		moduleTag = &s
	}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}

		hash := objcopyHash(cur.paths, cur.keys, cur.kvs, instance.Path.rel(), moduleTag)
		outputObj := build(instance.Path.rel() + "/objcopy_" + hash + ".o")

		payload := make([]STR, 0, 2+len(cur.pathInputs)+len(cur.keys)+1+len(cur.kvs))

		if len(cur.paths) > 0 {
			payload = append(payload, argInputs.str())

			for _, p := range cur.pathInputs {
				payload = append(payload, (p).str())
			}

			payload = append(payload, argKeys.str())
			payload = appendInternStrs(payload, cur.keys)
		}

		if len(cur.kvs) > 0 {
			payload = append(payload, argKvs.str())

			for _, kv := range cur.kvsCmd {
				payload = append(payload, internStr(kv))
			}
		}

		cmdArgs := objcopyCmdArgs(oc, outputObj, payload)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		var inputs InputChunks

		if len(cur.paths) <= 1 {
			inputs = na.inputList(rescompilersWithScriptChunk, cur.pathInputs)
		} else {
			inputs = na.inputList(rescompilersChunk, cur.pathInputs, objcopyScriptChunk)
		}

		deduper.reset()

		for _, ch := range inputs {
			for _, p := range ch {
				deduper.add(p)
			}
		}

		var tail []VFS

		for _, p := range cur.closureInputs {
			if !deduper.add(p) {
				continue
			}

			tail = append(tail, p)
		}

		for _, p := range cur.kvInputs {
			if !deduper.add(p) {
				continue
			}

			tail = append(tail, p)
		}

		for _, p := range cur.srcAttrInputs {
			if !deduper.add(p) {
				continue
			}

			tail = append(tail, p)
		}

		if len(tail) > 0 {
			inputs = append(inputs, tail)
		}

		var mainTail []VFS

		for _, p := range cur.mainOuts {
			if p == 0 {
				continue
			}

			if !deduper.add(p) {
				continue
			}

			mainTail = append(mainTail, p)
		}

		if len(mainTail) > 0 {
			inputs = append(inputs, mainTail)
		}

		node := &Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: env}),
			Env:          env,
			Inputs:       inputs,
			Outputs:      na.vfsList(outputObj),
			KV:           &pyObjcopyKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    instance.Platform.UsesPython3Clang,
		}

		dataInputs := make([]VFS, 0, len(cur.pathInputs)+len(cur.closureInputs)+len(cur.kvInputs))
		dataInputs = append(dataInputs, cur.pathInputs...)
		dataInputs = append(dataInputs, cur.closureInputs...)
		dataInputs = append(dataInputs, cur.kvInputs...)

		node.DepRefs = resolveCodegenDepRefsIncl(ctx, instance, dataInputs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

		r := ctx.emit.emit(node)
		out.Refs = append(out.Refs, r)
		out.Outputs = append(out.Outputs, outputObj)
		cur = acc{}
	}

	emitEntries := func(entries []ResourceEntry) {
		for _, e := range entries {
			if !contains(e.Path) && !contains(e.Key) {
				if e.Path == "-" {
					cur.kvs = append(cur.kvs, e.Key)
					cur.cmdLen += rootCmdLen + len(e.Key)

					if inner, ok := rootrelInputPath(e.Key); ok {
						r := resolveResourceInput(ctx, instance, inner, copyFileInputVFS(ctx.fs, instance.Path.rel(), inner))
						cur.kvInputs = append(cur.kvInputs, r.Input)
						cur.mainOuts = append(cur.mainOuts, r.ProducerMainOut)
						cur.kvsCmd = append(cur.kvsCmd, renderResourceKvCmd(rootrelExpand(e.Key, r.Input.rel())))
					} else {
						cur.kvsCmd = append(cur.kvsCmd, renderResourceKvCmd(e.Key))
					}
				} else {
					r := resolveResourceInput(ctx, instance, e.Path, copyFileInputVFS(ctx.fs, instance.Path.rel(), e.Path))
					cur.paths = append(cur.paths, e.Path)
					cur.pathInputs = append(cur.pathInputs, r.Input)
					cur.mainOuts = append(cur.mainOuts, r.ProducerMainOut)

					if r.ProducerRef != 0 {
						for _, v := range walkClosureTail(ctx.scannerFor(instance), r.Input, in.ScanCfg) {
							if v.isBuild() {
								cur.closureInputs = append(cur.closureInputs, v)
							}
						}

						for _, v := range r.SourceInputs {
							if v.isSource() && objcopySourceLeafKept(v.rel()) {
								cur.srcAttrInputs = append(cur.srcAttrInputs, v)
							}
						}

						for _, v := range r.SourceClosure {
							if v.isSource() && objcopySourceLeafKept(v.rel()) {
								cur.srcAttrInputs = append(cur.srcAttrInputs, v)
							}
						}
					}

					kb := encb64.StdEncoding.EncodeToString([]byte(e.Key))
					cur.keys = append(cur.keys, kb)
					cur.cmdLen += rootCmdLen + len(e.Path) + len(kb)
				}
			}

			if cur.cmdLen > maxCmdLen || e.EndsBatch {
				flush()
			}
		}

		flush()
	}

	py3BinProgramSide := d.moduleStmt.Name == tokPy3Program && !d.programPairedLib

	if !py3BinProgramSide {
		emitEntries(d.resources)
	}

	trailStart := len(out.Refs)

	srcRes := emitPySrcObjcopy(ctx, instance, d, oc)

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	if !py3BinProgramSide {
		emitEntries(d.pyPyiResources)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}

type ObjcopyEmit struct {
	Ref NodeRef
	Out VFS
}

type KvOnlyKind int

const (
	kvOnlyBin KvOnlyKind = iota
	kvOnlyLib
)

func emitKvOnlyObjcopyNode(
	ctx *GenCtx,
	instance ModuleInstance,
	kind KvOnlyKind,
	kvsHash []string,
	kvsCmd []string,
	d *ModuleData,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	na := ctx.na

	var moduleTag *string

	switch kind {
	case kvOnlyLib:
		moduleTag = resourceLibTagForData(d)
	default:
		moduleTag = resourceBinTagForData(d)
	}

	hash := objcopyHash(nil, nil, kvsHash, instance.Path.rel(), moduleTag)
	outputObj := build(instance.Path.rel() + "/objcopy_" + hash + ".o")

	payload := appendInternStrs([]STR{argKvs.str()}, kvsCmd)
	cmdArgs := objcopyCmdArgs(oc, outputObj, payload)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(rescompilersWithScriptChunk),
		Outputs:      na.vfsList(outputObj),
		KV:           &pyObjcopyKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesPython3Clang,
	}

	node.DepRefs = append(node.DepRefs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

	ref := ctx.emit.emit(node)

	return &ObjcopyEmit{Ref: ref, Out: outputObj}
}

func emitYaConfJSONObjcopy(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	oc *ObjcopyEmitCtx,
) []*ObjcopyEmit {
	na := ctx.na

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
		keyB64 := encb64.StdEncoding.EncodeToString([]byte(key))
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + res.sourcePath
		hash := objcopyHash([]string{res.hashPath}, []string{keyB64}, []string{kvHash}, instance.Path.rel(), moduleTag)
		outputObj := build(instance.Path.rel() + "/objcopy_" + hash + ".o")
		input := source(res.sourcePath)

		cmdArgs := objcopyCmdArgs(oc, outputObj, []STR{
			argInputs.str(), (input).str(),
			argKeys.str(), internStr(keyB64),
			argKvs.str(), internStr(kvCmd),
		})
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
		node := &Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: env}),
			Env:          env,
			Inputs:       na.inputList(rescompilersChunk, na.vfsList(input, objcopyScriptVFS)),
			Outputs:      na.vfsList(outputObj),
			KV:           &pyObjcopyKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    instance.Platform.UsesPython3Clang,
		}

		node.DepRefs = append(node.DepRefs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

		out = append(out, &ObjcopyEmit{Ref: ctx.emit.emit(node), Out: outputObj})
	}

	return out
}

func emitPyNamespaceForGroup(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	group PySrcGroup,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	reg := codegenRegForInstance(ctx, instance)
	pySources := make([]string, 0, len(group.Srcs))
	arcSources := make([]string, 0, len(group.Srcs))

	for _, srcRel := range group.Srcs {
		if !strings.HasSuffix(srcRel.string(), ".py") {
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

	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyLib, kvsHash, kvsCmd, d, oc)
}

func emitPyMainObjcopy(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
	if d.pyMain == nil {
		return nil
	}

	kv := "PY_MAIN=" + d.pyMain.string()

	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyBin, []string{kv}, []string{kv}, d, oc)
}

func emitNoCheckImportsObjcopy(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmit {
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

	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyBin, []string{kvHash}, []string{kvCmd}, d, oc)
}

func emitPySrcObjcopy(
	ctx *GenCtx,
	instance ModuleInstance,
	d *ModuleData,
	oc *ObjcopyEmitCtx,
) *ObjcopyEmitResult {
	na := ctx.na

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
			if nsRes := emitPyNamespaceForGroup(ctx, instance, d, group, oc); nsRes != nil {
				res.Refs = append(res.Refs, nsRes.Ref)
				res.Outputs = append(res.Outputs, nsRes.Out)
			}
		}

		entries := buildPySrcEntriesFor(codegenRegForInstance(ctx, instance), ctx.fs, d, instance.Path.rel(), strStrings(group.Srcs), group.TopLevel, group.Namespace)

		if len(entries) == 0 {
			continue
		}

		for _, ch := range chunkPySrcEntries(entries) {
			hash := objcopyHash(ch.paths, ch.keys, ch.kvsHash, instance.Path.rel(), moduleTag)
			outputObj := build(instance.Path.rel() + "/objcopy_" + hash + ".o")

			payload := make([]STR, 0, 2+len(ch.pathInps)+len(ch.keys)+1+len(ch.kvsCmd))
			payload = append(payload, argInputs.str())

			for _, p := range ch.pathInps {
				payload = append(payload, (p).str())
			}

			payload = append(payload, argKeys.str())
			payload = appendInternStrs(payload, ch.keys)

			if len(ch.kvsCmd) > 0 {
				payload = append(payload, argKvs.str())
				payload = appendInternStrs(payload, ch.kvsCmd)
			}

			cmdArgs := objcopyCmdArgs(oc, outputObj, payload)

			env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

			node := &Node{
				Platform:     instance.Platform,
				Cmds:         na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
				Env:          env,
				Inputs:       na.inputList(rescompilersChunk, ch.inps, objcopyScriptChunk),
				Outputs:      na.vfsList(outputObj),
				KV:           &pyObjcopyKV,
				Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				Resources:    instance.Platform.UsesPython3Clang,
			}

			node.DepRefs = resolveCodegenDepRefsIncl(ctx, instance, ch.inps, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

			r := ctx.emit.emit(node)
			res.Refs = append(res.Refs, r)
			res.Outputs = append(res.Outputs, outputObj)
		}
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}

var (
	pyObjcopyKV = KV{P: pkPY, PC: pcYellow, ShowOut: true}
)
