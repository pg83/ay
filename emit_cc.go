package main

import (
	"path/filepath"
	"strings"
)

var (
	argDashBBin = internV("-B", binPath)
	ccKV        = KV{P: pkCC, PC: pcGreen}
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

func emitCC(instance ModuleInstance, src STR, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS, InputChunks) {
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
	inChunk := tok[0:1:1]

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
	chunks[k+2] = tok[1:4:4]
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
		out = build(instance.Path.rel(), "/", outputRel, suffix)

		return out, input
	}

	if srcVFS.isBuild() && vfsHasPrefix(srcRel) {
		rel := srcVFS.rel()

		if dir := instance.Path.rel(); rel == dir || strings.HasPrefix(rel, dir+"/") {
			return build(rel, suffix), input
		}
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

func composeCCModuleArgBlocks(na *NodeArenas, p *Platform, in *ModuleCCInputs) *CcModuleArgBlocks {
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

	fl := na.chunks.alloc(11)
	fl[0] = na.argStrList(in.ClangWarnings)
	fl[1] = debugPrefixMapFlagsStr
	fl[2] = xclangDebugCompilationDirStr
	fl[3] = cflagsStr
	fl[4] = cWarningChunk(na, in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)
	fl[5] = p.DefinesStr
	fl[6] = na.argStrList(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal)
	fl[7] = noLibc
	fl[8] = catboostStr
	fl[9] = na.argStrList(in.ModuleScopeCFlags)
	fl[10] = noLibc
	na.chunks.commit(11)

	ct := na.chunks.alloc(3)
	ct[0] = na.argStrList(in.PeerCOnlyFlagsGlobal)
	ct[1] = builtinMacroDateTimeStr
	ct[2] = macroPrefixMapFlagsStr
	na.chunks.commit(3)

	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concat(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)

	cxt := na.chunks.alloc(8)
	cxt[0] = cxxStandardFlagStr
	cxt[1] = cxxWarningChunk(in.Flags.NoCompilerWarnings)
	cxt[2] = na.argStrList(cxxOwnExtras)
	cxt[3] = na.argStrList(cxxBucket)
	cxt[4] = catboostStr
	cxt[5] = na.argStrList(composePostCatboostBucket(cxxBucket))
	cxt[6] = builtinMacroDateTimeStr
	cxt[7] = macroPrefixMapFlagsStr
	na.chunks.commit(8)

	var cPost ArgChunks

	if len(in.COnlyFlags) > 0 {
		cPost = na.chunkList(na.argStrList(in.COnlyFlags))
	}

	return &CcModuleArgBlocks{
		cHead:    na.strList(in.TC.CC),
		cxxHead:  na.strList(in.TC.CXX),
		includes: ArgChunks(inc[:ni:ni]),
		flags:    ArgChunks(fl[:11:11]),
		cTail:    ArgChunks(ct[:3:3]),
		cxxTail:  ArgChunks(cxt[:8:8]),
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

func emitLibraryCSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcVFS := src.vfs()

	if srcVFS == 0 {
		srcVFS = resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	}

	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)

	in.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, in.IncludeInputs)

	if len(d.cythonCpp) > 0 {
		in.IncludeInputs = cythonCompileInducedInputs(ctx, instance, in.IncludeInputs)
	}

	ref, outPath, _ := emitCC(instance, src, srcVFS, in, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
