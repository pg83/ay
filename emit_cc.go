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

	InclArgs inclArgMemo

	PeerAddInclGlobal []VFS
	// PeerProtoAddInclGlobal is the _PROTO__INCLUDE chain for proto compiles
	// only (the $(S)/<PROTO_NAMESPACE> contribution of every transitively
	// peered PROTO_LIBRARY / PROTO_NAMESPACE GLOBAL). Distinct from
	// PeerAddInclGlobal which feeds the C++ compile pipeline.
	PeerProtoAddInclGlobal []VFS
	CXXFlags               []ARG
	COnlyFlags             []ARG

	ExtraDepRefs []NodeRef

	// SrcDirs is the cumulative SRCDIR search path (directory VFS); index 0 is
	// the module dir. resolveSourceVFS searches it (reverse); composeCCPaths
	// derives output naming from the resolved source VFS, not from this.
	SrcDirs []VFS

	SourceRoot string

	FS FS

	IncludeInputs []VFS

	NodeInputs []VFS

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

	DefaultVars     map[string]string
	DefaultVarOrder []string

	SetVars map[string]string

	Py3Suffix bool

	ObjectSuffixStem *string

	ForceCxx bool

	ModuleTag STR

	Variant *string

	Ragel6Flags []ARG

	BisonGenExt string

	// TC is the module's tool-invocation paths, derived from the PEERDIR resource-global
	// closure (d.tc). Threaded so every CC/codegen emitter takes its compiler / python /
	// objcopy from the peer toolchain rather than ambient platform flags.
	TC moduleToolchain
}

func EmitCC(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS, inputChunks) {
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

	var ownExtras []ARG

	if isCxx {
		ownExtras = in.CXXFlags
	} else {
		ownExtras = in.COnlyFlags
	}

	if isCxx && len(instance.Platform.CXXFlags) > 0 {
		ownExtras = append(append([]ARG{}, ownExtras...), instance.Platform.CXXFlags...)
	}

	peerExtras := composePeerExtras(in, isCxx)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in, isCxx)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)

	args := ccComposeArgs{
		Platform:           instance.Platform,
		OutVFS:             outVFS,
		InVFS:              inVFS,
		OwnAddIncl:         in.AddIncl,
		PeerAddIncl:        in.PeerAddInclGlobal,
		OwnCFlags:          ownCFlags,
		OwnExtras:          ownExtras,
		PeerExtras:         peerExtras,
		OwnGlobalBucket:    ownGlobalBucket,
		PerSrcCFlags:       in.PerSourceCFlags,
		ModuleScopeCFlags:  in.ModuleScopeCFlags,
		IsCxx:              isCxx,
		NoCompilerWarnings: in.Flags.NoCompilerWarnings,
		NoWShadow:          in.Flags.NoWShadow,
		InclArgs:           in.InclArgs,
		CCArg:              in.TC.CC,
		CXXArg:             in.TC.CXX,
	}
	cmdArgs := composeTargetCC(args)

	env := hostP.ToolEnv()

	// Inputs are assembled as chunks — the include closure (a shared cached
	// slice) is referenced, never copied. When the wrapcc.py compile wrapper is
	// active (non-opensource build, see wrapccPrefixFor) it is an ${input:} of
	// the CC node and runs under YMAKE_PYTHON3, so the script joins the inputs
	// as its own chunk and the python3 resource joins the deps.
	wrap := len(instance.Platform.WrapccHead) > 0

	var allInputs inputChunks

	if in.NodeInputs == nil {
		allInputs = append(allInputs, srcChunk(inVFS), in.IncludeInputs)
	} else {
		allInputs = append(allInputs, in.NodeInputs)
	}

	if wrap {
		allInputs = append(allInputs, wrapccPyChunk)
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  allInputs,
		Outputs: []VFS{outVFS},
		KV:      KV{P: pkCC, PC: pcGreen},
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: instance.Path.Rel()}

			if in.ModuleTag != 0 {
				tp.ModuleTag = in.ModuleTag
			}

			return tp
		}(),
		Requirements:  Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		usesResources: instance.Platform.CCUsesResources,
	}

	if len(in.ExtraDepRefs) > 0 {
		// Every caller passes a fresh ExtraDepRefs (literal/resolveCodegenDepRefs/
		// prepend), and the CC node's DepRefs is never appended-to after — share it.
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.Emit(node), outVFS, allInputs
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	// Compare against the canonical join (sources.go:resolveSourceVFS
	// path-cleans `..` / `.` segments, so SRCS(../foo.cpp) yields a
	// normalised srcVFS.Rel() like commands/foo.cpp — the bare
	// instance.Path+"/"+srcRel would still carry the unnormalised tail).
	canon := filepath.ToSlash(filepath.Clean(instance.Path.Rel() + "/" + srcRel))

	if srcVFS.IsSource() && srcVFS.Rel() != canon {
		outputRel := composeSrcDirOutputRel(instance.Path.Rel(), srcVFS.Rel())
		out = Build(instance.Path.Rel() + "/" + outputRel + suffix)
		return out, input
	}

	var outRel string

	switch {
	case in.FlatOutput:

		outRel = instance.Path.Rel() + "/" + srcRel + suffix
	case strings.Contains(srcRel, "/"):

		outRel = instance.Path.Rel() + "/" + normalizeDotDotSegments(srcRel) + suffix
	default:
		outRel = instance.Path.Rel() + "/" + srcRel + suffix
	}

	return Build(outRel), input
}

// composeSrcDirOutputRel rebases the resolved source path (target, e.g. a
// SRCDIR-found file like contrib/.../src/repmgr/x.c) under the module's build
// dir, mapping any `..` ascent into `__` segments.
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
		strings.HasSuffix(srcRel, ".cxx")
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
	// The result is only appended FROM into cmdArgs (read-only), so return the
	// shared flag slice directly rather than copying it.
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

// pickCompilerArg returns the compiler from the module's resolved toolchain
// (a.CCArg/CXXArg, derived from the PEERDIR resource-global closure — $(CLANG)/bin/clang[++]).
func pickCompilerArg(a ccComposeArgs) STR {
	if a.IsCxx {
		return a.CXXArg
	}

	return a.CCArg
}

type ccComposeArgs struct {
	Platform           *Platform
	OutVFS             VFS
	InVFS              VFS
	OwnAddIncl         []VFS
	PeerAddIncl        []VFS
	OwnCFlags          []ARG
	OwnExtras          []ARG
	PeerExtras         []ARG
	OwnGlobalBucket    []ARG
	PerSrcCFlags       []ARG
	ModuleScopeCFlags  []ARG
	IsCxx              bool
	NoCompilerWarnings bool
	NoWShadow          bool
	InclArgs           inclArgMemo
	CCArg              STR
	CXXArg             STR
}

func appendCompileFlagPipeline(cmdArgs []STR, bundle compileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags, catboost []ARG) []STR {
	return appendArgStr(cmdArgs, debugPrefixMapFlags, xclangDebugCompilationDir, bundle.CFlags, warningBundle, defineBundle, preNoLibcExtras, bundle.NoLibcBlock, catboost, moduleScopeCFlags, bundle.NoLibcBlock)
}

func composeTargetCC(a ccComposeArgs) []STR {
	bundle := compileFlagBundleFor(a.Platform)
	warningBundle := pickWarningFlags(a.NoCompilerWarnings, a.NoWShadow)

	// Sum the actual fixed package slices appended below (count cxx-only ones
	// unconditionally — a slight over-estimate beats an under-estimate realloc).
	// 12 covers the literals (compiler, --target, -B/-c/-o/output/input,
	// googleapis, cxxStandardFlag, post-catboost sentinel) plus slack.
	argCap := 12 +
		len(ccIncludesPrefix) +
		len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		2*len(catboostOpenSourceDefine) + len(cxxStandardWarnings) +
		len(builtinMacroDateTime) + len(macroPrefixMapFlags) +
		len(a.OwnAddIncl) + len(a.PeerAddIncl) + len(a.OwnCFlags) + len(a.OwnExtras) + len(a.PeerExtras) + 2*len(a.OwnGlobalBucket) + len(a.PerSrcCFlags) + len(a.ModuleScopeCFlags) +
		len(bundle.ArchArgs) + len(bundle.CFlags) + len(bundle.Defines) + 2*len(bundle.NoLibcBlock) + len(warningBundle)
	cmdArgs := make([]STR, 0, argCap+len(a.Platform.WrapccHead)+len(a.Platform.WrapccTail)+1)

	if len(a.Platform.WrapccHead) > 0 {
		cmdArgs = append(cmdArgs, a.Platform.WrapccHead...)
		cmdArgs = append(cmdArgs, (a.InVFS).str())
		cmdArgs = append(cmdArgs, a.Platform.WrapccTail...)
	}

	cmdArgs = append(cmdArgs, pickCompilerArg(a), a.Platform.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, argDashBBin, argDashC.str(), argDashO.str(), (a.OutVFS).str())
	cmdArgs = appendArgStr(cmdArgs, ccIncludesPrefix)
	cmdArgs = appendAddIncl(cmdArgs, a.OwnAddIncl, a.InclArgs)
	peerAddIncl := a.PeerAddIncl

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		cmdArgs = append(cmdArgs, a.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	cmdArgs = appendAddIncl(cmdArgs, peerAddIncl, a.InclArgs)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warningBundle, bundle.Defines, a.OwnCFlags, a.ModuleScopeCFlags, catboostOpenSourceDefineFor(a.Platform))

	var cOnlyExtras []ARG

	if a.IsCxx {
		cmdArgs = appendCxxStdAndOwn(cmdArgs, true, a.NoCompilerWarnings, true, a.OwnExtras)
	} else {
		cOnlyExtras = a.OwnExtras
	}

	if a.IsCxx {
		cmdArgs = appendArgStr(cmdArgs, a.OwnGlobalBucket, catboostOpenSourceDefineFor(a.Platform), composePostCatboostBucket(a.OwnGlobalBucket))
	} else {
		cmdArgs = appendArgStr(cmdArgs, a.PeerExtras)
	}

	cmdArgs = appendArgStr(cmdArgs, builtinMacroDateTime, macroPrefixMapFlags, a.PerSrcCFlags, cOnlyExtras)
	cmdArgs = append(cmdArgs, (a.InVFS).str())

	return cmdArgs
}

func appendAddIncl(cmdArgs []STR, addIncl []VFS, memo inclArgMemo) []STR {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(p))
	}

	return cmdArgs
}

// inclArgMemo caches the "-I<path>" compiler flag per addincl VFS — a pure
// function of the path, shared run-wide across both scanners' CC emission. The
// backing DenseMap (a plain array probe instead of the hot map[VFS]string hash)
// is owned by genCtx so further VFS-keyed value columns can share its idx array;
// inclArgMemo just holds a pointer to it, so it stays copyable by value.
type inclArgMemo struct {
	m *DenseMap[VFS, STR]
}

// newInclArgMemo builds a standalone memo with its own backing store. Production
// code uses ctx.inclArgs (backed by ctx.inclArgValues); this is for tests that
// emit CC/AS nodes without a genCtx.

func (m inclArgMemo) arg(path VFS) STR {
	if a, ok := m.m.Get(path); ok {
		return a
	}

	a := internStr("-I" + path.String())
	m.m.Put(path, a)

	return a
}
