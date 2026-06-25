package main

import (
	"path/filepath"
	"strings"
)

var (
	argDashBBin = internStr("-B" + binPath)
)

type ModuleCCInputs struct {
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
	ExtraDepRefs         []NodeRef
	ScanCfg              ScanContext
	SrcDirs              []VFS
	FS                   FS
	IncludeInputs        []VFS
	PeerCFlagsGlobal     []ARG
	PeerCXXFlagsGlobal   []ARG
	PeerCOnlyFlagsGlobal []ARG
	CFlags               []ARG
	ModuleScopeCFlags    []ARG
	OwnCFlagsGlobal      []ARG
	OwnCXXFlagsGlobal    []ARG
	OwnCOnlyFlagsGlobal  []ARG
	SFlags               []ARG
	PerSourceCFlags      []ARG
	FlatOutput           bool
	DefaultVars          map[STR]STR
	DefaultVarOrder      []STR
	SetVars              map[STR]STR
	Py3Suffix            bool
	ObjectSuffixStem     *string
	ForceCxx             bool
	NoOptimize           bool
	ModuleTag            STR
	Variant              *string
	Ragel6Flags          []ARG
	BisonFlags           []ARG
	BisonGenExt          string
	TC                   ModuleToolchain
}

func emitCC(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS, InputChunks) {
	na := emit.nodeArenas()

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
	inChunk := tok[0:1:1]

	const ccCmdArgsMax = 12

	chunks := na.chunks.alloc(ccCmdArgsMax)
	k := 0

	if len(instance.Platform.WrapccHead) > 0 {
		chunks[k] = instance.Platform.WrapccHead
		chunks[k+1] = inChunk
		chunks[k+2] = instance.Platform.WrapccTail
		k += 3
	}

	compiler, tail := blocks.cHead, blocks.cTail

	if isCxx {
		compiler, tail = blocks.cxxHead, blocks.cxxTail
	}

	chunks[k] = compiler
	chunks[k+1] = instance.Platform.CCHead
	chunks[k+2] = tok[1:4:4]
	chunks[k+3] = blocks.includes
	chunks[k+4] = blocks.flags
	chunks[k+5] = tail
	k += 6

	if len(in.PerSourceCFlags) > 0 {
		chunks[k] = na.argStrList(in.PerSourceCFlags)
		k++
	}

	if !isCxx && len(blocks.cPost) > 0 {
		chunks[k] = blocks.cPost
		k++
	}

	chunks[k] = inChunk
	k++
	na.chunks.commit(k)
	cmdArgs := ArgChunks(chunks[:k:k])

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
		out = build(instance.Path.rel() + "/" + outputRel + suffix)

		return out, input
	}

	if srcVFS.isBuild() && vfsHasPrefix(srcRel) {
		rel := srcVFS.rel()

		if dir := instance.Path.rel(); rel == dir || strings.HasPrefix(rel, dir+"/") {
			return build(rel + suffix), input
		}
	}

	if srcVFS.isBuild() && !in.FlatOutput {
		rel, dir := srcVFS.rel(), instance.Path.rel()

		if rel != dir && !strings.HasPrefix(rel, dir+"/") {
			outputRel := composeSrcDirOutputRel(dir, rel)

			return build(dir + "/" + outputRel + suffix), input
		}
	}

	srcRel = cleanRel(srcRel)

	var outRel string

	switch {
	case in.FlatOutput:

		outRel = instance.Path.rel() + "/" + srcRel + suffix
	case strings.Contains(srcRel, "/"):

		outRel = instance.Path.rel() + "/" + normalizeDotDotSegments(srcRel) + suffix
	default:
		outRel = instance.Path.rel() + "/" + srcRel + suffix
	}

	return build(outRel), input
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

func normalizeDotDotSegments(rel string) string {
	if !strings.Contains(rel, "..") {
		if strings.HasPrefix(rel, "__") {
			return rel
		}

		return "_/" + rel
	}

	parts := strings.Split(rel, "/")
	hasParent := false

	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
			hasParent = true
		}
	}

	if !hasParent {
		return "_/" + strings.Join(parts, "/")
	}

	return strings.Join(parts, "/")
}

func isCxxSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".cpp") ||
		strings.HasSuffix(srcRel, ".cc") ||
		strings.HasSuffix(srcRel, ".cxx") ||

		strings.HasSuffix(srcRel, ".C")
}

func pickWarningFlags(noCompilerWarnings bool, noWShadow bool) []ARG {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	if noWShadow {
		return append(append([]ARG{}, warningFlags...), argNoShadow)
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

func composePeerExtras(in ModuleCCInputs, isCxx bool) []ARG {
	if isCxx {
		return in.PeerCXXFlagsGlobal
	}

	return in.PeerCOnlyFlagsGlobal
}

func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCCInputs, p *Platform) []ARG {
	out := make([]ARG, 0, len(p.CFlags)+len(in.CFlags)+len(in.PeerCFlagsGlobal)+len(in.OwnCFlagsGlobal))
	out = append(out, p.CFlags...)
	out = append(out, in.CFlags...)
	out = append(out, in.PeerCFlagsGlobal...)
	out = append(out, in.OwnCFlagsGlobal...)

	return out
}

func composeOwnAndPeerGlobalBucket(in ModuleCCInputs, isCxx bool) []ARG {
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

	return dedupARG(out)
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
	includes []STR
	flags    []STR
	cTail    []STR
	cxxTail  []STR
	cPost    []STR
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

func composeCCModuleArgBlocks(na *NodeArenas, p *Platform, in *ModuleCCInputs) *CcModuleArgBlocks {
	bundle := compileFlagBundleFor(p)

	if in.NoOptimize {
		bundle.CFlags = suppressOptimize(bundle.CFlags)
	}

	warningBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(*in, p)
	catboost := catboostOpenSourceDefineFor(p)

	commonCap := 2 +
		len(ccIncludesPrefix) + len(in.AddIncl) + len(in.PeerAddInclGlobal) +
		len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warningBundle) + len(bundle.Defines) +
		len(ownCFlags) + 2*len(bundle.NoLibcBlock) + len(catboost) + len(in.ModuleScopeCFlags) +
		len(in.ClangWarnings)
	includes := make([]STR, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))
	includes = appendArgStr(includes, ccIncludesPrefix)
	includes = appendAddIncl(includes, in.AddIncl, in.InclArgs)
	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		includes = append(includes, in.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	includes = appendAddIncl(includes, peerAddIncl, in.InclArgs)

	flags := make([]STR, 0, commonCap-len(includes))
	flags = appendArgStr(flags, in.ClangWarnings)
	flags = appendCompileFlagPipeline(flags, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboost)

	cTail := na.argStrList(in.PeerCOnlyFlagsGlobal, builtinMacroDateTime, macroPrefixMapFlags)

	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concatARG(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)
	cxxTail := appendCxxStdAndOwn(nil, true, in.Flags.NoCompilerWarnings, true, cxxOwnExtras)
	cxxTail = appendArgStr(cxxTail, cxxBucket, catboost, composePostCatboostBucket(cxxBucket), builtinMacroDateTime, macroPrefixMapFlags)

	return &CcModuleArgBlocks{
		cHead:    na.strList(in.TC.CC),
		cxxHead:  na.strList(in.TC.CXX),
		includes: includes,
		flags:    flags,
		cTail:    cTail,
		cxxTail:  cxxTail,
		cPost:    na.argStrList(in.COnlyFlags),
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

	a := internStr("-I" + path.string())
	m.m.put(path, a)

	return a
}

func emitLibraryCSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)

	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)

	in.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, in.IncludeInputs)

	if len(d.cythonCpp) > 0 {
		in.IncludeInputs = cythonCompileInducedInputs(ctx, instance, in.IncludeInputs)
	}

	ref, outPath, _ := emitCC(instance, srcRel, srcVFS, in, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}

var (
	ccKV = KV{P: pkCC, PC: pcGreen}
)
