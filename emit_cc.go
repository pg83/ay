package main

import (
	"path/filepath"
	"strings"
)

var (
	argDashBBin = internV("-B", binPath)
	ccKV        = KV{P: pkCC, PC: pcGreen}
)

type ModuleCompileEnv struct {
	Flags                FlagSet
	CudaNvccFlags        []ANY
	AddIncl              []VFS
	InclArgs             InclArgMemo
	CCBlocks             *CcModuleArgBlocks
	PeerAddInclGlobal    []VFS
	ProtoInclude         []VFS
	ProtoIncludePeers    []VFS
	PbHCompanionExt      string
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
	MainOutInducedInputs bool
	ModuleTag            STR
	Ragel6Flags          []ARG
	BisonFlags           []ARG
	BisonGenExt          string
	TC                   ModuleToolchain
	ForceConsistentDebug bool
}

type ModuleCCInputs struct {
	ModuleCompileEnv

	PerSourceCFlags []ARG
	FlatOutput      bool
	ForceCxx        bool
	Variant         *string
	ExtraDepRefs    []NodeRef
	IncludeInputs   []VFS
	IncludeView     Closure
}

func (e *EmitContext) ccInputsFor(srcVFS VFS) ModuleCCInputs {
	ctx, instance, d := e.ctx, e.instance, e.d
	env := d.cc
	in := ModuleCCInputs{ModuleCompileEnv: env}

	if info := e.codegen.lookup(srcVFS); info != nil && info.Compile != nil {
		sp := info.Compile

		in.PerSourceCFlags = sp.CFlags
		in.FlatOutput = sp.FlatOutput
		in.Variant = sp.Variant
		in.ObjectSuffixStem = sp.ObjectSuffixStem
		in.Py3Suffix = sp.Py3Suffix
		in.ForceCxx = sp.ForceCxx

		envDelta := false

		if sp.EnvAddIncl != nil {
			in.AddIncl = sp.EnvAddIncl
			envDelta = true
		}

		if sp.EnvCFlags != nil {
			in.CFlags = sp.EnvCFlags
			envDelta = true
		}

		if envDelta {
			in.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &in.ModuleCompileEnv)
		}

		return in
	}

	srcID := internStr(trimModulePrefix(srcVFS.relString(), instance.Path.relString()))

	if extras := d.perSrcCFlagsFor(srcID.any()); extras != nil {
		in.PerSourceCFlags = *extras
	}

	if d.flatSrc(srcID.any()) {
		in.FlatOutput = true
	}

	return in
}

func (e *EmitContext) emitCC(srcVFS VFS) (NodeRef, VFS) {
	return e.emitCCWith(srcVFS, e.ccInputsFor(srcVFS))
}

func (e *EmitContext) moduleSourceVFS(src ANY) VFS {
	_, _, d := e.ctx, e.instance, e.d

	return e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
}

func (e *EmitContext) emitCCFlat(srcVFS VFS, variant *string, cflags []ARG) (NodeRef, VFS) {
	in := e.ccInputsFor(srcVFS)

	in.FlatOutput = true
	in.Variant = variant
	in.PerSourceCFlags = cflags

	return e.emitCCWith(srcVFS, in)
}

func (e *EmitContext) emitCCWith(srcVFS VFS, in ModuleCCInputs) (NodeRef, VFS) {
	ctx, instance := e.ctx, e.instance

	in.IncludeView = walkClosure(e.scanner, srcVFS, in.ScanCfg)
	in.ExtraDepRefs = resolveCodegenDepRefsInclView(ctx, instance, ctx.na, in.IncludeView)

	if in.MainOutInducedInputs {
		in.IncludeInputs = e.mainOutInducedInputs(in.IncludeView)
		in.IncludeView = Closure{}
	}

	ref, outPath, _ := composeCCNode(instance, srcVFS.fullSTR(), srcVFS, in, ctx.host, ctx.emit)

	return ref, outPath
}

func (e *EmitContext) mainOutInducedInputs(includeView Closure) []VFS {
	reg := e.codegen

	var out, extra []VFS

	includeView.each(func(v VFS) {
		out = append(out, v)

		if !v.isBuild() {
			return
		}

		if info := reg.lookup(v); info != nil && info.ProducerMainOut != 0 {
			extra = append(extra, info.ProducerMainOut)
		}
	})

	return append(out, extra...)
}

func composeCCNode(instance ModuleInstance, src STR, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS, InputChunks) {
	na := emit.nodeArenas()
	srcRel := src.string()

	if v := src.vfs(); v != 0 {
		srcVFS = v
		srcRel = trimModulePrefix(v.relString(), instance.Path.relString())
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
	tok := na.anyList(inVFS.any(), argDashC.any(), argDashO.any(), outVFS.any())
	inChunk := tok[0:1]
	wrapcc := len(instance.Platform.WrapccHead) > 0
	compiler, tail := blocks.cHead, blocks.cxxTail

	if isCxx {
		compiler = blocks.cxxHead
	} else {
		tail = blocks.cTail
	}

	total := 4 + 4

	if wrapcc {
		total += 3
	}

	if len(tail) > 0 {
		total++
	}

	if len(in.PerSourceCFlags) > 0 {
		total++
	}

	if !isCxx && len(blocks.cPost) > 0 {
		total++
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
	chunks[k+3] = blocks.includes
	chunks[k+4] = blocks.flags
	k += 5

	if len(tail) > 0 {
		chunks[k] = tail
		k++
	}

	chunks[k] = builtinMacroDateTimeStr
	chunks[k+1] = macroPrefixMapFlagsStr
	k += 2

	if len(in.PerSourceCFlags) > 0 {
		chunks[k] = na.argAnyList(in.PerSourceCFlags)
		k++
	}

	if !isCxx && len(blocks.cPost) > 0 {
		chunks[k] = blocks.cPost
		k++
	}

	chunks[k] = inChunk
	k++
	na.chunks.commit(k)

	cmdArgs := ArgChunks(chunks[:k])
	env := hostP.toolEnv()
	wrap := len(instance.Platform.WrapccHead) > 0

	var allInputs InputChunks

	switch {
	case in.IncludeView.self != 0 && wrap:
		allInputs = na.inputList(na.vfsList(in.IncludeView.self, wrapccPyVFS), in.IncludeView.buckets...)
	case in.IncludeView.self != 0:
		allInputs = na.inputList(na.vfsList(in.IncludeView.self), in.IncludeView.buckets...)
	case wrap:
		allInputs = na.inputList(in.IncludeInputs, wrapccPyChunk)
	default:
		allInputs = na.inputList(in.IncludeInputs)
	}

	node := Node{
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

	return emit.emitNode(node), outVFS, allInputs
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	canonMatches := func() bool {
		if srcRel != "" && pathIsClean(srcRel) {
			rel, dir := srcVFS.relString(), instance.Path.relString()

			return len(rel) == len(dir)+1+len(srcRel) &&
				rel[:len(dir)] == dir && rel[len(dir)] == '/' && rel[len(dir)+1:] == srcRel
		}

		return srcVFS.relString() == filepath.ToSlash(filepath.Clean(instance.Path.relString()+"/"+srcRel))
	}

	if srcVFS.isSource() && !canonMatches() {
		outputRel := composeSrcDirOutputRel(instance.Path.relString(), srcVFS.relString())

		out = build(instance.Path.relString(), "/", outputRel, suffix)

		return out, input
	}

	if srcVFS.isBuild() && !in.FlatOutput {
		rel, dir := srcVFS.relString(), instance.Path.relString()

		if rel != dir && !strings.HasPrefix(rel, dir+"/") {
			outputRel := composeSrcDirOutputRel(dir, rel)

			return build(dir, "/", outputRel, suffix), input
		}
	}

	srcRel = cleanRel(srcRel)

	switch {
	case in.FlatOutput:
		return build(instance.Path.relString(), "/", srcRel, suffix), input
	case strings.Contains(srcRel, "/"):
		body, underscore := normalizeDotDotSegments(srcRel)

		if underscore {
			return build(instance.Path.relString(), "/_/", body, suffix), input
		}

		return build(instance.Path.relString(), "/", body, suffix), input
	default:
		return build(instance.Path.relString(), "/", srcRel, suffix), input
	}
}

func composeSrcDirOutputRel(instancePath, target string) string {
	rel, err := filepath.Rel(instancePath, target)

	if err != nil {
		return "_/" + filepath.Base(target)
	}

	if !strings.Contains(rel, "..") {
		joined := filepath.ToSlash(rel)

		if strings.Contains(joined, "/") {
			return "_/" + joined
		}

		return joined
	}

	parts := strings.Split(rel, string(filepath.Separator))

	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
		}
	}

	return strings.Join(parts, "/")
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

func appendCxxStdAndOwn(cmdArgs []ANY, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []ARG) []ANY {
	if isCxx {
		cmdArgs = append(cmdArgs, (cxxStandardFlag).any())

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				cmdArgs = appendArgAny(cmdArgs, noWarningsBundle)
			} else {
				cmdArgs = appendArgAny(cmdArgs, cxxStandardWarnings)
			}
		}
	}

	cmdArgs = appendArgAny(cmdArgs, ownExtras)

	return cmdArgs
}

func composePeerExtras(in ModuleCompileEnv, isCxx bool) []ARG {
	if isCxx {
		return in.PeerCXXFlagsGlobal
	}

	return in.PeerCOnlyFlagsGlobal
}

func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCompileEnv, p *Platform) []ARG {
	return concat(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal)
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

func appendCompileFlagPipeline(cmdArgs []ANY, bundle CompileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags, catboost []ARG) []ANY {
	return appendArgAny(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir, bundle.CFlags, warningBundle, defineBundle, preNoLibcExtras, bundle.NoLibcBlock, catboost, moduleScopeCFlags, bundle.NoLibcBlock)
}

type CcModuleArgBlocks struct {
	cHead    []ANY
	cxxHead  []ANY
	includes []ANY
	flags    []ANY
	cTail    []ANY
	cxxTail  []ANY
	cPost    []ANY
}

func cWarningChunk(na *NodeArenas, noCompilerWarnings, noWShadow bool) []ANY {
	switch {
	case noCompilerWarnings:
		return noWarningsBundleStr
	case noWShadow:
		return na.argAnyList(warningFlags, []ARG{argNoShadow})
	default:
		return warningFlagsStr
	}
}

func cxxWarningChunk(noCompilerWarnings bool) []ANY {
	if noCompilerWarnings {
		return noWarningsBundleStr
	}

	return cxxStandardWarningsStr
}

func catboostOpenSourceChunk(p *Platform) []ANY {
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
		cflagsStr = na.argAnyList(suppressOptimize(p.CompileCFlags))
	}

	catboostStr := catboostOpenSourceChunk(p)
	noLibc := p.NoLibcBlockStr
	incl := na.anys.alloc(len(ccIncludesPrefixStr) + len(in.AddIncl) + len(in.PeerAddInclGlobal))
	k := copy(incl, ccIncludesPrefixStr)

	for _, pt := range in.AddIncl {
		incl[k] = in.InclArgs.arg(pt).any()
		k++
	}

	for _, pt := range in.PeerAddInclGlobal {
		incl[k] = in.InclArgs.arg(pt).any()
		k++
	}

	na.anys.commit(k)

	includes := incl[:k:k]
	warnC := cWarningChunk(na, in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	forceDebug := [][]ANY(nil)

	if in.ForceConsistentDebug {
		forceDebug = [][]ANY{debugPrefixMapFlagsStr, xclangDebugCompilationDirStr}
	}

	flagParts := [][]ANY{
		na.argAnyList(in.ClangWarnings),
	}

	flagParts = append(flagParts, forceDebug...)

	flagParts = append(flagParts, [][]ANY{
		debugPrefixMapFlagsStr,
		xclangDebugCompilationDirStr,
		cflagsStr,
		warnC,
		p.DefinesStr,
		na.argAnyList(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal),
		noLibc,
		catboostStr,
		na.argAnyList(in.ModuleScopeCFlags),
		noLibc,
	}...)

	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concat(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)

	cxxTailParts := [][]ANY{
		cxxStandardFlagStr,
		cxxWarningChunk(in.Flags.NoCompilerWarnings),
		na.argAnyList(cxxOwnExtras),
		na.argAnyList(cxxBucket),
		catboostStr,
		na.argAnyList(composePostCatboostBucket(cxxBucket)),
	}

	return &CcModuleArgBlocks{
		cHead:    na.anyList(in.TC.CC.any()),
		cxxHead:  na.anyList(in.TC.CXX.any()),
		includes: includes,
		flags:    na.anyConcat(flagParts...),
		cTail:    na.argAnyList(in.PeerCOnlyFlagsGlobal),
		cxxTail:  na.anyConcat(cxxTailParts...),
		cPost:    na.argAnyList(in.COnlyFlags),
	}
}

func appendAddIncl(cmdArgs []ANY, addIncl []VFS, memo InclArgMemo) []ANY {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(p).any())
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

	if path.relString() == "" {
		a = internV("-I", strings.TrimSuffix(path.string(), "/"))
	}

	m.m.put(path, a)

	return a
}

func (e *EmitContext) emitLibraryCSource(meta SrcMeta) {
	src := meta.Source
	_, _, d := e.ctx, e.instance, e.d
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	}

	ref, outPath := e.emitCC(srcVFS)

	e.collectObj(ref, outPath, meta)
}
