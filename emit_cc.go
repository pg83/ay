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
	CCBlocks             CcModuleArgBlocks
	PeerAddInclGlobal    []VFS
	ProtoInclude         []VFS
	ProtoIncludePeers    []VFS
	PbHCompanionExt      string
	CXXFlags             []ANY
	COnlyFlags           []ANY
	ClangWarnings        []ANY
	SrcDirs              []VFS
	FS                   FS
	PeerCFlagsGlobal     []ANY
	PeerCXXFlagsGlobal   []ANY
	PeerCOnlyFlagsGlobal []ANY
	CFlags               []ANY
	ModuleScopeCFlags    []ANY
	OwnCFlagsGlobal      []ANY
	OwnCXXFlagsGlobal    []ANY
	OwnCOnlyFlagsGlobal  []ANY
	SFlags               []ANY
	DefaultVars          map[STR]STR
	DefaultVarOrder      []STR
	SetVars              map[STR]STR
	Py3Suffix            bool
	ObjectSuffixStem     *string
	NoOptimize           bool
	MainOutInducedInputs bool
	ModuleTag            STR
	Ragel6Flags          []ANY
	BisonFlags           []ANY
	BisonGenExt          string
	TC                   ModuleToolchain
	ForceConsistentDebug bool
}

type ModuleCCInputs struct {
	ModuleCompileEnv

	PerSourceCFlags []ANY
	FlatOutput      bool
	ForceCxx        bool
	Variant         *string
	ExtraDepRefs    []NodeRef
	IncludeInputs   []VFS
	IncludeView     Closure
}

func (e *EmitContext) ccInputsFor(compile CompileSpec) ModuleCCInputs {
	ctx, instance, d := e.ctx, e.instance, e.d
	env := d.cc
	in := ModuleCCInputs{ModuleCompileEnv: env}

	in.PerSourceCFlags = compile.CFlags
	in.FlatOutput = compile.FlatOutput
	in.ForceCxx = compile.ForceCxx

	if compile.Py3Suffix {
		in.Py3Suffix = true
	}

	if compile.Variant != 0 {
		variant := compile.Variant.string()

		in.Variant = &variant
	}

	envDelta := false

	if compile.EnvAddIncl != nil {
		in.AddIncl = compile.EnvAddIncl
		envDelta = true
	}

	if compile.EnvCFlags != nil {
		in.CFlags = compile.EnvCFlags
		envDelta = true
	}

	if envDelta {
		in.CCBlocks = composeCCModuleArgBlocks(ctx.na, instance.Platform, &in.ModuleCompileEnv)
	}

	return in
}

func (e *EmitContext) moduleSourceVFS(src ANY) VFS {
	_, _, d := e.ctx, e.instance, e.d

	return e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
}

func (e *EmitContext) emitCCWith(srcVFS VFS, in ModuleCCInputs, reserved NodeRef) (NodeRef, VFS) {
	ctx := e.ctx

	in.IncludeView = e.scanner.walkClosure(srcVFS, e.d.scanCtx, scanDomainCC)

	if in.MainOutInducedInputs {
		in.IncludeInputs = e.mainOutInducedInputs(ctx.na, in.IncludeView)
		in.IncludeView = Closure{}
	}

	ref, outPath, _ := e.composeCCNodeAt(srcVFS, in, ctx.host, reserved)

	return ref, outPath
}

func (e *EmitContext) mainOutInducedInputs(na *NodeArenas, includeView Closure) []VFS {
	reg := e.codegen
	extraMark := len(e.prodVFS)

	includeView.each(func(v VFS) {
		if !v.isBuild() {
			return
		}

		if info := reg.lookup(v); info != nil && info.ProducerMainOut != 0 {
			e.prodVFS = append(e.prodVFS, info.ProducerMainOut)
		}
	})

	extra := e.prodVFSTake(extraMark)
	out := na.vfs.alloc(2 * includeView.len())[:0]

	includeView.each(func(v VFS) {
		out = append(out, v)
	})

	out = append(out, extra...)
	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func (e *EmitContext) composeCCNode(srcVFS VFS, in ModuleCCInputs, hostP *Platform) (NodeRef, VFS, InputChunks) {
	return e.composeCCNodeAt(srcVFS, in, hostP, 0)
}

func (e *EmitContext) composeCCNodeAt(srcVFS VFS, in ModuleCCInputs, hostP *Platform, reserved NodeRef) (NodeRef, VFS, InputChunks) {
	instance := e.instance
	na := e.ctx.na
	srcRel := trimModulePrefix(srcVFS.relString(), instance.Path.relString())
	suffix := ccObjectSuffix(instance, in)

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

	chunks[k] = builtinMacroDateTime
	chunks[k+1] = macroPrefixMapFlags
	k += 2

	if len(in.PerSourceCFlags) > 0 {
		chunks[k] = na.anyConcat(in.PerSourceCFlags)
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

	if reserved != 0 {
		e.emitReservedNode(node, reserved)

		return reserved, outVFS, allInputs
	}

	return e.emitNode(node), outVFS, allInputs
}

func ccObjectSuffix(instance ModuleInstance, in ModuleCCInputs) string {
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

	return suffix
}

func (e *EmitContext) ccOutputFor(srcVFS VFS, compile CompileSpec) VFS {
	in := ModuleCCInputs{ModuleCompileEnv: e.d.cc, FlatOutput: compile.FlatOutput}

	if compile.Py3Suffix {
		in.Py3Suffix = true
	}

	if compile.Variant != 0 {
		variant := compile.Variant.string()

		in.Variant = &variant
	}

	srcRel := trimModulePrefix(srcVFS.relString(), e.instance.Path.relString())
	out, _ := composeCCPaths(e.instance, srcRel, srcVFS, in, ccObjectSuffix(e.instance, in))

	return out
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

		if rel != dir && !(len(rel) > len(dir) && rel[len(dir)] == '/' && rel[:len(dir)] == dir) {
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
	a, b := instancePath, target

	for a != "" && b != "" {
		ia := strings.IndexByte(a, '/')
		ib := strings.IndexByte(b, '/')
		ca, cb := a, b

		if ia >= 0 {
			ca = a[:ia]
		}

		if ib >= 0 {
			cb = b[:ib]
		}

		if ca != cb {
			break
		}

		if ia < 0 {
			a = ""
		} else {
			a = a[ia+1:]
		}

		if ib < 0 {
			b = ""
		} else {
			b = b[ib+1:]
		}
	}

	if a == "" {
		if b == "" {
			return "."
		}

		if strings.Contains(b, "/") {
			return "_/" + b
		}

		return b
	}

	ups := strings.Count(a, "/") + 1
	buf := make([]byte, 0, 3*ups+len(b))

	for i := 0; i < ups; i++ {
		if i > 0 {
			buf = append(buf, '/')
		}

		buf = append(buf, "__"...)
	}

	if b != "" {
		buf = append(buf, '/')
		buf = append(buf, b...)
	}

	return string(buf)
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

func pickWarningFlags(noCompilerWarnings bool, noWShadow bool) []ANY {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	if noWShadow {
		return concat(warningFlags, []ANY{argNoShadow.any()})
	}

	return warningFlags
}

func appendCxxStdAndOwn(cmdArgs []ANY, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []ANY) []ANY {
	if isCxx {
		cmdArgs = append(cmdArgs, cxxStandardFlag.any())

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				cmdArgs = appendAnyLists(cmdArgs, noWarningsBundle)
			} else {
				cmdArgs = appendAnyLists(cmdArgs, cxxStandardWarnings)
			}
		}
	}

	cmdArgs = appendAnyLists(cmdArgs, ownExtras)

	return cmdArgs
}

func composePeerExtras(in ModuleCompileEnv, isCxx bool) []ANY {
	if isCxx {
		return in.PeerCXXFlagsGlobal
	}

	return in.PeerCOnlyFlagsGlobal
}

func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCompileEnv, p *Platform) []ANY {
	return concat(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal)
}

func composeOwnAndPeerGlobalBucket(in ModuleCompileEnv, isCxx bool) []ANY {
	out := make([]ANY, 0,
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

func composePostCatboostBucket(preBucket []ANY) []ANY {
	for _, x := range preBucket {
		if x == baseUnitCxxNostdinc.any() {
			return preBucket
		}
	}

	out := make([]ANY, 0, len(preBucket)+1)

	out = append(out, preBucket...)
	out = append(out, baseUnitCxxNostdinc.any())

	return out
}

func appendCompileFlagPipeline(cmdArgs []ANY, bundle CompileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags, catboost []ANY) []ANY {
	return appendAnyLists(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir, bundle.CFlags, warningBundle, defineBundle, preNoLibcExtras, bundle.NoLibcBlock, catboost, moduleScopeCFlags, bundle.NoLibcBlock)
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
		return noWarningsBundle
	case noWShadow:
		return na.anyConcat(warningFlags, []ANY{argNoShadow.any()})
	default:
		return warningFlags
	}
}

func cxxWarningChunk(noCompilerWarnings bool) []ANY {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	return cxxStandardWarnings
}

func catboostOpenSourceChunk(p *Platform) []ANY {
	if p.Flags[envOPENSOURCE] == strYes {
		return catboostOpenSourceDefine
	}

	return nil
}

func suppressOptimize(cf []ANY) []ANY {
	for i, a := range cf {
		if a == argO3.any() {
			out := make([]ANY, len(cf))

			copy(out, cf)
			out[i] = argO0.any()

			return out
		}
	}

	return cf
}

func composeCCModuleArgBlocks(na *NodeArenas, p *Platform, in *ModuleCompileEnv) CcModuleArgBlocks {
	cflagsStr := p.CompileCFlags

	if in.NoOptimize {
		cflagsStr = suppressOptimize(p.CompileCFlags)
	}

	catboostStr := catboostOpenSourceChunk(p)
	noLibc := p.NoLibcBlock
	incl := na.anys.alloc(len(ccIncludesPrefix) + len(in.AddIncl) + len(in.PeerAddInclGlobal))
	k := copy(incl, ccIncludesPrefix)

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

	var flagBuf [13][]ANY

	nf := 0

	flagBuf[nf] = na.anyConcat(in.ClangWarnings)
	nf++

	if in.ForceConsistentDebug {
		flagBuf[nf] = debugPrefixMapFlags
		flagBuf[nf+1] = xclangDebugCompilationDir
		nf += 2
	}

	for _, part := range [10][]ANY{
		debugPrefixMapFlags,
		xclangDebugCompilationDir,
		cflagsStr,
		warnC,
		p.Defines,
		na.anyConcat(p.CFlags, in.CFlags, in.PeerCFlagsGlobal, in.OwnCFlagsGlobal),
		noLibc,
		catboostStr,
		na.anyConcat(in.ModuleScopeCFlags),
		noLibc,
	} {
		flagBuf[nf] = part
		nf++
	}

	flagParts := flagBuf[:nf]
	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concat(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)

	cxxTailParts := [6][]ANY{
		cxxStandardFlagChunk,
		cxxWarningChunk(in.Flags.NoCompilerWarnings),
		na.anyConcat(cxxOwnExtras),
		na.anyConcat(cxxBucket),
		catboostStr,
		composePostCatboostBucket(cxxBucket),
	}

	return CcModuleArgBlocks{
		cHead:    na.anyList(in.TC.CC.any()),
		cxxHead:  na.anyList(in.TC.CXX.any()),
		includes: includes,
		flags:    na.anyConcat(flagParts...),
		cTail:    na.anyConcat(in.PeerCOnlyFlagsGlobal),
		cxxTail:  na.anyConcat(cxxTailParts[:]...),
		cPost:    na.anyConcat(in.COnlyFlags),
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

	a := internV("-I", path.prefix(), path.relString())

	if path.relString() == "" {
		a = internV("-I", path.prefix()[:vfsPrefixLen-1])
	}

	m.m.put(path, a)

	return a
}
