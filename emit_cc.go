package main

import (
	"path/filepath"
	"strings"
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
	CXXFlags               []string
	COnlyFlags             []string

	ExtraDepRefs []NodeRef

	SrcDir *string

	SourceRoot string

	FS FS

	IncludeInputs []VFS

	NodeInputs []VFS

	PeerCFlagsGlobal []string

	PeerCXXFlagsGlobal []string

	PeerCOnlyFlagsGlobal []string

	CFlags []string

	ModuleScopeCFlags []string

	OwnCFlagsGlobal []string

	OwnCXXFlagsGlobal []string

	OwnCOnlyFlagsGlobal []string

	SFlags []string

	PerSourceCFlags []string

	FlatOutput bool

	DefaultVars     map[string]string
	DefaultVarOrder []string

	SetVars map[string]string

	Py3Suffix bool

	ObjectSuffixStem *string

	ForceCxx bool

	ModuleTag *string

	Variant *string

	Ragel6Flags []string

	BisonGenExt string
}

func EmitCC(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS, []VFS) {
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
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	isCxx := in.ForceCxx || isCxxSource(srcRel)

	var ownExtras []string

	if isCxx {
		ownExtras = in.CXXFlags
	} else {
		ownExtras = in.COnlyFlags
	}

	if isCxx && len(instance.Platform.CXXFlags) > 0 {
		ownExtras = append(append([]string{}, ownExtras...), instance.Platform.CXXFlags...)
	}

	var cmdArgs []string

	peerExtras := composePeerExtras(in, isCxx)
	ownGlobalBucket := composeOwnAndPeerGlobalBucket(in, isCxx)
	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)

	args := ccComposeArgs{
		Platform:           instance.Platform,
		OutputPath:         outputPath,
		InputPath:          inputPath,
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
	}
	cmdArgs = composeTargetCC(args)

	env := hostP.ToolEnv()

	allInputs := in.NodeInputs

	if allInputs == nil {
		allInputs = make([]VFS, 0, 1+len(in.IncludeInputs))
		allInputs = append(allInputs, inVFS)
		allInputs = append(allInputs, in.IncludeInputs...)
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  allInputs,
		Outputs: []VFS{outVFS},
		KV: map[string]interface{}{
			"p":  "CC",
			"pc": "green",
		},
		Tags: instance.Platform.Tags,
		TargetProperties: func() map[string]string {
			tp := map[string]string{"module_dir": instance.Path}
			if in.ModuleTag != nil {
				tp["module_tag"] = *in.ModuleTag
			}

			return tp
		}(),
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = append([]NodeRef(nil), in.ExtraDepRefs...)
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outVFS, allInputs
}

func composeCCPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, suffix string) (out, input VFS) {
	input = srcVFS

	// Compare against the canonical join (sources.go:resolveSourceVFS
	// path-cleans `..` / `.` segments, so SRCS(../foo.cpp) yields a
	// normalised srcVFS.Rel() like commands/foo.cpp — the bare
	// instance.Path+"/"+srcRel would still carry the unnormalised tail).
	canon := filepath.ToSlash(filepath.Clean(instance.Path + "/" + srcRel))

	if srcVFS.IsSource() && srcVFS.Rel() != canon && in.SrcDir != nil {
		outputRel := composeSrcDirOutputRel(instance.Path, *in.SrcDir, srcRel)
		out = Build(instance.Path + "/" + outputRel + suffix)
		return out, input
	}

	var outRel string

	switch {
	case in.FlatOutput:

		outRel = instance.Path + "/" + srcRel + suffix
	case strings.Contains(srcRel, "/"):

		outRel = instance.Path + "/" + normalizeDotDotSegments(srcRel) + suffix
	default:
		outRel = instance.Path + "/" + srcRel + suffix
	}

	return Build(outRel), input
}

func sourceExistsLocally(fs FS, modulePath, srcRel string) bool {
	return fs.IsFile(modulePath + "/" + srcRel)
}

func composeSrcDirOutputRel(instancePath, srcDir, srcRel string) string {
	target := filepath.Join(srcDir, srcRel)
	rel, err := filepath.Rel(instancePath, target)

	if err != nil {
		return "_/" + srcRel
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

func pickCompiler(tools Toolchain, isCxx bool) string {
	if isCxx {
		return tools.CXX
	}

	return tools.CC
}

func pickWarningFlags(noCompilerWarnings bool, noWShadow bool) []string {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	if noWShadow {
		return append(append([]string{}, warningFlags...), "-Wno-shadow")
	}

	return warningFlags
}

func appendCxxStdAndOwn(cmdArgs []string, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []string) []string {
	if isCxx {
		cmdArgs = append(cmdArgs, cxxStandardFlag)

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				cmdArgs = append(cmdArgs, noWarningsBundle...)
			} else {
				cmdArgs = append(cmdArgs, cxxStandardWarnings...)
			}
		}
	}

	cmdArgs = append(cmdArgs, ownExtras...)

	return cmdArgs
}

func composePeerExtras(in ModuleCCInputs, isCxx bool) []string {
	if isCxx {
		out := make([]string, 0, len(in.PeerCXXFlagsGlobal))
		out = append(out, in.PeerCXXFlagsGlobal...)

		return out
	}

	out := make([]string, 0, len(in.PeerCOnlyFlagsGlobal))
	out = append(out, in.PeerCOnlyFlagsGlobal...)

	return out
}

func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCCInputs, p *Platform) []string {
	out := make([]string, 0, len(p.CFlags)+len(in.CFlags)+len(in.PeerCFlagsGlobal)+len(in.OwnCFlagsGlobal))
	out = append(out, p.CFlags...)
	out = append(out, in.CFlags...)
	out = append(out, in.PeerCFlagsGlobal...)
	out = append(out, in.OwnCFlagsGlobal...)

	return out
}

func platformCompilerFlags(p *Platform, isCxx bool) []string {
	if len(p.CFlags) == 0 && (!isCxx || len(p.CXXFlags) == 0) {
		return nil
	}

	out := make([]string, 0, len(p.CFlags)+len(p.CXXFlags))
	out = append(out, p.CFlags...)

	if isCxx {
		out = append(out, p.CXXFlags...)
	}

	return out
}

const baseUnitCxxNostdinc = "-nostdinc++"

func composeOwnAndPeerGlobalBucket(in ModuleCCInputs, isCxx bool) []string {
	out := make([]string, 0,
		len(in.OwnCXXFlagsGlobal)+len(in.PeerCXXFlagsGlobal)+
			len(in.OwnCOnlyFlagsGlobal)+len(in.PeerCOnlyFlagsGlobal))
	seen := make(map[string]struct{}, cap(out))
	addEach := func(src []string) {
		for _, x := range src {
			if _, dup := seen[x]; dup {
				continue
			}
			seen[x] = struct{}{}
			out = append(out, x)
		}
	}

	if isCxx {
		addEach(in.OwnCXXFlagsGlobal)
		addEach(in.PeerCXXFlagsGlobal)
	} else {
		addEach(in.OwnCOnlyFlagsGlobal)
		addEach(in.PeerCOnlyFlagsGlobal)
	}

	return out
}

func composePostCatboostBucket(preBucket []string) []string {
	for _, x := range preBucket {
		if x == baseUnitCxxNostdinc {
			return preBucket
		}
	}

	out := make([]string, 0, len(preBucket)+1)
	out = append(out, preBucket...)
	out = append(out, baseUnitCxxNostdinc)

	return out
}

type ccComposeArgs struct {
	Platform           *Platform
	OutputPath         string
	InputPath          string
	OwnAddIncl         []VFS
	PeerAddIncl        []VFS
	OwnCFlags          []string
	OwnExtras          []string
	PeerExtras         []string
	OwnGlobalBucket    []string
	PerSrcCFlags       []string
	ModuleScopeCFlags  []string
	IsCxx              bool
	NoCompilerWarnings bool
	NoWShadow          bool
	InclArgs           inclArgMemo
}

func appendCompileFlagPipeline(cmdArgs []string, bundle compileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, moduleScopeCFlags []string) []string {
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, bundle.CFlags...)
	cmdArgs = append(cmdArgs, warningBundle...)
	cmdArgs = append(cmdArgs, defineBundle...)
	cmdArgs = append(cmdArgs, preNoLibcExtras...)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, moduleScopeCFlags...)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)

	return cmdArgs
}

func composeTargetCC(a ccComposeArgs) []string {
	bundle := compileFlagBundleFor(a.Platform)
	warningBundle := pickWarningFlags(a.NoCompilerWarnings, a.NoWShadow)

	argCap := 101 + len(a.OwnAddIncl) + len(a.PeerAddIncl) + len(a.OwnCFlags) + len(a.OwnExtras) + len(a.PeerExtras) + 2*len(a.OwnGlobalBucket) + len(a.PerSrcCFlags) + len(a.ModuleScopeCFlags) + 4 +
		len(bundle.ArchArgs) + len(bundle.CFlags) + len(bundle.Defines) + 2*len(bundle.NoLibcBlock) + len(warningBundle)
	cmdArgs := make([]string, 0, argCap)
	cmdArgs = append(cmdArgs,
		pickCompiler(a.Platform.Tools, a.IsCxx),
		"--target="+a.Platform.Triple,
	)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs,
		"-B"+binPath,
		"-c",
		"-o",
		a.OutputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, a.OwnAddIncl, a.InclArgs)
	peerAddIncl := a.PeerAddIncl

	if len(peerAddIncl) > 0 && peerAddIncl[0] == googleapisCommonProtosAddIncl {
		cmdArgs = append(cmdArgs, a.InclArgs.arg(peerAddIncl[0]))
		peerAddIncl = peerAddIncl[1:]
	}

	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, peerAddIncl, a.InclArgs)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warningBundle, bundle.Defines, a.OwnCFlags, a.ModuleScopeCFlags)

	var cOnlyExtras []string

	if a.IsCxx {
		cmdArgs = appendCxxStdAndOwn(cmdArgs, true, a.NoCompilerWarnings, true, a.OwnExtras)
	} else {
		cOnlyExtras = a.OwnExtras
	}

	if a.IsCxx {
		cmdArgs = append(cmdArgs, a.OwnGlobalBucket...)
		cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
		cmdArgs = append(cmdArgs, composePostCatboostBucket(a.OwnGlobalBucket)...)
	} else {
		cmdArgs = append(cmdArgs, a.PeerExtras...)
	}

	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)

	cmdArgs = append(cmdArgs, a.PerSrcCFlags...)

	cmdArgs = append(cmdArgs, cOnlyExtras...)
	cmdArgs = append(cmdArgs, a.InputPath)

	return cmdArgs
}

func appendAddIncl(cmdArgs []string, addIncl []VFS, memo inclArgMemo) []string {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, memo.arg(p))
	}

	return cmdArgs
}

type inclArgMemo map[VFS]string

func (m inclArgMemo) arg(path VFS) string {
	if s, ok := m[path]; ok {
		return s
	}

	s := "-I" + path.String()
	m[path] = s

	return s
}
