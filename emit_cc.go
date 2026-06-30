package main

import (
	"path/filepath"
	"strings"
)

var (
	argDashBBin = internV("-B", binPath)
	ccKV        = KV{P: pkCC, PC: pcGreen}
)

// ModuleCompileEnv is the durable, immutable per-module (or per-sub-module)
// compile environment. It is built once from ModuleData (+peer contributions)
// and threaded to emitters and producers. It never carries per-source or
// transient state — those live on ModuleCCInputs, which only emitCC assembles.
type ModuleCompileEnv struct {
	Flags                FlagSet
	CudaNvccFlags        []STR
	AddIncl              []VFS
	InclArgs             InclArgMemo
	CCBlocks             *CcModuleArgBlocks
	PeerAddInclGlobal    []VFS
	ProtoInclude         []VFS
	ProtoIncludePeers    []VFS
	CXXFlags             []ARG
	COnlyFlags           []ARG
	ClangWarnings        []ARG
	ScanCfg              ScanContext
	SrcDirs              []VFS
	FS                   FS
	PeerCFlagsGlobal     []ARG
	PeerCXXFlagsGlobal   []ARG
	PeerCOnlyFlagsGlobal []ARG
	CFlags               []ARG
	ModuleScopeCFlags    []ARG
	OwnCFlagsGlobal      []ARG
	OwnCXXFlagsGlobal    []ARG
	OwnCOnlyFlagsGlobal  []ARG
	SFlags               []ARG
	DefaultVars          map[STR]STR
	DefaultVarOrder      []STR
	SetVars              map[STR]STR
	Py3Suffix            bool
	ObjectSuffixStem     *string
	NoOptimize           bool
	ModuleTag            STR
	Ragel6Flags          []ARG
	BisonFlags           []ARG
	BisonGenExt          string
	TC                   ModuleToolchain
}

// ModuleCCInputs is the fully-resolved compile request for a single object.
// It is assembled only inside the cc subsystem (emitCC / emitLibraryCSource)
// from a ModuleCompileEnv plus the per-source override (from ModuleData or a
// GeneratedFileInfo.Compile spec). No other code constructs it.
type ModuleCCInputs struct {
	ModuleCompileEnv

	PerSourceCFlags []ARG
	FlatOutput      bool
	ForceCxx        bool
	Variant         *string
	ExtraDepRefs    []NodeRef
	IncludeInputs   []VFS
}

// ccInputsFor resolves the per-object compile inputs from the module env plus
// per-source enrichment: from the registered CompileSpec for generated
// sources, otherwise from ModuleData's per-source knowledge.
func (env ModuleCompileEnv) ccInputsFor(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS VFS) ModuleCCInputs {
	in := ModuleCCInputs{ModuleCompileEnv: env}

	if info := ctx.codegenFor(instance).lookup(srcVFS); info != nil && info.Compile != nil {
		sp := info.Compile

		if sp.Env != nil {
			in.ModuleCompileEnv = *sp.Env
		}

		in.PerSourceCFlags = sp.CFlags
		in.FlatOutput = sp.FlatOutput
		in.Variant = sp.Variant
		in.ObjectSuffixStem = sp.ObjectSuffixStem
		in.Py3Suffix = sp.Py3Suffix
		in.ForceCxx = sp.ForceCxx

		if len(sp.AddInclExtra) > 0 {
			in.AddIncl = concat(in.AddIncl, sp.AddInclExtra)
			in.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &in.ModuleCompileEnv)
		}

		return in
	}

	srcID := internStr(strings.TrimPrefix(srcVFS.rel(), instance.Path.rel()+"/"))

	if extras := d.perSrcCFlagsFor(srcID); extras != nil {
		in.PerSourceCFlags = *extras
	}

	if d.flatSrc(srcID) {
		in.FlatOutput = true
	}

	return in
}

// emitCC is the sole constructor of ModuleCCInputs. It takes the module's
// compile environment from ModuleData (d.cc), enriches it with per-source
// module knowledge, and for generated sources pulls the enrichment from the
// source's registered CompileSpec. No other code assembles cc flags.
func emitCC(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS VFS) (NodeRef, VFS) {
	in := d.cc.ccInputsFor(ctx, instance, d, srcVFS)

	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)
	in.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, in.IncludeInputs)

	if len(d.cythonCpp) > 0 {
		in.IncludeInputs = cythonCompileInducedInputs(ctx, instance, in.IncludeInputs)
	}

	ref, outPath, _ := composeCCNode(instance, srcVFS.str(), srcVFS, in, ctx.host, ctx.emit)

	return ref, outPath
}

func composeCCNode(instance ModuleInstance, src STR, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS, InputChunks) {
	na := emit.nodeArenas()
	srcRel := src.string()

	if v := src.vfs(); v != 0 {
		srcVFS = v
		srcRel = strings.TrimPrefix(v.rel(), instance.Path.rel()+"/")
	}

	suffix := ".o"

	if instance.Platform.PIC {
		suffix = ".pic.o"
	}

	if in.ObjectSuffixStem != nil {
		if instance.Platform.PIC {
			suffix = "." + *in.ObjectSuffixStem + ".pic.o"
		} else {
			suffix = "." + *in.ObjectSuffixStem + ".o"
		}
	} else if in.Py3Suffix {
		if instance.Platform.PIC {
			suffix = ".py3.pic.o"
		} else {
			suffix = ".py3.o"
		}
	}

	if in.Variant != nil {
		suffix = "." + *in.Variant + suffix
	}

	outVFS, inVFS := composeCCPaths(instance, srcRel, srcVFS, in, suffix)
	isCxx := in.ForceCxx || isCxxSource(srcRel)
	blocks := in.CCBlocks
	tok := na.strList((inVFS).str(), argDashC.str(), argDashO.str(), (outVFS).str())
	inChunk := tok[0:1]
	wrapcc := len(instance.Platform.WrapccHead) > 0
	compiler, tail := blocks.cHead, blocks.cxxTail

	if isCxx {
		compiler = blocks.cxxHead
	} else {
		tail = blocks.cTail
	}

	total := 4 + len(blocks.includes) + len(blocks.flags) + len(tail)

	if wrapcc {
		total += 3
	}

	if len(in.PerSourceCFlags) > 0 {
		total++
	}

	if !isCxx {
		total += len(blocks.cPost)
	}

	chunks := na.chunks.alloc(total)
	k := 0

	if wrapcc {
		chunks[k] = instance.Platform.WrapccHead
		chunks[k+1] = inChunk
		chunks[k+2] = instance.Platform.WrapccTail
		k += 3
	}

	chunks[k] = compiler
	chunks[k+1] = instance.Platform.CCHead
	chunks[k+2] = tok[1:4]
	k += 3
	k += copy(chunks[k:], blocks.includes)
	k += copy(chunks[k:], blocks.flags)
	k += copy(chunks[k:], tail)

	if len(in.PerSourceCFlags) > 0 {
		chunks[k] = na.argStrList(in.PerSourceCFlags)
		k++
	}

	if !isCxx {
		k += copy(chunks[k:], blocks.cPost)
	}

	chunks[k] = inChunk
	k++
	na.chunks.commit(k)

	cmdArgs := ArgChunks(chunks[:k])
	env := hostP.toolEnv()
	wrap := len(instance.Platform.WrapccHead) > 0

	var allInputs InputChunks

	if wrap {
		allInputs = na.inputList(in.IncludeInputs, wrapccPyChunk)
	} else {
		allInputs = na.inputList(in.IncludeInputs)
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: cmdArgs,
			Env:     env,
		}),
		Env:          env,
		Inputs:       allInputs,
		Outputs:      na.vfsList(outVFS),
		KV:           &ccKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.CCUsesResources,
	}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emit(node), outVFS, allInputs
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	canonMatches := func() bool {
		if srcRel != "" && pathIsClean(srcRel) {
			rel, dir := srcVFS.rel(), instance.Path.rel()

			return len(rel) == len(dir)+1+len(srcRel) &&
				rel[:len(dir)] == dir && rel[len(dir)] == '/' && rel[len(dir)+1:] == srcRel
		}

		return srcVFS.rel() == filepath.ToSlash(filepath.Clean(instance.Path.rel()+"/"+srcRel))
	}

	if srcVFS.isSource() && !canonMatches() {
		outputRel := composeSrcDirOutputRel(instance.Path.rel(), srcVFS.rel())

		out = build(instance.Path.rel(), "/", outputRel, suffix)

		return out, input
	}

	if srcVFS.isBuild() && !in.FlatOutput {
		rel, dir := srcVFS.rel(), instance.Path.rel()

		if rel != dir && !strings.HasPrefix(rel, dir+"/") {
			outputRel := composeSrcDirOutputRel(dir, rel)

			return build(dir, "/", outputRel, suffix), input
		}
	}

	srcRel = cleanRel(srcRel)

	switch {
	case in.FlatOutput:
		return build(instance.Path.rel(), "/", srcRel, suffix), input
	case strings.Contains(srcRel, "/"):
		body, underscore := normalizeDotDotSegments(srcRel)

		if underscore {
			return build(instance.Path.rel(), "/_/", body, suffix), input
		}

		return build(instance.Path.rel(), "/", body, suffix), input
	default:
		return build(instance.Path.rel(), "/", srcRel, suffix), input
	}
}

func composeSrcDirOutputRel(instancePath, target string) string {
	rel, err := filepath.Rel(instancePath, target)

	if err != nil {
		return "_/" + filepath.Base(target)
	}

	parts := strings.Split(rel, string(filepath.Separator))
	hasParent := false

	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
			hasParent = true
		}
	}

	joined := strings.Join(parts, "/")

	if !hasParent {
		if strings.Contains(joined, "/") {
			return "_/" + joined
		}

		return joined
	}

	return joined
}

func normalizeDotDotSegments(rel string) (body string, underscore bool) {
	if !strings.Contains(rel, "..") {
		return rel, !strings.HasPrefix(rel, "__")
	}

	parts := strings.Split(rel, "/")
	hasParent := false

	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
			hasParent = true
		}
	}

	return strings.Join(parts, "/"), !hasParent
}

func pickWarningFlags(noCompilerWarnings bool, noWShadow bool) []ARG {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	if noWShadow {
		return concat(warningFlags, []ARG{argNoShadow})
	}

	return warningFlags
}

func appendCxxStdAndOwn(cmdArgs []STR, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []ARG) []STR {
	if isCxx {
		cmdArgs = append(cmdArgs, (cxxStandardFlag).str())

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				cmdArgs = appendArgStr(cmdArgs, noWarningsBundle)
			} else {
				cmdArgs = appendArgStr(cmdArgs, cxxStandardWarnings)
			}
		}
	}

	cmdArgs = appendArgStr(cmdArgs, ownExtras)

	return cmdArgs
}

func composePeerExtras(in ModuleCompileEnv, isCxx bool) []ARG {
	if isCxx {
		return in.PeerCXXFlagsGlobal
	}

	return in.PeerCOnlyFlagsGlobal
}

func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCompileEnv, p *Platform) []ARG {
	out := make([]ARG, 0, len(p.CFlags)+len(in.CFlags)+len(in.PeerCFlagsGlobal)+len(in.OwnCFlagsGlobal))

	out = append(out, p.CFlags...)
	out = append(out, in.CFlags...)
	out = append(out, in.PeerCFlagsGlobal...)
	out = append(out, in.OwnCFlagsGlobal...)

	return out
}

func composeOwnAndPeerGlobalBucket(in ModuleCompileEnv, isCxx bool) []ARG {
	out := make([]ARG, 0,
		len(in.OwnCXXFlagsGlobal)+len(in.PeerCXXFlagsGlobal)+
			len(in.OwnCOnlyFlagsGlobal)+len(in.PeerCOnlyFlagsGlobal))

	if isCxx {
		out = append(out, in.OwnCXXFlagsGlobal...)
		out = append(out, in.PeerCXXFlagsGlobal...)
	} else {
		out = append(out, in.OwnCOnlyFlagsGlobal...)
		out = append(out, in.PeerCOnlyFlagsGlobal...)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func composePostCatboostBucket(preBucket []ARG) []ARG {
	for _, x := range preBucket {
		if x == baseUnitCxxNostdinc {
			return preBucket
		}
	}

	out := make([]ARG, 0, len(preBucket)+1)

	out = append(out, preBucket...)
	out = append(out, baseUnitCxxNostdinc)

	return out
}

func appendCompileFlagPipeline(cmdArgs []STR, bundle CompileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags, catboost []ARG) []STR {
	return appendArgStr(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir, bundle.CFlags, warningBundle, defineBundle, preNoLibcExtras, bundle.NoLibcBlock, catboost, moduleScopeCFlags, bundle.NoLibcBlock)
}

type CcModuleArgBlocks struct {
	cHead    []STR
	cxxHead  []STR
	includes ArgChunks
	flags    ArgChunks
	cTail    ArgChunks
	cxxTail  ArgChunks
	cPost    ArgChunks
}

func cWarningChunk(na *NodeArenas, noCompilerWarnings, noWShadow bool) []STR {
	switch {
	case noCompilerWarnings:
		return noWarningsBundleStr
	case noWShadow:
		return na.argStrList(warningFlags, []ARG{argNoShadow})
	default:
		return warningFlagsStr
	}
}

func cxxWarningChunk(noCompilerWarnings bool) []STR {
	if noCompilerWarnings {
		return noWarningsBundleStr
	}

	return cxxStandardWarningsStr
}

func catboostOpenSourceChunk(p *Platform) []STR {
	if p.Flags[envOPENSOURCE] == strYes {
		return catboostOpenSourceDefineStr
	}

	return nil
}

func suppressOptimize(cf []ARG) []ARG {
	for i, a := range cf {
		if a == argO3 {
			out := make([]ARG, len(cf))

			copy(out, cf)
			out[i] = argO0

			return out
		}
	}

	return cf
}

func composeCCModuleArgBlocks(na *NodeArenas, p *Platform, in *ModuleCompileEnv) *CcModuleArgBlocks {
	cflagsStr := p.CompileCFlagsStr

	if in.NoOptimize {
		cflagsStr = na.argStrList(suppressOptimize(p.CompileCFlags))
	}

	catboostStr := catboostOpenSourceChunk(p)
	noLibc := p.NoLibcBlockStr
	inc := na.chunks.alloc(4)
	ni := 0

	inc[ni] = ccIncludesPrefixStr
	ni++
	inc[ni] = na.inclArgList(in.AddIncl, in.InclArgs)
	ni++

	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		inc[ni] = na.strList(in.InclArgs.arg(peerAddIncl[0]))
		ni++
		peerAddIncl = peerAddIncl[1:]
	}

	inc[ni] = na.inclArgList(peerAddIncl, in.InclArgs)
	ni++
	na.chunks.commit(ni)

	flags := na.chunkList(
		na.argStrList(in.ClangWarnings),
		debugPrefixMapFlagsStr,
		xclangDebugCompilationDirStr,
		cflagsStr,
		cWarningChunk(na, in.Flags.NoCompilerWarnings, in.Flags.NoWShadow),
		p.DefinesStr,
		na.argStrList(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal),
		noLibc,
		catboostStr,
		na.argStrList(in.ModuleScopeCFlags),
		noLibc,
	)

	cTail := na.chunkList(
		na.argStrList(in.PeerCOnlyFlagsGlobal),
		builtinMacroDateTimeStr,
		macroPrefixMapFlagsStr,
	)

	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concat(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)

	cxxTail := na.chunkList(
		cxxStandardFlagStr,
		cxxWarningChunk(in.Flags.NoCompilerWarnings),
		na.argStrList(cxxOwnExtras),
		na.argStrList(cxxBucket),
		catboostStr,
		na.argStrList(composePostCatboostBucket(cxxBucket)),
		builtinMacroDateTimeStr,
		macroPrefixMapFlagsStr,
	)

	var cPost ArgChunks

	if len(in.COnlyFlags) > 0 {
		cPost = na.chunkList(na.argStrList(in.COnlyFlags))
	}

	return &CcModuleArgBlocks{
		cHead:    na.strList(in.TC.CC),
		cxxHead:  na.strList(in.TC.CXX),
		includes: ArgChunks(inc[:ni]),
		flags:    flags,
		cTail:    cTail,
		cxxTail:  cxxTail,
		cPost:    cPost,
	}
}

func appendAddIncl(cmdArgs []STR, addIncl []VFS, memo InclArgMemo) []STR {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(p))
	}

	return cmdArgs
}

type InclArgMemo struct {
	m *DenseMap[VFS, STR]
}

func (m InclArgMemo) arg(path VFS) STR {
	if a, ok := m.m.get(path); ok {
		return a
	}

	a := internV("-I", path.string())

	m.m.put(path, a)

	return a
}

func emitLibraryCSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR) *SourceEmit {
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = resolveModuleSourceVFS(ctx, instance, d, src, d.cc.SrcDirs)
	}

	ref, outPath := emitCC(ctx, instance, d, srcVFS)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
