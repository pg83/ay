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
	// Fixed tool/script prefixes of the objcopy nodes' inputs, shared as chunks
	// (referenced, never copied per node).
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
)

type objcopyEmitResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func emitResourceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
) *objcopyEmitResult {
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	rescompilerLDRef, _ := ctx.tool(argToolsRescompiler)
	rescompressorLDRef, _ := ctx.tool(argToolsRescompressor)
	out := &objcopyEmitResult{}

	if nodeRes := emitPyMainObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if nodeRes := emitNoCheckImportsObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	for _, nodeRes := range emitYaConfJSONObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef) {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
	}

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 {
		srcRes := emitPySrcObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef)

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

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
		paths       []string
		pathInputs  []VFS
		extraInputs []VFS
		kvInputs    []VFS
		pathDeps    []NodeRef
		keys        []string
		kvs         []string
		cmdLen      int
	}
	cur := acc{}
	var moduleTag *string

	if d.moduleStmt != nil {
		moduleTag = resourceLibTagForData(d)
	}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}

		hash := objcopyHash(cur.paths, cur.keys, cur.kvs, instance.Path.Rel(), moduleTag)
		outputObj := Build(instance.Path.Rel() + "/objcopy_" + hash + ".o")

		cmdArgs := []STR{
			d.tc.Python3,
			internStr(objcopyScriptPath),
			argCompiler.str(), d.tc.CXX,
			argObjcopy.str(), d.tc.Objcopy,
			argCompressor.str(), internStr(rescompressorBinPath),
			argRescompiler.str(), internStr(rescompilerBinPath),
			argOutputObj.str(), (outputObj).str(),
			argTarget.str(), internStr(instance.Platform.Triple),
		}

		if len(cur.paths) > 0 {
			cmdArgs = append(cmdArgs, argInputs.str())

			for _, p := range cur.pathInputs {
				cmdArgs = append(cmdArgs, (p).str())
			}

			cmdArgs = append(cmdArgs, argKeys.str())
			cmdArgs = appendInternStrs(cmdArgs, cur.keys)
		}

		if len(cur.kvs) > 0 {
			cmdArgs = append(cmdArgs, argKvs.str())

			for _, kv := range cur.kvs {
				cmdArgs = append(cmdArgs, internStr(expandRootrel(kv, instance.Path.Rel())))
			}
		}

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		var inputs inputChunks

		if len(cur.paths) <= 1 {
			inputs = inputChunks{rescompilersWithScriptChunk, cur.pathInputs, cur.extraInputs}
		} else {
			inputs = inputChunks{rescompilersChunk, cur.pathInputs, objcopyScriptChunk, cur.extraInputs}
		}

		deduper.reset()

		for _, ch := range inputs {
			for _, p := range ch {
				deduper.add(p)
			}
		}

		var kvTail []VFS

		for _, p := range cur.kvInputs {
			if !deduper.add(p) {
				continue
			}

			kvTail = append(kvTail, p)
		}

		if len(kvTail) > 0 {
			inputs = append(inputs, kvTail)
		}

		resTargetProps := TargetProperties{ModuleDir: instance.Path.Rel()}

		if d.moduleStmt != nil {
			switch d.moduleStmt.Name {
			case tokPy23Library, tokPy23NativeLibrary:
				resTargetProps.ModuleTag = tagPy3
			case tokPy3Program:
				resTargetProps.ModuleTag = tagPy3Bin
			}
		}

		node := &Node{
			Platform: instance.Platform,
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:              env,
			Inputs:           inputs,
			Outputs:          []VFS{outputObj},
			KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
			TargetProperties: resTargetProps,
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
		}

		if rescompilerLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}

		if rescompressorLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		// The inputs set above is complete by now, so the deduper is free for the
		// dep-ref set.
		deduper.reset()

		for _, ref := range cur.pathDeps {
			if ref == (NodeRef(0)) {
				continue
			}

			if !deduper.add(VFS(ref)) {
				continue
			}

			node.DepRefs = append(node.DepRefs, ref)
		}

		r := ctx.emit.Emit(node)
		out.Refs = append(out.Refs, r)
		out.Outputs = append(out.Outputs, outputObj)
		cur = acc{}
	}

	emitEntries := func(entries []resourceEntry) {
		for _, e := range entries {
			if !contains(e.Path) && !contains(e.Key) {
				if e.Path == "-" {
					cur.kvs = append(cur.kvs, e.Key)
					cur.cmdLen += rootCmdLen + len(e.Key)

					if inner, ok := rootrelInputPath(e.Key); ok {
						cur.kvInputs = append(cur.kvInputs, Source(instance.Path.Rel()+"/"+inner))
					}
				} else {
					inputVFS := copyFileInputVFS(ctx.fs, instance.Path.Rel(), e.Path)
					// Producer keys (RUN_PROGRAM OUTFiles, COPY outputs, etc.)
					// are stored in expanded form ($(B)/<unit>/X), but RESOURCE
					// pair.Path is kept raw (${BINDIR}/X) to match upstream's
					// objcopy_<hash>. Lookup canonicalizes by VFS string.
					var producerRef NodeRef

					if d.prOutputProducer != nil {
						canonKey := inputVFS.String()

						if ref, ok := d.prOutputProducer[canonKey]; ok {
							inputVFS = copyFileOutputVFS(instance.Path.Rel(), e.Path)
							producerRef = ref
							cur.extraInputs = dedupVFS(cur.extraInputs, prResourceExtraInputs(d, canonKey))
						} else if ref, ok := d.prOutputProducer[e.Path]; ok {
							inputVFS = copyFileOutputVFS(instance.Path.Rel(), e.Path)
							producerRef = ref
							cur.extraInputs = dedupVFS(cur.extraInputs, prResourceExtraInputs(d, e.Path))
						}
					}

					cur.paths = append(cur.paths, e.Path)
					cur.pathInputs = append(cur.pathInputs, inputVFS)
					cur.pathDeps = append(cur.pathDeps, producerRef)
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

	emitEntries(d.resources)

	srcRes := emitPySrcObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef)

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	emitEntries(d.pyPyiResources)

	return out
}

type objcopyEmit struct {
	Ref NodeRef
	Out VFS
}

// kvOnlyKind selects the upstream submodule whose MODULE_TAG the kv-only
// objcopy emission inherits — PY_MAIN / NO_CHECK_IMPORTS belong to the
// PY3_BIN submodule (PROGRAM-side), py/namespace and RESOURCE_FILES belong to
// PY3_BIN_LIB (LIBRARY-side).
type kvOnlyKind int

const (
	kvOnlyBin kvOnlyKind = iota
	kvOnlyLib
)

func emitKvOnlyObjcopyNode(
	ctx *genCtx,
	instance ModuleInstance,
	kind kvOnlyKind,
	kvsHash []string,
	kvsCmd []string,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	var moduleTag *string

	switch kind {
	case kvOnlyLib:
		moduleTag = resourceLibTagForData(d)
	default:
		moduleTag = resourceBinTagForData(d)
	}

	hash := objcopyHash(nil, nil, kvsHash, instance.Path.Rel(), moduleTag)
	outputObj := Build(instance.Path.Rel() + "/objcopy_" + hash + ".o")

	cmdArgs := []STR{
		d.tc.Python3,
		internStr(objcopyScriptPath),
		argCompiler.str(), d.tc.CXX,
		argObjcopy.str(), d.tc.Objcopy,
		argCompressor.str(), internStr(rescompressorBinPath),
		argRescompiler.str(), internStr(rescompilerBinPath),
		argOutputObj.str(), (outputObj).str(),
		argTarget.str(), internStr(instance.Platform.Triple),
		argKvs.str(),
	}
	cmdArgs = appendInternStrs(cmdArgs, kvsCmd)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	targetProps := TargetProperties{ModuleDir: instance.Path.Rel()}

	switch d.moduleStmt.Name {
	case tokPy23Library, tokPy23NativeLibrary:
		targetProps.ModuleTag = tagPy3
	}

	if d.moduleStmt.Name == tokPy3Program || d.programPairedLib {
		if kind == kvOnlyLib {
			targetProps.ModuleTag = tagPy3BinLib
		} else {
			targetProps.ModuleTag = tagPy3Bin
		}
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputChunks{rescompilersWithScriptChunk},
		Outputs:          []VFS{outputObj},
		KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
	}

	if rescompilerLDRef != (NodeRef(0)) {
		node.DepRefs = append(node.DepRefs, rescompilerLDRef)
	}

	if rescompressorLDRef != (NodeRef(0)) {
		node.DepRefs = append(node.DepRefs, rescompressorLDRef)
	}

	ref := ctx.emit.Emit(node)
	return &objcopyEmit{Ref: ref, Out: outputObj}
}

func emitYaConfJSONObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) []*objcopyEmit {
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
			sourcePath: file,
			keyPath:    "ya.conf.json",
			hashPath:   "ya.conf.json",
		})
		formulas := yaConfFormulaResources(ctx.fs, file)
		sort.Strings(formulas)

		for _, formula := range formulas {
			resources = append(resources, yaConfResource{
				sourcePath: formula,
				keyPath:    formula,
				hashPath:   formula,
			})
		}
	}

	out := make([]*objcopyEmit, 0, len(resources))
	var moduleTag *string

	if d.moduleStmt != nil {
		moduleTag = resourceLibTagForData(d)
	}

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		keyB64 := encb64.StdEncoding.EncodeToString([]byte(key))
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + res.sourcePath
		hash := objcopyHash([]string{res.hashPath}, []string{keyB64}, []string{kvHash}, instance.Path.Rel(), moduleTag)
		outputObj := Build(instance.Path.Rel() + "/objcopy_" + hash + ".o")
		input := Source(res.sourcePath)

		cmdArgs := []STR{
			d.tc.Python3,
			internStr(objcopyScriptPath),
			argCompiler.str(), d.tc.CXX,
			argObjcopy.str(), d.tc.Objcopy,
			argCompressor.str(), internStr(rescompressorBinPath),
			argRescompiler.str(), internStr(rescompilerBinPath),
			argOutputObj.str(), (outputObj).str(),
			argTarget.str(), internStr(instance.Platform.Triple),
			argInputs.str(), (input).str(),
			argKeys.str(), internStr(keyB64),
			argKvs.str(), internStr(kvCmd),
		}
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
		node := &Node{
			Platform: instance.Platform,
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:              env,
			Inputs:           inputChunks{rescompilersChunk, {input, objcopyScriptVFS}},
			Outputs:          []VFS{outputObj},
			KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
		}

		if rescompilerLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}

		if rescompressorLDRef != (NodeRef(0)) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		out = append(out, &objcopyEmit{Ref: ctx.emit.Emit(node), Out: outputObj})
	}

	return out
}

func emitPyNamespaceForGroup(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	group pySrcGroup,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	pySources := make([]string, 0, len(group.Srcs))

	for _, srcRel := range group.Srcs {
		if strings.HasSuffix(srcRel, ".py") {
			pySources = append(pySources, srcRel)
		}
	}

	if len(pySources) == 0 {
		return nil
	}

	nsPrefix := strings.ReplaceAll(instance.Path.Rel(), "/", ".") + "."

	if group.Namespace != nil {
		nsPrefix = strings.TrimSuffix(*group.Namespace, ".") + "."
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

	keyPath := instance.Path.Rel()

	// A TOP_LEVEL group keys the namespace by the dir its sources resolve
	// against — the last declared SRCDIR (srcDirs[0] is the module dir).
	if group.TopLevel && len(d.srcDirs) > 1 {
		keyPath = d.srcDirs[len(d.srcDirs)-1].Rel()
	}

	key := "py/namespace/" + modListMD5 + "/" + keyPath
	kvHash := key + "=\"" + nsValue + "\""
	kvCmd := key + "=" + nsValue
	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyLib, []string{kvHash}, []string{kvCmd}, d, rescompilerLDRef, rescompressorLDRef)
}

func emitPyMainObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	if d.pyMain == nil || d.moduleStmt == nil {
		return nil
	}

	kv := "PY_MAIN=" + *d.pyMain
	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyBin, []string{kv}, []string{kv}, d, rescompilerLDRef, rescompressorLDRef)
}

func emitNoCheckImportsObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	if len(d.noCheckImports) == 0 || d.moduleStmt == nil {
		return nil
	}

	value := strings.Join(d.noCheckImports, " ")
	sum := md5.Sum([]byte(value))
	b32 := strings.ToLower(enc32.StdEncoding.EncodeToString(sum[:]))
	b32 = strings.TrimRight(b32, "=")
	key := "py/no_check_imports/" + b32
	kvHash := key + "=\"" + value + "\""
	kvCmd := key + "=" + value
	return emitKvOnlyObjcopyNode(ctx, instance, kvOnlyBin, []string{kvHash}, []string{kvCmd}, d, rescompilerLDRef, rescompressorLDRef)
}

func emitPySrcObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmitResult {
	if len(d.pySrcs) == 0 || d.moduleStmt == nil {
		return nil
	}

	if resourceLibTagForData(d) == nil {
		return nil
	}

	// PY3_PROGRAM PROGRAM-side mirrors upstream's PY3_BIN submodule, which has
	// ENABLE(PROCESS_PY_MAIN_ONLY) (conf/python.conf:352). onpy_srcs honours
	// that flag (pybuild.py:266,400) by skipping all pys/namespace processing
	// after PY_MAIN handling — the LIBRARY twin (PY3_BIN_LIB) is responsible
	// for emitting pysrc + namespace objcopies and packing them into its
	// .global.a, which the PROGRAM links via PEERDIRSELF=PY3_BIN_LIB. Emitting
	// from the PROGRAM side here would either double-link the objcopies into
	// the LD command or produce a tag-divergent twin.
	if d.moduleStmt.Name == tokPy3Program && !d.programPairedLib {
		return nil
	}

	groups := d.pySrcGroups

	if len(groups) == 0 {
		groups = []pySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	namespaceEnabled := !d.noExtendedPySearch &&
		!strings.HasPrefix(instance.Path.Rel(), "contrib/python") &&
		!strings.HasPrefix(instance.Path.Rel(), "contrib/tools/python3") &&
		resourceModuleTag(d.moduleStmt.Name) != nil

	moduleTag := resourceLibTagForData(d)
	res := &objcopyEmitResult{}

	for _, group := range groups {
		if namespaceEnabled {
			if nsRes := emitPyNamespaceForGroup(ctx, instance, d, group, rescompilerLDRef, rescompressorLDRef); nsRes != nil {
				res.Refs = append(res.Refs, nsRes.Ref)
				res.Outputs = append(res.Outputs, nsRes.Out)
			}
		}

		entries := buildPySrcEntriesFor(ctx.fs, d, instance.Path.Rel(), group.Srcs, group.TopLevel, group.Namespace)

		if len(entries) == 0 {
			continue
		}

		for _, ch := range chunkPySrcEntries(entries) {
			hash := objcopyHash(ch.paths, ch.keys, ch.kvsHash, instance.Path.Rel(), moduleTag)
			outputObj := Build(instance.Path.Rel() + "/objcopy_" + hash + ".o")

			cmdArgs := []STR{
				d.tc.Python3,
				internStr(objcopyScriptPath),
				argCompiler.str(), d.tc.CXX,
				argObjcopy.str(), d.tc.Objcopy,
				argCompressor.str(), internStr(rescompressorBinPath),
				argRescompiler.str(), internStr(rescompilerBinPath),
				argOutputObj.str(), (outputObj).str(),
				argTarget.str(), internStr(instance.Platform.Triple),
			}

			cmdArgs = append(cmdArgs, argInputs.str())

			for _, p := range ch.pathInps {
				cmdArgs = append(cmdArgs, (p).str())
			}

			cmdArgs = append(cmdArgs, argKeys.str())
			cmdArgs = appendInternStrs(cmdArgs, ch.keys)

			if len(ch.kvsCmd) > 0 {
				cmdArgs = append(cmdArgs, argKvs.str())
				cmdArgs = appendInternStrs(cmdArgs, ch.kvsCmd)
			}

			env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
			targetProps := TargetProperties{ModuleDir: instance.Path.Rel()}

			switch d.moduleStmt.Name {
			case tokPy23Library, tokPy23NativeLibrary:
				targetProps.ModuleTag = tagPy3
			}

			// pysrc/namespace emissions for both the PY3_PROGRAM PROGRAM-side and
			// its KindLib twin live under the PY3_BIN_LIB submodule in upstream;
			// stamp them with that submodule's lowercased tag so the dump matches REF.
			if d.moduleStmt.Name == tokPy3Program || d.programPairedLib {
				targetProps.ModuleTag = tagPy3BinLib
			}

			node := &Node{
				Platform:         instance.Platform,
				Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
				Env:              env,
				Inputs:           inputChunks{rescompilersChunk, ch.inps, objcopyScriptChunk},
				Outputs:          []VFS{outputObj},
				KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
				TargetProperties: targetProps,
				Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
			}

			if rescompilerLDRef != (NodeRef(0)) {
				node.DepRefs = append(node.DepRefs, rescompilerLDRef)
			}

			if rescompressorLDRef != (NodeRef(0)) {
				node.DepRefs = append(node.DepRefs, rescompressorLDRef)
			}

			exclude := []NodeRef{}

			if rescompilerLDRef != (NodeRef(0)) {
				exclude = append(exclude, rescompilerLDRef)
			}

			if rescompressorLDRef != (NodeRef(0)) {
				exclude = append(exclude, rescompressorLDRef)
			}

			if extras := resolveCodegenDepRefsExt(ctx, instance, nil, ch.inps, exclude...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}

			r := ctx.emit.Emit(node)
			res.Refs = append(res.Refs, r)
			res.Outputs = append(res.Outputs, outputObj)
		}
	}

	if len(res.Refs) == 0 {
		return nil
	}

	return res
}
