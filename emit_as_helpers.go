package main

import (
	"strings"
)

// yasmBinaryVFS is the canonical $(B)-relative host yasm binary path;
// yasmBinaryPath is its String() form. Hardcoded because the only
// consumer is the asmlib host-PIC branch (gated by asmlibYasmModules);
// yasm's PROGRAM directory is stable.
var (
	yasmBinaryVFS  = Build("contrib/tools/yasm/yasm")
	yasmBinaryPath = yasmBinaryVFS.String()
)

// composeASPaths derives (outputPath, inputPath) for the clang AS path.
// `srcVFS` is the resolved input path; the output is computed from
// (instance.Path, srcRel, srcVFS.Root) — SRCDIR redirect when
// srcVFS.Rel diverges from `instance.Path/<srcRel>`. `in.SrcDir` (if
// non-nil) carries the original SRCDIR value used to compose the
// case-3 output infix.
func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.IsSource() && srcVFS.Rel != instance.Path+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path, *in.SrcDir, srcRel)
		return Build(instance.Path + "/" + outputRel + ".o"), srcVFS
	}

	var outRel string
	outName := srcRel + ".o"
	if strings.HasSuffix(srcRel, ".asm") {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path + "/_/" + outName
	} else {
		outRel = instance.Path + "/" + outName
	}

	return Build(outRel), srcVFS
}

// composeASCmdArgs builds the cmd_args bundle.
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, in ModuleCCInputs) []string {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 3 + len(bundle.ArchArgs)

	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)

	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)
	autoPeerCFlags := in.AutoPeerCFlags

	includes := composeASIncludes(in)

	betweenBlocks := len(catboostOpenSourceDefine) + len(autoPeerCFlags)
	betweenBlocks += len(bundle.CPUFeatures)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, instance.Platform.Tools.CC, "--target="+instance.Platform.Triple)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs, "-B"+binPath)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, autoPeerCFlags)
	cmdArgs = append(cmdArgs, in.SFlags...)
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

// composeASIncludes derives the include-tail slice following the source path in cmd_args.
func composeASIncludes(in ModuleCCInputs) []string {
	out := make([]string, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(ccIncludesSuffix)+len(in.PeerAddInclGlobal))
	out = append(out, ccIncludesPrefix...)
	out = appendAddIncl(out, in.AddIncl)
	out = append(out, ccIncludesSuffix...)
	out = appendAddIncl(out, in.PeerAddInclGlobal)

	return out
}
