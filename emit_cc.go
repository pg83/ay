package main

import (
	"path/filepath"
	"slices"
	"strings"
)

var (
	argDashBBin     = internV("-B", binPath)
	ccKV            = KV{P: pkCC, PC: pcGreen}
	cxxBisonFlags   = []ANY{argWnoUnusedButSetVariable.any(), argWnoDeprecatedCopy.any()}
	cxxRagelFlags   = []ANY{argWnoImplicitFallthrough.any()}
	cxxSwigFlags    = []ANY{argWnoDeprecatedDeclarations.any()}
	cxxLexerFlags   = []ANY{argWnoUnusedVariable.any()}
	coreCxxNostdinc = []ANY{baseUnitCxxNostdinc.any()}
)

type ModuleCompileEnv struct {
	CompilerEnv          Environment
	FlagResourceUses     [3][]STR
	CFlagValues          []CompilerFlagValue
	CXXFlagValues        []CompilerFlagValue
	COnlyFlagValues      []CompilerFlagValue
	StaticAnalyzer       *CppStaticAnalyzer
	FortranHead          []ANY
	FortranResources     []STR
	FortranResourceUses  []STR
	PchFlagsGlobal       []ANY
	Flags                FlagSet
	AddIncl              []VFS
	InclArgs             InclArgMemo
	CCBlocks             CcModuleArgBlocks
	PeerAddInclGlobal    []VFS
	ProtoInclude         []VFS
	ProtoIncludePeers    []VFS
	SwigInclude          []VFS
	PbHCompanionExt      string
	CXXFlags             []ANY
	CXXFlagsVar          []ANY
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
	ObjSuf               STR
	ObjectSuffixStem     *string
	NoOptimize           bool
	MainOutInducedInputs bool
	ModuleTag            STR
	Ragel6Flags          []ANY
	BisonFlags           []ANY
	BisonGenExt          string
	TC                   ModuleToolchain
	ForceConsistentDebug bool
	NoBaseUnit           bool
}

type ModuleCCInputs struct {
	*ModuleCompileEnv

	PerSourceCFlags []ANY
	AddIncl         []VFS
	CFlags          []ANY
	CCBlocks        *CcModuleArgBlocks
	FlatOutput      bool
	OutputStem      VFS
	ForceCxx        bool
	ObjSuf          STR
	Variant         *string
	IncludeInputs   []VFS
	IncludeView     Closure
	ExistingSource  VFS
}

func newModuleCCInputs(env *ModuleCompileEnv) ModuleCCInputs {
	return ModuleCCInputs{
		ModuleCompileEnv: env,
		AddIncl:          env.AddIncl,
		CFlags:           env.CFlags,
		CCBlocks:         &env.CCBlocks,
		ObjSuf:           env.ObjSuf,
	}
}

func (e *EmitContext) ccInputsFor(compile CompileSpec) ModuleCCInputs {
	ctx, instance, d := e.ctx, e.instance, e.d
	in := newModuleCCInputs(&d.cc)

	in.PerSourceCFlags = compile.CFlags
	in.FlatOutput = compile.FlatOutput
	in.OutputStem = compile.OutputStem
	in.ForceCxx = compile.ForceCxx

	if compile.ObjSuf != 0 {
		in.ObjSuf = compile.ObjSuf
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
		env := d.cc

		env.AddIncl = in.AddIncl
		env.CFlags = in.CFlags

		blocks := composeCCModuleArgBlocks(ctx.na, instance.Platform, &env)

		in.CCBlocks = &blocks
	}

	return in
}

func (e *EmitContext) moduleSourceVFS(src ANY) VFS {
	_, _, d := e.ctx, e.instance, e.d

	return e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
}

type CCPending struct {
	src    VFS
	out    VFS
	inputs ModuleCCInputs
	ref    NodeRef
}

func (e *EmitContext) emitCCWith(srcVFS, out VFS, in ModuleCCInputs, reserved NodeRef) (NodeRef, VFS, bool) {
	ctx := e.ctx

	if out == 0 {
		srcRel := trimModulePrefix(srcVFS.relString(), e.instance.Path.relString())

		out, _ = composeCCPaths(e.instance, srcRel, srcVFS, in, ccObjectSuffix(e.instance, in))
	}

	if e.deferCC {
		if !e.submitOutput(out) {
			return 0, out, false
		}

		if reserved == 0 {
			reserved = ctx.emit.reserve()
		}

		e.ccPending = append(e.ccPending, CCPending{src: srcVFS, out: out, inputs: in, ref: reserved})

		return reserved, out, true
	}

	if e.instance.Language == LangFbsJavaNative {
		ref, out := e.bindFbsNativeSource(srcVFS, out, in, reserved)

		return ref, out, true
	}

	configuration := e.instance.Demand == demandLinked && ctx.emit.deferPublication > 0 && srcVFS.isSource()

	if configuration {
		sc := e.scanner.getFileScanCtx(srcVFS, e.d.scanCtx, scanDomainCC, e.scanner.parsers.registry.defaultParser)

		defer e.scanner.putScanCtx(sc)

		sc.initialOnly = true
		in.IncludeView = sc.closureOf(srcVFS)
	} else {
		in.IncludeView = e.scanner.walkClosure(srcVFS, e.d.scanCtx, scanDomainCC)
	}

	if in.MainOutInducedInputs {
		in.IncludeInputs = e.mainOutInducedInputs(ctx.na, in.IncludeView)
	}

	if configuration && in.IncludeView.incomplete {
		id := e.scanner.includeFix(in.IncludeView.self, fixIncludes)
		fix := &e.scanner.fixer.entries.s[id-1]

		if fix.pending == nil {
			fix.pending = e.pendingEmit(func() {
				e.scanner.walkClosure(srcVFS, e.d.scanCtx, scanDomainCC)
			})
		}
	}

	if e.d.multimodule == tokFbsLibrary {
		ref, out := e.bindFbsNativeSource(srcVFS, out, in, reserved)

		return ref, out, true
	}

	ref, outPath, _ := e.composeCCNodeAt(srcVFS, out, in, ctx.host, reserved)

	return ref, outPath, true
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
	out := na.vfs.alloc(len(extra))[:0]

	out = append(out, extra...)
	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func (e *EmitContext) composeCCNodeAt(srcVFS, outVFS VFS, in ModuleCCInputs, hostP *Platform, reserved NodeRef) (NodeRef, VFS, InputChunks) {
	instance := e.instance
	na := e.ctx.na
	srcRel := trimModulePrefix(srcVFS.relString(), instance.Path.relString())
	inVFS := srcVFS
	isCxx := in.ForceCxx || isCxxSource(srcRel)

	var langFlags []ANY

	if isCxx && (instance.Platform.ifEnv.bool(envCLANG) || instance.Platform.ifEnv.bool(envCLANG_CL) || instance.Platform.ifEnv.bool(envGCC)) {
		langFlags = cxxLanguageFlags(strings.TrimSuffix(srcRel, filepath.Ext(srcRel)))
	}

	blocks := compilerCommandBlocks(in.CCBlocks, isCxx)
	tok := na.anyList(inVFS.any(), argDashC.any(), argDashO.any(), outVFS.any())
	inChunk := tok[0:1]
	wrapcc := len(blocks.wrapHead) > 0
	compiler := blocks.cHead

	var tail []ANY

	if isCxx {
		compiler = blocks.cxxHead
		tail = blocks.cxxTail
	}

	total := 4 + 5

	if wrapcc {
		total += 3
	}

	if len(tail) > 0 {
		total++
	}

	if len(in.PerSourceCFlags) > 0 {
		total++
	}

	if len(langFlags) > 0 {
		total++
	}

	if !isCxx && len(blocks.cPost) > 0 {
		total++
	}

	chunks := na.chunks.alloc(total)
	k := 0

	if wrapcc {
		chunks[k] = blocks.wrapHead
		chunks[k+1] = inChunk
		chunks[k+2] = instance.Platform.WrapccTail
		k += 3
	}

	chunks[k] = compiler
	chunks[k+1] = instance.Platform.CCHead
	chunks[k+2] = blocks.extraCFlags
	chunks[k+3] = tok[1:4]
	chunks[k+4] = blocks.includes
	chunks[k+5] = blocks.flags
	k += 6

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

	if len(langFlags) > 0 {
		chunks[k] = langFlags
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
	includeSelf := in.IncludeView.self
	includeBuckets := in.IncludeView.bucketList()
	nInputs := len(includeBuckets)

	if includeSelf != 0 {
		nInputs++
	}

	if wrapcc {
		nInputs++
	}

	if len(in.IncludeInputs) > 0 {
		nInputs++
	}

	inputChunks := na.inputs.alloc(nInputs)[:0]

	if includeSelf != 0 {
		inputChunks = append(inputChunks, na.vfsList(includeSelf))
	}

	if wrapcc {
		inputChunks = append(inputChunks, wrapccPyChunk)
	}

	inputChunks = append(inputChunks, includeBuckets...)

	if includeSelf != 0 && len(in.IncludeInputs) > 0 {
		inputChunks = append(inputChunks, in.IncludeInputs)
	}

	na.inputs.commit(len(inputChunks))

	allInputs := InputChunks(inputChunks[:len(inputChunks):len(inputChunks)])
	compilerCmd := Cmd{CmdArgs: cmdArgs, Env: env, Resources: blocks.resources}

	var cmds []Cmd

	resources := instance.Platform.CCUsesResources

	if analyzer := in.StaticAnalyzer; isCxx && analyzer != nil {
		analysisCmd := e.staticAnalyzerCmd(analyzer, in, inChunk, tok[1:4])

		cmds = na.cmdList(analysisCmd, compilerCmd)
		allInputs = appendInputChunks(na, allInputs, na.rootInputList(analyzer.inputs))
		resources = analyzer.resources
	} else {
		cmds = na.cmdList(compilerCmd)
	}

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: cmds,
		Env:  env, Inputs: allInputs, Outputs: na.vfsList(outVFS),
		KV:        &ccKV,
		Resources: resources}, input: InputSpec{root: in.IncludeView.self, existingSource: in.ExistingSource},
	}

	e.emitReservedNode(node, reserved)

	return reserved, outVFS, allInputs
}

func (env *ModuleCompileEnv) objSufDot() string {
	if env.ObjectSuffixStem != nil {
		return "." + *env.ObjectSuffixStem
	}

	if env.ObjSuf == 0 {
		return ""
	}

	return "." + env.ObjSuf.string()
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
	} else if in.ObjSuf != 0 {
		suffix = "." + in.ObjSuf.string() + suffix
	}

	if in.Variant != nil {
		suffix = "." + *in.Variant + suffix
	}

	return suffix
}

func (e *EmitContext) ccOutputFor(srcVFS VFS, compile CompileSpec) VFS {
	in := newModuleCCInputs(&e.d.cc)

	in.FlatOutput = compile.FlatOutput
	in.OutputStem = compile.OutputStem

	if compile.ObjSuf != 0 {
		in.ObjSuf = compile.ObjSuf
	}

	srcRel := trimModulePrefix(srcVFS.relString(), e.instance.Path.relString())
	out, _ := composeCCPaths(e.instance, srcRel, srcVFS, in, ccObjectSuffix(e.instance, in))

	return out
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	if in.OutputStem != 0 {
		return build(in.OutputStem.relString(), suffix), input
	}

	dir := instance.Path.rel().cleanRel()
	separator := "/"

	if dir == "" {
		separator = ""
	}

	rel := srcVFS.relString()
	srcRelClean := srcRel != "" && srcRel != "." && pathIsClean(srcRel)

	canonMatches := func() bool {
		if srcRelClean {
			if dir == "" {
				return rel == srcRel
			}

			return len(rel) == len(dir)+1+len(srcRel) &&
				rel[:len(dir)] == dir && rel[len(dir)] == '/' && rel[len(dir)+1:] == srcRel
		}

		return rel == filepath.ToSlash(filepath.Clean(joinRel(dir, srcRel)))
	}

	if srcVFS.isSource() && !canonMatches() {
		outputRel := composeSrcDirOutputRel(dir, rel)

		out = build(dir, separator, outputRel, suffix)

		return out, input
	}

	if srcVFS.isBuild() && !in.FlatOutput && dir != "" {
		if !(len(rel) > len(dir) && rel[len(dir)] == '/' && rel[:len(dir)] == dir) {
			outputRel := composeSrcDirOutputRel(dir, rel)

			return build(dir, separator, outputRel, suffix), input
		}
	}

	if !srcRelClean {
		srcRel = cleanRel(srcRel)
	}

	switch {
	case in.FlatOutput:
		return build(dir, separator, srcRel, suffix), input
	case strings.Contains(srcRel, "/"):
		body, underscore := normalizeDotDotSegments(srcRel)

		if underscore {
			return build(dir, separator, "_/", body, suffix), input
		}

		return build(dir, separator, body, suffix), input
	default:
		return build(dir, separator, srcRel, suffix), input
	}
}

func composeSrcDirOutputRel(instancePath, target string) string {
	a, b := instancePath, target

	for a != "" && b != "" {
		ia := strings.IndexByte(a, '/')
		ib := strings.IndexByte(b, '/')

		if ib < 0 {
			break
		}

		ca, cb := a, b[:ib]

		if ia >= 0 {
			ca = a[:ia]
		}

		if ca != cb {
			break
		}

		if ia < 0 {
			a = ""
		} else {
			a = a[ia+1:]
		}

		b = b[ib+1:]
	}

	if a == "" {
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

func composeOwnAndPeerGlobalBucket(in *ModuleCompileEnv) []ANY {
	out := make([]ANY, 0, len(in.OwnCXXFlagsGlobal)+len(in.PeerCXXFlagsGlobal))

	out = append(out, in.PeerCXXFlagsGlobal...)
	out = append(out, in.OwnCXXFlagsGlobal...)

	if len(out) == 0 {
		return nil
	}

	return out
}

func coreCxxFlags(p *Platform) []ANY {
	if p.ifEnv.bool(envMsvc) || p.ifEnv.bool(envUseStlSystem) {
		return nil
	}

	return coreCxxNostdinc
}

func appendCompileFlagPipeline(cmdArgs []ANY, bundle CompileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags, catboost []ANY) []ANY {
	return appendAnyLists(cmdArgs, bundle.DebugInfo, bundle.CFlags, warningBundle, defineBundle, preNoLibcExtras, bundle.NoLibcBlock, catboost, moduleScopeCFlags, bundle.NoLibcBlock)
}

type CcModuleArgBlocks struct {
	asHead        []ANY
	asResources   []STR
	compilerFlags *compilerFlagBlocks
	resources     []STR
	extraCFlags   []ANY
	wrapHead      []ANY
	cHead         []ANY
	cxxHead       []ANY
	includes      []ANY
	flags         []ANY
	cxxTail       []ANY
	cPost         []ANY
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
	binding := na.flagBlocks.one()

	*binding = compilerFlagBlocks{arena: na, platform: p, input: *in}
	binding.input.CCBlocks = CcModuleArgBlocks{}

	return CcModuleArgBlocks{compilerFlags: binding, includes: includes}
}

func composeCCCommandArgBlocks(na *NodeArenas, p *Platform, in *ModuleCompileEnv, includes []ANY) CcModuleArgBlocks {
	cflagsStr := p.CompileCFlags

	if in.NoOptimize {
		cflagsStr = suppressOptimize(p.CompileCFlags)
	}

	if in.CompilerEnv.string(envNO_DEBUGINFO) == "yes" && len(p.DebugInfoFlags) > 0 {
		flags := na.anys.alloc(len(cflagsStr))[:0]

		for _, flag := range cflagsStr {
			if !slices.Contains(p.DebugInfoFlags, flag) {
				flags = append(flags, flag)
			}
		}

		na.anys.commit(len(flags))
		cflagsStr = flags[:len(flags):len(flags)]
	}

	catboostStr := catboostOpenSourceChunk(p)
	noLibc := p.NoLibcBlock
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

	for _, part := range [9][]ANY{
		p.CLDebugInfo,
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

	cxxBucket := composeOwnAndPeerGlobalBucket(in)
	coreCxx := coreCxxFlags(p)

	if in.NoBaseUnit {
		coreCxx = nil
	}

	cxxTailParts := [7][]ANY{
		cxxStandardFlagChunk,
		cxxWarningChunk(in.Flags.NoCompilerWarnings),
		na.anyConcat(cxxOwnExtras),
		na.anyConcat(cxxBucket),
		catboostStr,
		coreCxx,
		in.CXXFlagsVar,
	}

	wrapHead := p.WrapccHead

	if p.WrapUnshare {
		wrapHead = na.anyConcat([]ANY{bldResourcesUnshareWrapperBinWrapUnshare.any()}, in.PchFlagsGlobal,
			[]ANY{argSourceRoot.any(), strS.any(), argBuildRoot.any(), strB.any(), strWrapUnshareEnd.any()}, wrapHead)
	}

	return CcModuleArgBlocks{
		wrapHead: wrapHead,
		cHead:    na.anyList(in.TC.CC.any()),
		cxxHead:  na.anyList(in.TC.CXX.any()),
		includes: includes,
		flags:    na.anyConcat(flagParts...),
		cxxTail:  na.anyConcat(cxxTailParts[:]...),
		cPost:    na.anyConcat(in.COnlyFlags, in.PeerCOnlyFlagsGlobal, in.OwnCOnlyFlagsGlobal),
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

	rel := path.relString()
	a := internV("-I", path.prefix(), rel)

	if rel == "" {
		a = internV("-I", path.prefix()[:vfsPrefixLen-1])
	}

	m.m.put(path, a)

	return a
}

func cxxLanguageFlags(stem string) []ANY {
	switch {
	case strings.HasSuffix(stem, ".y"), strings.HasSuffix(stem, ".ypp"):
		return cxxBisonFlags
	case strings.HasSuffix(stem, ".rl5"), strings.HasSuffix(stem, ".rl6"), strings.HasSuffix(stem, ".pyx"),
		strings.HasSuffix(stem, ".py.py3"), strings.HasSuffix(stem, ".py.py2"),
		strings.HasSuffix(stem, ".pyx.py3"), strings.HasSuffix(stem, ".pyx.py2"):
		return cxxRagelFlags
	case strings.HasSuffix(stem, ".swg"):
		return cxxSwigFlags
	case strings.HasSuffix(stem, ".l"), strings.HasSuffix(stem, ".g4"):
		return cxxLexerFlags
	}

	return nil
}

func (e *EmitContext) ccInputsForSource(meta SrcMeta) ModuleCCInputs {
	in := e.ccInputsFor(meta.Compile)

	if meta.Resolved.Existing {
		in.ExistingSource = meta.Resolved.Path
	}

	return in
}
