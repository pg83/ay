package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"strings"
)

// emitResourceObjcopy emits one objcopy PY node per flush of the
// upstream `TObjCopyResourcePacker`. Returned slice is the
// `$(B)/...objcopy_*.o` output paths in flush order, intended to be
// appended to the module's `.global.a` `srcs[]` by the caller.
//
// Walks `tools/rescompiler/bin` and `tools/rescompressor/bin` to
// recover their LD NodeRefs and threads them as deps; both walks are
// memoized in ctx.memo so the parallel call site in emitPySrcs does
// not double-emit.
type objcopyEmitResult struct {
	Refs               []NodeRef
	Outputs            []VFS
	GlobalMemberInputs []VFS
}

func emitResourceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
) *objcopyEmitResult {
	// Emit kv_only sibling shapes (PY_MAIN, py/namespace,
	// py/no_check_imports) alongside RESOURCE/RESOURCE_FILES. Each
	// sibling is independent and conditional on its own per-module data.
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || len(d.pySrcs) > 0 || len(d.yaConfJSON) > 0
	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	rescompilerLDRef := walkHostToolForRef(ctx, instance, "tools/rescompiler/bin")
	rescompressorLDRef := walkHostToolForRef(ctx, instance, "tools/rescompressor/bin")

	out := &objcopyEmitResult{}
	// Collect the SOURCE_ROOT-rooted member inputs each emitted objcopy
	// node would contribute to the enclosing module's .global.a archive.
	// Every node carries `objcopy.py`; path-based flushes also carry
	// their source paths. Caller dedups + folds into globalMemberInputs.
	var globalMemberInputs []VFS
	addGlobal := func(p VFS) { globalMemberInputs = append(globalMemberInputs, p) }

	// kv_only siblings — each fires only when its trigger is present.
	// PY_MAIN fires BEFORE py/namespace (upstream pybuild.py:395-398
	// invokes py_main first; namespace is emitted at pybuild.py:587-594).
	// Order affects the LD cmd[2] SRCS_GLOBAL emission sequence.
	if nodeRes := emitPyMainObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
		addGlobal(objcopyScriptVFS)
	}

	for _, nodeRes := range emitPyNamespaceObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef) {
		if nodeRes != nil {
			out.Refs = append(out.Refs, nodeRes.Ref)
			out.Outputs = append(out.Outputs, nodeRes.Out)
			addGlobal(objcopyScriptVFS)
		}
	}

	if nodeRes := emitNoCheckImportsObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef); nodeRes != nil {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
		addGlobal(objcopyScriptVFS)
	}

	for _, nodeRes := range emitYaConfJSONObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef) {
		out.Refs = append(out.Refs, nodeRes.Ref)
		out.Outputs = append(out.Outputs, nodeRes.Out)
		addGlobal(objcopyScriptVFS)
		addGlobal(nodeRes.Input)
	}

	if len(d.resources) == 0 && len(d.pyPyiResources) == 0 {
		// Per-PY_SRCS resfs entry objcopy nodes. One node per packer-flush
		// chunk; large modules (Lib, lib2/py) split via chunkPySrcEntries.
		srcRes := emitPySrcObjcopy(ctx, instance, d, rescompilerLDRef, rescompressorLDRef)
		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
			globalMemberInputs = append(globalMemberInputs, srcRes.GlobalMemberInputs...)
		}

		out.GlobalMemberInputs = globalMemberInputs
		return out
	}

	// Filter rejected entries (mirrors objcopy.h:84-96 CanHandle):
	// drop entries whose path or name contains the BAD substrings.
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
		pathDeps    []NodeRef
		keys        []string // base64-encoded (padded) keys for path entries
		kvs         []string
		cmdLen      int
	}
	cur := acc{}

	var moduleTag *string
	if d.moduleStmt != nil {
		moduleTag = resourceModuleTagForData(d)
	}

	flush := func() {
		if cur.cmdLen == 0 {
			return
		}
		hash := objcopyHash(cur.paths, cur.keys, cur.kvs, instance.Path, moduleTag)
		outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			objcopyScriptPath,
			"--compiler", instance.Platform.Tools.CXX,
			"--objcopy", instance.Platform.Tools.Objcopy,
			"--compressor", rescompressorBinPath,
			"--rescompiler", rescompilerBinPath,
			"--output_obj", outputObj.String(),
			"--target", instance.Platform.Triple,
		}

		if len(cur.paths) > 0 {
			cmdArgs = append(cmdArgs, "--inputs")
			for _, p := range cur.pathInputs {
				cmdArgs = append(cmdArgs, p.String())
			}
			cmdArgs = append(cmdArgs, "--keys")
			cmdArgs = append(cmdArgs, cur.keys...)
		}

		if len(cur.kvs) > 0 {
			cmdArgs = append(cmdArgs, "--kvs")
			for _, kv := range cur.kvs {
				cmdArgs = append(cmdArgs, expandRootrel(kv, instance.Path))
			}
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

		inputs := []VFS{
			Build("tools/rescompiler/rescompiler"),
			Build("tools/rescompressor/rescompressor"),
		}
		if len(cur.paths) <= 1 {
			inputs = append(inputs, objcopyScriptVFS)
			inputs = append(inputs, cur.pathInputs...)
			inputs = append(inputs, cur.extraInputs...)
		} else {
			inputs = append(inputs, cur.pathInputs...)
			inputs = append(inputs, objcopyScriptVFS)
			inputs = append(inputs, cur.extraInputs...)
		}

		objcopyTags := []string{}
		if len(instance.Platform.Tags) > 0 {
			objcopyTags = append(objcopyTags, instance.Platform.Tags...)
		}

		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  inputs,
			Outputs: []VFS{outputObj},
			KV: map[string]string{
				"p":        "PY",
				"pc":       "yellow",
				"show_out": "yes",
			},
			Tags: objcopyTags,
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		if rescompilerLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}

		if rescompressorLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		depSeen := map[NodeRef]struct{}{}
		for _, ref := range cur.pathDeps {
			if ref == (NodeRef{}) {
				continue
			}
			if _, dup := depSeen[ref]; dup {
				continue
			}
			depSeen[ref] = struct{}{}
			node.DepRefs = append(node.DepRefs, ref)
		}

		r := ctx.emit.Emit(node)
		out.Refs = append(out.Refs, r)
		out.Outputs = append(out.Outputs, outputObj)

		for _, p := range cur.pathInputs {
			addGlobal(p)
		}
		for _, p := range cur.extraInputs {
			addGlobal(p)
		}
		addGlobal(objcopyScriptVFS)

		cur = acc{}
	}

	emitEntries := func(entries []resourceEntry) {
		for _, e := range entries {
			if !contains(e.Path) && !contains(e.Key) {
				if e.Path == "-" {
					cur.kvs = append(cur.kvs, e.Key)
					cur.cmdLen += rootCmdLen + len(e.Key)
				} else {
					inputVFS := Source(instance.Path + "/" + e.Path)
					var producerRef NodeRef
					if d.prOutputProducer != nil {
						if ref, ok := d.prOutputProducer[e.Path]; ok {
							inputVFS = Build(instance.Path + "/" + e.Path)
							producerRef = ref
							cur.extraInputs = mergeDedupVFS(cur.extraInputs, prResourceExtraInputs(d, e.Path))
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

			if cur.cmdLen > maxCmdLen {
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
		globalMemberInputs = append(globalMemberInputs, srcRes.GlobalMemberInputs...)
	}
	emitEntries(d.pyPyiResources)

	out.GlobalMemberInputs = globalMemberInputs
	return out
}

// objcopyEmit is the emit-product of a single kv-only objcopy sub-emitter
// (PY_MAIN / namespace / no_check_imports). nil = trigger absent on this
// module -> nothing emitted; non-nil = (NodeRef, output path) pair.
type objcopyEmit struct {
	Ref   NodeRef
	Out   VFS
	Input VFS
}

func emitKvOnlyObjcopyNode(
	ctx *genCtx,
	instance ModuleInstance,
	kvsHash []string,
	kvsCmd []string,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) *objcopyEmit {
	moduleTag := resourceModuleTagForData(d)
	hash := objcopyHash(nil, nil, kvsHash, instance.Path, moduleTag)
	outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		objcopyScriptPath,
		"--compiler", instance.Platform.Tools.CXX,
		"--objcopy", instance.Platform.Tools.Objcopy,
		"--compressor", rescompressorBinPath,
		"--rescompiler", rescompilerBinPath,
		"--output_obj", outputObj.String(),
		"--target", instance.Platform.Triple,
		"--kvs",
	}
	cmdArgs = append(cmdArgs, kvsCmd...)

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
	inputs := []VFS{
		Build("tools/rescompiler/rescompiler"),
		Build("tools/rescompressor/rescompressor"),
		objcopyScriptVFS,
	}

	targetProps := map[string]string{
		"module_dir": instance.Path,
	}
	switch d.moduleStmt.Name {
	case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
		targetProps["module_tag"] = "py3"
	}
	if d.py3ProgramMultimodule && d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
		targetProps["module_tag"] = "py3_bin"
	}

	kvTags := []string{}
	if len(instance.Platform.Tags) > 0 {
		kvTags = append(kvTags, instance.Platform.Tags...)
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{outputObj},
		KV:               map[string]string{"p": "PY", "pc": "yellow", "show_out": "yes"},
		Tags:             kvTags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		HostPlatform:     instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if rescompilerLDRef != (NodeRef{}) {
		node.DepRefs = append(node.DepRefs, rescompilerLDRef)
	}
	if rescompressorLDRef != (NodeRef{}) {
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
		for _, formula := range yaConfFormulaResources(ctx.sourceRoot, file) {
			resources = append(resources, yaConfResource{
				sourcePath: formula,
				keyPath:    formula,
				hashPath:   formula,
			})
		}
		resources = append(resources, yaConfResource{
			sourcePath: file,
			keyPath:    "ya.conf.json",
			hashPath:   "ya.conf.json",
		})
	}

	out := make([]*objcopyEmit, 0, len(resources))
	var moduleTag *string
	if d.moduleStmt != nil {
		moduleTag = resourceModuleTagForData(d)
	}

	for _, res := range resources {
		key := "resfs/file/" + res.keyPath
		keyB64 := encb64.StdEncoding.EncodeToString([]byte(key))
		kvHash := "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + res.hashPath + "\"}"
		kvCmd := "resfs/src/" + key + "=" + res.sourcePath
		hash := objcopyHash([]string{res.hashPath}, []string{keyB64}, []string{kvHash}, instance.Path, moduleTag)
		outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")
		input := Source(res.sourcePath)

		cmdArgs := []string{
			instance.Platform.Tools.Python3,
			objcopyScriptPath,
			"--compiler", instance.Platform.Tools.CXX,
			"--objcopy", instance.Platform.Tools.Objcopy,
			"--compressor", rescompressorBinPath,
			"--rescompiler", rescompilerBinPath,
			"--output_obj", outputObj.String(),
			"--target", instance.Platform.Triple,
			"--inputs", input.String(),
			"--keys", keyB64,
			"--kvs", kvCmd,
		}

		env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  []VFS{rescompilerBinVFS, rescompressorBinVFS, input, objcopyScriptVFS},
			Outputs: []VFS{outputObj},
			KV: map[string]string{
				"p":        "PY",
				"pc":       "yellow",
				"show_out": "yes",
			},
			Tags: []string{},
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		if len(instance.Platform.Tags) > 0 {
			node.Tags = append([]string(nil), instance.Platform.Tags...)
		}
		if rescompilerLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompilerLDRef)
		}
		if rescompressorLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, rescompressorLDRef)
		}

		out = append(out, &objcopyEmit{Ref: ctx.emit.Emit(node), Out: outputObj, Input: input})
	}

	return out
}

// emitPyNamespaceObjcopy emits the `py/namespace/<mod_list_md5>/<unit>=<ns>.`
// kv objcopy node per pybuild.py:587-594. The mod_list_md5 is a
// streaming md5 over each `(path, mod)` pair's `mod` UTF-8 bytes,
// iteration-ordered. Skipped for `contrib/tools/python3*` modules
// (pybuild.py:40-48) and for modules whose PY_SRCS is TOP_LEVEL.
func emitPyNamespaceObjcopy(
	ctx *genCtx,
	instance ModuleInstance,
	d *moduleData,
	rescompilerLDRef NodeRef,
	rescompressorLDRef NodeRef,
) []*objcopyEmit {
	if len(d.pySrcs) == 0 || d.noExtendedPySearch {
		return nil
	}
	if strings.HasPrefix(instance.Path, "contrib/python") ||
		strings.HasPrefix(instance.Path, "contrib/tools/python3") {
		return nil
	}
	if d.moduleStmt == nil || resourceModuleTag(d.moduleStmt.Name) == nil {
		return nil
	}

	groups := d.pySrcGroups
	if len(groups) == 0 {
		groups = []pySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	var out []*objcopyEmit
	for _, group := range groups {
		pySources := make([]string, 0, len(group.Srcs))
		for _, srcRel := range group.Srcs {
			if strings.HasSuffix(srcRel, ".py") {
				pySources = append(pySources, srcRel)
			}
		}
		if len(pySources) == 0 {
			continue
		}

		nsPrefix := strings.ReplaceAll(instance.Path, "/", ".") + "."
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

		keyPath := instance.Path
		if group.TopLevel && d.srcDir != nil {
			keyPath = *d.srcDir
		}
		key := "py/namespace/" + modListMD5 + "/" + keyPath
		kvHash := key + "=\"" + nsValue + "\""
		kvCmd := key + "=" + nsValue
		out = append(out, emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d, rescompilerLDRef, rescompressorLDRef))
	}

	return out
}

// emitPyMainObjcopy emits the `PY_MAIN=<dotted>:<func>` kv objcopy node
// per pybuild.py:759.
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
	return emitKvOnlyObjcopyNode(ctx, instance, []string{kv}, []string{kv}, d, rescompilerLDRef, rescompressorLDRef)
}

// emitNoCheckImportsObjcopy emits the
// `py/no_check_imports/<pathid>=<args-space-joined>` kv objcopy node.
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

	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d, rescompilerLDRef, rescompressorLDRef)
}

// emitPySrcObjcopy emits one objcopy PY node per chunk of PY_SRCS-derived
// resfs entries. Skipped when no PY_SRCS or when all entries are
// suppressed by PYBUILD_NO_PY + PYBUILD_NO_PYC.
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
	if resourceModuleTagForData(d) == nil {
		return nil
	}

	groups := d.pySrcGroups
	if len(groups) == 0 {
		groups = []pySrcGroup{{Srcs: d.pySrcs, TopLevel: d.pyTopLevel, Namespace: d.pyNamespace}}
	}

	var chunkGroups [][]pySrcChunk
	chunkCount := 0
	for _, group := range groups {
		entries := buildPySrcEntriesFor(d, instance.Path, group.Srcs, group.TopLevel, group.Namespace)
		if len(entries) == 0 {
			continue
		}
		chunks := chunkPySrcEntries(entries)
		chunkGroups = append(chunkGroups, chunks)
		chunkCount += len(chunks)
	}
	if chunkCount == 0 {
		return nil
	}

	moduleTag := resourceModuleTagForData(d)
	res := &objcopyEmitResult{
		Refs:    make([]NodeRef, 0, chunkCount),
		Outputs: make([]VFS, 0, chunkCount),
	}
	var globalMemberInputs []VFS
	for _, chunks := range chunkGroups {
		for _, ch := range chunks {
			hash := objcopyHash(ch.paths, ch.keys, ch.kvsHash, instance.Path, moduleTag)
			outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")

			cmdArgs := []string{
				instance.Platform.Tools.Python3,
				objcopyScriptPath,
				"--compiler", instance.Platform.Tools.CXX,
				"--objcopy", instance.Platform.Tools.Objcopy,
				"--compressor", rescompressorBinPath,
				"--rescompiler", rescompilerBinPath,
				"--output_obj", outputObj.String(),
				"--target", instance.Platform.Triple,
			}

			cmdArgs = append(cmdArgs, "--inputs")
			cmdArgs = append(cmdArgs, ch.pathInps...)
			cmdArgs = append(cmdArgs, "--keys")
			cmdArgs = append(cmdArgs, ch.keys...)
			cmdArgs = append(cmdArgs, "--kvs")
			cmdArgs = append(cmdArgs, ch.kvsCmd...)

			inputs := []VFS{
				Build("tools/rescompiler/rescompiler"),
				Build("tools/rescompressor/rescompressor"),
			}
			for _, p := range ch.inps {
				inputs = append(inputs, p)
			}
			inputs = append(inputs, objcopyScriptVFS)

			env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}
			targetProps := map[string]string{"module_dir": instance.Path}
			switch d.moduleStmt.Name {
			case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
				targetProps["module_tag"] = "py3"
			}
			if d.py3ProgramMultimodule && d.moduleStmt.Name == "PY3_PROGRAM_BIN" {
				targetProps["module_tag"] = "py3_bin"
			}

			pyTags := []string{}
			if len(instance.Platform.Tags) > 0 {
				pyTags = append(pyTags, instance.Platform.Tags...)
			}

			node := &Node{
				Cmds:             []Cmd{{CmdArgs: cmdArgs, Env: env}},
				Env:              env,
				Inputs:           inputs,
				Outputs:          []VFS{outputObj},
				KV:               map[string]string{"p": "PY", "pc": "yellow", "show_out": "yes"},
				Tags:             pyTags,
				TargetProperties: targetProps,
				Platform:         string(instance.Platform.Target),
				HostPlatform:     instance.Platform.IsHost,
				Requirements: map[string]interface{}{
					"cpu":     float64(1),
					"network": "restricted",
					"ram":     float64(32),
				},
			}

			if rescompilerLDRef != (NodeRef{}) {
				node.DepRefs = append(node.DepRefs, rescompilerLDRef)
			}
			if rescompressorLDRef != (NodeRef{}) {
				node.DepRefs = append(node.DepRefs, rescompressorLDRef)
			}

			exclude := []NodeRef{}
			if rescompilerLDRef != (NodeRef{}) {
				exclude = append(exclude, rescompilerLDRef)
			}
			if rescompressorLDRef != (NodeRef{}) {
				exclude = append(exclude, rescompressorLDRef)
			}
			if extras := resolveCodegenDepRefsExt(ctx, instance, nil, ch.inps, exclude...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}

			r := ctx.emit.Emit(node)
			res.Refs = append(res.Refs, r)
			res.Outputs = append(res.Outputs, outputObj)

			for _, p := range ch.inps {
				if p.IsSource() {
					globalMemberInputs = append(globalMemberInputs, p)
				}
			}
			globalMemberInputs = append(globalMemberInputs, objcopyScriptVFS)
		}
	}

	res.GlobalMemberInputs = globalMemberInputs
	return res
}
