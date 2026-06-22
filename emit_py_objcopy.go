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

type ObjcopyEmitResult struct {
	Refs    []NodeRef
	Outputs []VFS
	// PySrcTrailCount is the number of trailing Refs that come from PY_SRCS
	// resource packing — the .py objcopies (emitPySrcObjcopy, including the
	// py/namespace kv objcopy) and the .pyi objcopies (d.pyPyiResources). Upstream
	// processes these onresource_files calls a step after the SRCS(GLOBAL) band, so
	// in a global archive they stay AFTER the global sources while the leading
	// explicit-resource objcopies move before them. See gen.go's global-archive
	// member placement.
	PySrcTrailCount int
}

// objcopyArgBlocks are the module-stable spans of a resource-objcopy command
// line — everything around the per-node output path. Built once per module
// (emitResourceObjcopy) and referenced as chunks by every objcopy node the
// module emits; the per-node remainder is the output path and the
// --inputs/--keys/--kvs payload.
type ObjcopyArgBlocks struct {
	// pre: [python3, objcopy.py, --compiler, <cxx>, --objcopy, <objcopy>,
	// --compressor, <path>, --rescompiler, <path>, --output-obj]
	pre []STR
	// post: [--target, <triple>]
	post []STR
}

// objcopyEmitCtx carries the per-module objcopy emission state: the resource
// tool refs and the stable arg blocks.
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

// objcopyCmdArgs assembles an objcopy command line: the module-stable blocks
// are referenced, only [out] and the payload tail are per-node.
func objcopyCmdArgs(oc *ObjcopyEmitCtx, outputObj VFS, payload []STR) ArgChunks {
	return oc.na.chunkList(oc.blocks.pre, oc.na.strList((outputObj).str()), oc.blocks.post, payload)
}

// resolvedResource is the outcome of resolving one embedded RESOURCE path. For a
// generated payload it carries the $(B) build artifact, the producer ref/main
// output, and the producer's recorded source-attribution sets; for an ordinary
// source it is just the fallback $(S) input with zero refs and no sources.
type resolvedResource struct {
	Input           VFS
	ProducerRef     NodeRef
	ProducerMainOut VFS
	// SourceInputs / SourceClosure are the producer's $(S) source-attribution
	// sets (GeneratedFileInfo). Upstream's flat-input model lists every consumed
	// input's transitive source closure on the objcopy node, so an embedded
	// generated payload carries the chain's source leaves. Empty for a source.
	SourceInputs  []VFS
	SourceClosure []VFS
}

// resolveResourceInput resolves one embedded resource path to its node input. A
// generated resource (RUN_PROGRAM OUT/OUT_NOAUTO/STDOUT, COPY output, ...) lives
// in the codegen registry keyed by its output VFS; resourceOutputVFS
// canonicalizes the raw RESOURCE path (${BINDIR}/X, $(B)/<unit>/X, a plain
// module-relative name, or an arcadia-root-relative path rooted at the module
// dir) to that key. When found, the input is the $(B) build artifact and the
// producer ref is returned so the objcopy node depends on it. Otherwise the path
// is an ordinary source file: the fallback VFS (its $(S) location) is used with
// no extra dep.
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
		// mainOuts collects, per resolved generated resource, the producer's main
		// output (0 for an ordinary source or a producer with no recorded main).
		// A multi-output producer's main output rides every consumer of any of its
		// outputs as a spurious input (the OutTogether main-output edge), even on
		// a chunk that embeds only the producer's additional outputs.
		mainOuts []VFS
		// srcAttrInputs collects, per resolved generated resource, the producer
		// chain's $(S) source-attribution leaves (kept by objcopySourceLeafKept).
		// Cache-key inputs only — never in --inputs/--keys/hash/command.
		srcAttrInputs []VFS
		keys          []string
		kvs           []string
		kvsCmd        []string
		cmdLen        int
	}
	cur := acc{}
	moduleTag := resourceLibTagForData(d)

	// A RESOURCE() in a PROTO_LIBRARY body belongs to the C++ _CPP_PROTO submodule
	// (MODULE_TAG=CPP_PROTO; same predicate as cfModuleTag). Upstream's resfs
	// objcopy packer folds that submodule tag into the output-name hash and stamps
	// the node's module_tag with the lowercased tag.
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

		// The embedded generated resources' build-root OUTPUT_INCLUDES closure and
		// the resfs/src kv inputs ride the objcopy node as cache-key inputs only —
		// never in the --inputs payload (which would change the objcopy_<hash>).
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

		// The embedded generated payloads' producer-chain $(S) source leaves ride
		// as cache-key inputs too (upstream's flat-input source attribution), deduped
		// against everything already present.
		for _, p := range cur.srcAttrInputs {
			if !deduper.add(p) {
				continue
			}

			tail = append(tail, p)
		}

		if len(tail) > 0 {
			inputs = append(inputs, tail)
		}

		// Spurious main-output inputs: a multi-output producer's main output rides
		// every consumer of any of its outputs (the OutTogether main-output edge),
		// so a chunk that embeds only the producer's additional outputs still lists
		// the main output. Deduped against the inputs already present (a chunk that
		// embeds the main output as a payload has it covered).
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

		resTargetProps := TargetProperties{ModuleDir: instance.Path.rel()}

		switch {
		case d.moduleStmt.Name == tokPy23Library || d.moduleStmt.Name == tokPy23NativeLibrary:
			resTargetProps.ModuleTag = tagPy3
		// RESOURCE/RESOURCE_FILES are .IGNORED on the PY3_BIN (PROGRAM) submodule
		// (conf/python.conf:350), so the resfs objcopy is owned by the PY3_BIN_LIB
		// (LIBRARY) twin for both the PROGRAM half (emitted then deduped against the
		// twin) and the paired LIBRARY half. Stamp py3_bin_lib, matching the output
		// hash tag (resourceLibTagForData).
		case d.moduleStmt.Name == tokPy3Program || d.programPairedLib:
			resTargetProps.ModuleTag = tagPy3BinLib
		case cppProtoSubmodule:
			resTargetProps.ModuleTag = tagCppProto
		}

		node := &Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: env}),
			Env:              env,
			Inputs:           inputs,
			Outputs:          na.vfsList(outputObj),
			KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
			TargetProperties: resTargetProps,
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:        instance.Platform.UsesPython3Clang,
		}

		node.DepRefs = append(node.DepRefs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

		// Every $(B) data input (the embedded generated payloads, their producer's
		// OUTPUT_INCLUDES closure headers, and generated resfs/src kv files) carries
		// a producer dependency: resolveCodegenDepRefs maps each registered build
		// output to its producer ref, deduping and excluding the tool refs already
		// added. A source-tree resource resolves to nothing, so the common case is a
		// no-op.
		dataInputs := make([]VFS, 0, len(cur.pathInputs)+len(cur.closureInputs)+len(cur.kvInputs))
		dataInputs = append(dataInputs, cur.pathInputs...)
		dataInputs = append(dataInputs, cur.closureInputs...)
		dataInputs = append(dataInputs, cur.kvInputs...)

		node.DepRefs = append(node.DepRefs, resolveCodegenDepRefs(ctx, instance, dataInputs, oc.rescompilerLDRef, oc.rescompressorLDRef)...)

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

					// The resfs/src kv names the same file as the payload entry via
					// ${rootrel;input=TEXT:"<inner>"}. Resolve it exactly like the
					// payload entry below — codegen registry first (a RUN_PROGRAM
					// STDOUT/OUT, FROM_SANDBOX, or BUNDLE output binds to $(B) with the
					// producer dep), then copyFileInputVFS, which binds a root-relative
					// ordinary source (existing at the arcadia root outside the module)
					// to its $(S) source path. The emitted resfs/src value is that
					// resolved input's rootrel, not a naive module-dir join.
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

					// A generated build-root resource (RUN_PROGRAM OUT/STDOUT, …) carries
					// its producer's OUTPUT_INCLUDES build-root closure onto the objcopy
					// node — upstream's induced deps on every output, walked through the
					// .pb's ${input:…}. Keep only the $(B) half (the $(S) over-emit is
					// pruned ref-side by dump normalize). A source resource resolves to
					// producerRef 0 and adds nothing.
					if r.ProducerRef != 0 {
						for _, v := range walkClosureTail(ctx.scannerFor(instance), r.Input, in.ScanCfg) {
							if v.isBuild() {
								cur.closureInputs = append(cur.closureInputs, v)
							}
						}

						// Upstream's flat-input model also lists the producer chain's
						// transitive $(S) source leaves on the objcopy node (the generated
						// payload's SourceInputs, folded forward across opaque generated
						// INs, plus its transitive source closure). Keep only the leaves
						// that survive the resource-objcopy over-emit prune — the same set
						// the reference normalizer keeps (objcopySourceLeafKept). Rides as
						// a cache-key input below, never in the --inputs payload.
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

	// Upstream's PY3_BIN (PROGRAM) submodule sets .IGNORED=RESOURCE RESOURCE_FILES
	// (conf/python.conf:350): the RESOURCE/RESOURCE_FILES objcopy is owned solely by
	// the PY3_BIN_LIB (LIBRARY) twin, which packs it into its <tag>_global archive;
	// the PROGRAM links it through .PEERDIRSELF=PY3_BIN_LIB, never as a direct LD
	// member. Mirror that ignore on the PROGRAM side — the same PROCESS_PY_MAIN_ONLY
	// split emitPySrcObjcopy already applies to PY_SRCS — so the PROGRAM-side
	// emission contributes only PROGRAM-owned objcopies (PY_MAIN/NO_CHECK_IMPORTS),
	// not the LIBRARY-owned resource members.
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

// kvOnlyKind selects the upstream submodule whose MODULE_TAG the kv-only
// objcopy emission inherits — PY_MAIN / NO_CHECK_IMPORTS belong to the
// PY3_BIN submodule (PROGRAM-side), py/namespace and RESOURCE_FILES belong to
// PY3_BIN_LIB (LIBRARY-side).
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

	targetProps := TargetProperties{ModuleDir: instance.Path.rel()}

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
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(rescompilersWithScriptChunk),
		Outputs:          na.vfsList(outputObj),
		KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:        instance.Platform.UsesPython3Clang,
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
			Env:              env,
			Inputs:           na.inputList(rescompilersChunk, na.vfsList(input, objcopyScriptVFS)),
			Outputs:          na.vfsList(outputObj),
			KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:        instance.Platform.UsesPython3Clang,
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
	// Upstream folds EVERY PY_SRCS mod name into mod_list_md5 (pybuild.py), but
	// gates the py/namespace resource itself on is_extended_source_search →
	// is_arc_src(path) (pybuild.py:388): a $(B) build-generated PY_SRCS source is
	// not an arc source, so it never contributes a namespace entry. So the md5 runs
	// over all .py sources, while a module whose only PY_SRCS is generated emits no
	// namespace node at all.
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

	// Upstream keys each namespace at mod_root_path = rootrel_arc_src(token) minus
	// its trailing `/<token>` (pybuild.py:390-392), NOT the module dir. For a
	// module-local token that is the module dir; for a SRCDIR-redirected token the
	// SRCDIR; for an arcadia-root-relative checked-in token (the file lives at the
	// root path it names) the prefix above the token — empty when the token IS the
	// rootrel. resolvePySrcRel is our rootrel_arc_src equivalent. py_namespaces is a
	// map, so one kv per distinct namespace root, emitted sorted.
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
			targetProps := TargetProperties{ModuleDir: instance.Path.rel()}

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
				Cmds:             na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
				Env:              env,
				Inputs:           na.inputList(rescompilersChunk, ch.inps, objcopyScriptChunk),
				Outputs:          na.vfsList(outputObj),
				KV:               KV{P: pkPY, PC: pcYellow, ShowOut: true},
				TargetProperties: targetProps,
				Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				Resources:        instance.Platform.UsesPython3Clang,
			}

			node.DepRefs = append(node.DepRefs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

			exclude := depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)

			if extras := resolveCodegenDepRefs(ctx, instance, ch.inps, exclude...); len(extras) > 0 {
				node.DepRefs = append(node.DepRefs, extras...)
			}

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
