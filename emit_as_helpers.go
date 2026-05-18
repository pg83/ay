package main

import (
	"path"
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
func composeASPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs) (out, input VFS) {
	useSrcDir := in.SrcDir != nil && *in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, *in.SrcDir, srcRel)
		return Build(instance.Path + "/" + outputRel + ".o"),
			Source(path.Clean(*in.SrcDir + "/" + srcRel))
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

	return Build(outRel), Source(instance.Path + "/" + srcRel)
}

// composeASCmdArgs builds the cmd_args bundle.
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, in ModuleCCInputs) []string {
	noStdInc := instance.Flags.NoStdInc

	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 3 + len(bundle.ArchArgs)

	// No-stdinc preNoLibcExtras: module's own CFLAGS + GLOBAL CFLAGS.
	// Empty for normal modules.
	noStdIncCFlags := []string(nil)
	if noStdInc {
		noStdIncCFlags = make([]string, 0, len(in.CFlags)+len(in.OwnCFlagsGlobal))
		noStdIncCFlags = append(noStdIncCFlags, in.CFlags...)
		noStdIncCFlags = append(noStdIncCFlags, in.OwnCFlagsGlobal...)
	}

	warnBundle := pickWarningFlags(instance.Flags.NoCompilerWarnings)

	var ownCFlags, autoPeerCFlags []string
	if !noStdInc {
		ownCFlags = composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)
		autoPeerCFlags = in.AutoPeerCFlags
	}

	preNoLibcExtras := make([]string, 0, len(noStdIncCFlags)+len(ownCFlags))
	preNoLibcExtras = append(preNoLibcExtras, noStdIncCFlags...)
	preNoLibcExtras = append(preNoLibcExtras, ownCFlags...)

	includes := composeASIncludes(in, noStdInc)

	betweenBlocks := len(catboostOpenSourceDefine) + len(autoPeerCFlags)
	betweenBlocks += len(bundle.CPUFeatures)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(noStdIncCFlags) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, instance.Platform.Tools.CC, "--target="+instance.Platform.Triple)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs, "-B"+binPath)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, preNoLibcExtras, autoPeerCFlags)
	cmdArgs = append(cmdArgs, in.SFlags...)
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

// composeASIncludes derives the include-tail slice following the source path in cmd_args.
func composeASIncludes(in ModuleCCInputs, noStdInc bool) []string {
	if noStdInc {
		return composeNoStdIncIncludes(in.AddIncl)
	}

	out := make([]string, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(ccIncludesSuffix)+len(in.PeerAddInclGlobal))
	out = append(out, ccIncludesPrefix...)
	out = appendAddIncl(out, in.AddIncl)
	out = append(out, ccIncludesSuffix...)
	out = appendAddIncl(out, in.PeerAddInclGlobal)

	return out
}
