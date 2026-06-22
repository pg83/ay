package main

import (
	"path/filepath"
	"strings"
)

var (
	argDashBBin = internStr("-B" + binPath)
)

type ModuleCCInputs struct {
	Flags   FlagSet
	AddIncl []VFS

	InclArgs InclArgMemo

	// CCBlocks: module-stable CC command-line spans, built ONCE and shared by every
	// per-source copy. Mutating an arg-relevant field (AddIncl/CFlags) requires a
	// rebuild. nil: EmitCC composes locally (tests).
	CCBlocks *CcModuleArgBlocks

	PeerAddInclGlobal []VFS
	// ProtoInclude is the ordered _PROTO__INCLUDE set for proto compiles (every
	// peered PROTO_LIBRARY's namespace + PROTO_ADDINCL). Distinct from
	// PeerAddInclGlobal, which feeds C++.
	ProtoInclude []VFS
	// ProtoIncludePeers is the peers-only _PROTO__INCLUDE set, the protoc command
	// band for LIBRARY-hosted .proto sources: the own namespace rides the
	// `-I=$(S)/cppOutRoot` arm and appears here only if a peer re-declares it.
	ProtoIncludePeers []VFS
	CXXFlags          []ARG
	COnlyFlags        []ARG
	// ClangWarnings is the autoincluded CLANG_WARNINGS, emitted after the -I block
	// and before the compile-flag pipeline.
	ClangWarnings []ARG

	ExtraDepRefs []NodeRef

	// ScanCfg is the sealed scan config every walkClosure uses; builders that
	// change the resolve-relevant addincl set reseal it.
	ScanCfg ScanContext

	// SrcDirs is the cumulative SRCDIR search path; index 0 is the module dir.
	// resolveSourceVFS searches it in reverse; output naming comes from the
	// resolved source VFS, not from this.
	SrcDirs []VFS

	FS FS

	IncludeInputs []VFS

	PeerCFlagsGlobal []ARG

	PeerCXXFlagsGlobal []ARG

	PeerCOnlyFlagsGlobal []ARG

	CFlags []ARG

	ModuleScopeCFlags []ARG

	OwnCFlagsGlobal []ARG

	OwnCXXFlagsGlobal []ARG

	OwnCOnlyFlagsGlobal []ARG

	SFlags []ARG

	PerSourceCFlags []ARG

	FlatOutput bool

	DefaultVars     map[STR]STR
	DefaultVarOrder []STR

	SetVars map[STR]STR

	Py3Suffix bool

	ObjectSuffixStem *string

	ForceCxx bool

	// NoOptimize reflects NO_OPTIMIZE(): when set, the compile C-flag vector's
	// optimize token (-O3) is reassigned to -O0.
	NoOptimize bool

	ModuleTag STR

	Variant *string

	Ragel6Flags []ARG

	BisonFlags []ARG

	BisonGenExt string

	// TC is the module's tool-invocation paths from the PEERDIR resource-global
	// closure, so every emitter takes its compiler / python / objcopy from the
	// peer toolchain, not ambient flags.
	TC ModuleToolchain
}

func emitCC(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS, InputChunks) {
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

	// All per-node STR tokens ride ONE arena block (sliced sub-chunks).
	tok := na.strList((inVFS).str(), argDashC.str(), argDashO.str(), (outVFS).str())
	inChunk := tok[0:1:1]

	// Chunk-count ceiling: wrapcc(3) + fixed(5) + per-source(1) + cPost(1) + in(1).
	const ccCmdArgsMax = 11

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
	chunks[k+3] = blocks.common
	chunks[k+4] = tail
	k += 5

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

	// Inputs are chunks — the include closure (a shared cached slice) is
	// referenced, never copied. When the wrapcc.py wrapper is active it is an
	// ${input:} run under the python resource, so the script joins the inputs and
	// the python resource joins the deps.
	wrap := len(instance.Platform.WrapccHead) > 0

	// IncludeInputs is the full input window (root included) — see walkClosure.
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
		Env:     env,
		Inputs:  allInputs,
		Outputs: na.vfsList(outVFS),
		KV:      KV{P: pkCC, PC: pcGreen},
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: instance.Path.rel()}

			if in.ModuleTag != 0 {
				tp.ModuleTag = in.ModuleTag
			}

			return tp
		}(),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.CCUsesResources,
	}

	if len(in.ExtraDepRefs) > 0 {
		// Every caller passes a fresh ExtraDepRefs, and DepRefs is never
		// appended-to after — share it.
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emit(node), outVFS, allInputs
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	// Compare against the canonical join. A clean srcRel needs no Clean or concat;
	// otherwise SRCS(../foo.cpp) yields a normalised srcVFS.Rel() the bare concat
	// would not match.
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

	// A build-rooted SRCS spelling (${BINDIR}/x.cpp) re-feeds a generated in-module
	// source: the object is $(B)/<rel>.o, keyed off the resolved build path (the
	// prefixed srcRel would bury the $(B)/ root). A module-relative srcRel naming a
	// $(B) leaf keeps the _/-rebase below.
	if srcVFS.isBuild() && vfsHasPrefix(srcRel) {
		rel := srcVFS.rel()

		if dir := instance.Path.rel(); rel == dir || strings.HasPrefix(rel, dir+"/") {
			return build(rel + suffix), input
		}
	}

	// A build-generated source OUTSIDE the module dir (SRCDIR ascent / sibling-rooted
	// SRCS): rebase under the module BINDIR, mapping each `..` into `__` — like the
	// $(S) SRCDIR branch above. srcRel is the full module-rooted path, so the switch
	// below would wrongly bury it under `_/`.
	if srcVFS.isBuild() && !in.FlatOutput {
		rel, dir := srcVFS.rel(), instance.Path.rel()

		if rel != dir && !strings.HasPrefix(rel, dir+"/") {
			outputRel := composeSrcDirOutputRel(dir, rel)

			return build(dir + "/" + outputRel + suffix), input
		}
	}

	// An explicit-dot SRCS token (SRCS(./generated/x.cpp)) reaches this switch with
	// the raw `./` prefix; strip redundant `.` segments before localizing under
	// `_/<dir>`. cleanRel keeps a leading `..` intact and is the identity on
	// already-clean tokens.
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

// composeSrcDirOutputRel rebases the resolved source path (a SRCDIR-found file)
// under the module's build dir, mapping any `..` ascent into `__` segments.
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
	// No ".." anywhere (common case): only the "_/" prefix concat remains.
	if !strings.Contains(rel, "..") {
		// A subdir source's object is localized under "_/" UNLESS its dir starts
		// with "__" — the branch a ".." ascent takes after its .. -> __ transform,
		// which a literal __-prefixed dir hits too: joined to BINDIR with no "_/".
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
		// Uppercase .C compiles as C++.
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
	// The result is read-only (only appended FROM into cmdArgs), so return the
	// shared flag slice directly.
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

// ccModuleArgBlocks are the module-stable spans of a CC command line, built ONCE
// and referenced as chunks by every CC node — no per-node flag copying:
//
//	cHead/cxxHead: [compiler] (the module toolchain's cc / c++)
//	common:        ccIncludesPrefix + the -I block + the compile flag pipeline
//	cTail/cxxTail: the variant span after the pipeline (peer C extras / cxx
//	               std + own extras + flag buckets) + the builtin macro and
//	               macro-prefix-map tail
//	cPost:         the C-only own extras (positioned AFTER per-source flags)
type CcModuleArgBlocks struct {
	cHead   []STR
	cxxHead []STR
	common  []STR
	cTail   []STR
	cxxTail []STR
	cPost   []STR
}

// suppressOptimize returns cf with the optimize token -O3 reassigned to -O0.
// Only release x86 carries an optimize token; debug/target vectors leave
// OPTIMIZE empty, so they are returned unchanged.
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

	// Sum the fixed slices appended below — a slight over-estimate beats an
	// under-estimate realloc.
	commonCap := 2 +
		len(ccIncludesPrefix) + len(in.AddIncl) + len(in.PeerAddInclGlobal) +
		len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warningBundle) + len(bundle.Defines) +
		len(ownCFlags) + 2*len(bundle.NoLibcBlock) + len(catboost) + len(in.ModuleScopeCFlags) +
		len(in.ClangWarnings)
	common := make([]STR, 0, commonCap)
	common = appendArgStr(common, ccIncludesPrefix)
	common = appendAddIncl(common, in.AddIncl, in.InclArgs)
	peerAddIncl := in.PeerAddInclGlobal

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		common = append(common, in.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	common = appendAddIncl(common, peerAddIncl, in.InclArgs)
	common = appendArgStr(common, in.ClangWarnings)
	common = appendCompileFlagPipeline(common, bundle, warningBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboost)

	// C variant: peer C extras follow the pipeline; OWN C extras trail the
	// per-source flags (cPost).
	cTail := na.argStrList(in.PeerCOnlyFlagsGlobal, builtinMacroDateTime, macroPrefixMapFlags)

	// C++ variant: std + warning bundle + own extras (module + platform), the
	// global flag buckets around the catboost define, then the shared tail.
	cxxOwnExtras := in.CXXFlags

	if len(p.CXXFlags) > 0 {
		cxxOwnExtras = concatARG(in.CXXFlags, p.CXXFlags)
	}

	cxxBucket := composeOwnAndPeerGlobalBucket(*in, true)
	cxxTail := appendCxxStdAndOwn(nil, true, in.Flags.NoCompilerWarnings, true, cxxOwnExtras)
	cxxTail = appendArgStr(cxxTail, cxxBucket, catboost, composePostCatboostBucket(cxxBucket), builtinMacroDateTime, macroPrefixMapFlags)

	return &CcModuleArgBlocks{
		cHead:   na.strList(in.TC.CC),
		cxxHead: na.strList(in.TC.CXX),
		common:  common,
		cTail:   cTail,
		cxxTail: cxxTail,
		cPost:   na.argStrList(in.COnlyFlags),
	}
}

func appendAddIncl(cmdArgs []STR, addIncl []VFS, memo InclArgMemo) []STR {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(p))
	}

	return cmdArgs
}

// inclArgMemo caches the "-I<path>" flag per addincl VFS — a pure function of the
// path, shared run-wide. The backing DenseMap (an array probe, not a map hash) is
// owned by genCtx so further VFS-keyed columns share its idx array; inclArgMemo
// holds a pointer, staying copyable by value.
type InclArgMemo struct {
	m *DenseMap[VFS, STR]
}

// newInclArgMemo builds a standalone memo with its own backing store. Production
// code uses ctx.inclArgs; this is for tests that emit CC/AS nodes without a genCtx.

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

	in.ExtraDepRefs = resolveCodegenDepRefs(ctx, instance, in.IncludeInputs)

	// A handwritten source that #includes a Cython-generated header from the same
	// module rides the header's induced "pyx" closure (the .pyx/.pxd source) and
	// the CY node's main output, appended as bare inputs (re-resolving the .pxd/.pyx
	// would re-pull the producer's `cdef extern` C closure). No-op unless the module
	// has a CYTHON_C_H / _API_H header the closure reaches; deps resolve over the
	// un-augmented closure above (the main output is already a dep through the header),
	// so ExtraDepRefs stay byte-identical.
	if len(d.cythonCpp) > 0 {
		in.IncludeInputs = cythonCompileInducedInputs(ctx, instance, in.IncludeInputs)
	}

	ref, outPath, _ := emitCC(instance, srcRel, srcVFS, in, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
