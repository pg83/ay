package main

import (
	"crypto/md5"
	enc32 "encoding/base32"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

// objcopyEmitResult collects the emitted objcopy node refs, output
// `$(B)/...objcopy_*.o` paths, and the SOURCE_ROOT-rooted inputs the
// caller must fold into the module's `.global.a` member set.
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

	rescompilerLDRef, _ := ctx.tool("tools/rescompiler/bin")
	rescompressorLDRef, _ := ctx.tool("tools/rescompressor/bin")

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

	// py/namespace objcopy nodes are emitted by emitPySrcObjcopy, interleaved
	// before each PY_SRCS group's py-source chunks (matches ya.make macro
	// evaluation order: pybuild.py:587-594 emits the namespace resource
	// immediately ahead of that group's resfs entries, and RESOURCE-class
	// macros precede the PY_SRCS macros).

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
		kvInputs    []VFS // input=TEXT files from kv-only entries (graph inputs only, not --inputs)
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

		// Fold in kv input=TEXT files not already present (path entries for
		// non-straddling files dedup away; only the chunk-straddle file —
		// kv in this chunk, path+key in the next — is genuinely new).
		inputSeen := make(map[VFS]struct{}, len(inputs))
		for _, p := range inputs {
			inputSeen[p] = struct{}{}
		}
		for _, p := range cur.kvInputs {
			if _, dup := inputSeen[p]; dup {
				continue
			}
			inputSeen[p] = struct{}{}
			inputs = append(inputs, p)
		}

		objcopyTags := []string{}
		if len(instance.Platform.Tags) > 0 {
			objcopyTags = append(objcopyTags, instance.Platform.Tags...)
		}

		// RESOURCE/RESOURCE_FILES objcopy nodes carry the same module_tag
		// as the kv-only/PY_SRCS objcopy nodes: PY23 library variants → py3,
		// PY3_PROGRAM → py3_bin. PY3_LIBRARY and non-PY modules emit none.
		resTargetProps := map[string]string{"module_dir": instance.Path}
		if d.moduleStmt != nil {
			switch d.moduleStmt.Name {
			case "PY23_LIBRARY", "PY23_NATIVE_LIBRARY":
				resTargetProps["module_tag"] = "py3"
			case "PY3_PROGRAM":
				resTargetProps["module_tag"] = "py3_bin"
			}
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
			Tags:             objcopyTags,
			TargetProperties: resTargetProps,
			Platform:         string(instance.Platform.Target),
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
					// The `resfs/src/...=${rootrel;...;input=TEXT:"P"}` kv
					// (RESOURCE_FILES) registers P as a graph input via the
					// input=TEXT modifier. Upstream lists P in inputs[] even
					// when this kv lands in a different chunk than P's path+key
					// add (chunk straddle). flush() folds these into inputs[]
					// (deduped), but never into --inputs/__PATHS.
					if inner, ok := rootrelInputPath(e.Key); ok {
						cur.kvInputs = append(cur.kvInputs, Source(instance.Path+"/"+inner))
					}
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
	if d.moduleStmt.Name == "PY3_PROGRAM" {
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

	// Member order: the YA_CONF_JSON file resource first, then its formula
	// resources sorted lexicographically by full formula path. Matches the
	// REF .global.a cmd_args order; emission order is what normalize.py
	// preserves (inputs/deps are sorted, cmds are not).
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
			Platform: string(instance.Platform.Target),
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

// emitPyNamespaceForGroup emits the `py/namespace/<mod_list_md5>/<unit>=<ns>.`
// kv objcopy node for a single PY_SRCS group (pybuild.py:587-594). The
// mod_list_md5 is a streaming md5 over each source's dotted `mod` UTF-8
// bytes in declaration order. Returns nil when the group carries no `.py`
// sources. The module-level namespace guards (noExtendedPySearch,
// contrib/python*, contrib/tools/python3*, resourceModuleTag) are checked
// by the caller (emitPySrcObjcopy) before invoking this.
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
	return emitKvOnlyObjcopyNode(ctx, instance, []string{kvHash}, []string{kvCmd}, d, rescompilerLDRef, rescompressorLDRef)
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

	// Per-group namespace objcopy nodes interleave ahead of that group's
	// py-source chunks. The namespace guard set differs from the chunk
	// guard: PY3_PROGRAM emits chunks but no namespace (resourceModuleTag
	// returns nil for it), and noExtendedPySearch / contrib/python* /
	// contrib/tools/python3* suppress only the namespace.
	namespaceEnabled := !d.noExtendedPySearch &&
		!strings.HasPrefix(instance.Path, "contrib/python") &&
		!strings.HasPrefix(instance.Path, "contrib/tools/python3") &&
		resourceModuleTag(d.moduleStmt.Name) != nil

	moduleTag := resourceModuleTagForData(d)
	res := &objcopyEmitResult{}
	var globalMemberInputs []VFS
	for _, group := range groups {
		if namespaceEnabled {
			if nsRes := emitPyNamespaceForGroup(ctx, instance, d, group, rescompilerLDRef, rescompressorLDRef); nsRes != nil {
				res.Refs = append(res.Refs, nsRes.Ref)
				res.Outputs = append(res.Outputs, nsRes.Out)
				globalMemberInputs = append(globalMemberInputs, objcopyScriptVFS)
			}
		}

		entries := buildPySrcEntriesFor(d, instance.Path, group.Srcs, group.TopLevel, group.Namespace)
		if len(entries) == 0 {
			continue
		}
		for _, ch := range chunkPySrcEntries(entries) {
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
			if len(ch.kvsCmd) > 0 {
				cmdArgs = append(cmdArgs, "--kvs")
				cmdArgs = append(cmdArgs, ch.kvsCmd...)
			}

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
			if d.moduleStmt.Name == "PY3_PROGRAM" {
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

	if len(res.Refs) == 0 {
		return nil
	}

	res.GlobalMemberInputs = globalMemberInputs
	return res
}
