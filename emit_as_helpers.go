package main

import (
	"strings"
)

var (
	yasmBinaryPath = yasmBinaryVFS.String()
)

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.IsSource() && srcVFS.Rel() != instance.Path.Rel()+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path.Rel(), srcVFS.Rel())
		return Build(instance.Path.Rel() + "/" + outputRel + ".o"), srcVFS
	}

	var outRel string
	outName := srcRel + ".o"

	if strings.HasSuffix(srcRel, ".asm") {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path.Rel() + "/_/" + outName
	} else {
		outRel = instance.Path.Rel() + "/" + outName
	}

	return Build(outRel), srcVFS
}

func composeASCmdArgs(instance ModuleInstance, outVFS, inVFS VFS, in ModuleCCInputs) []STR {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 3 + len(bundle.ArchArgs)

	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)

	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)

	includes := composeASIncludes(in)

	betweenBlocks := len(catboostOpenSourceDefine)
	betweenBlocks += len(in.ModuleScopeCFlags)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4
	cmdArgs := make([]STR, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, in.TC.CC, instance.Platform.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, argDashBBin)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(instance.Platform))
	cmdArgs = appendArgStr(cmdArgs, in.SFlags)
	cmdArgs = append(cmdArgs, argDashC.str(), argDashO.str(), (outVFS).str(), (inVFS).str())
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

func composeASIncludes(in ModuleCCInputs) []STR {
	out := make([]STR, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))
	out = appendArgStr(out, ccIncludesPrefix)
	out = appendAddIncl(out, in.AddIncl, in.InclArgs)
	out = appendAddIncl(out, in.PeerAddInclGlobal, in.InclArgs)

	return out
}
