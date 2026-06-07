package main

import (
	"strings"
)

var (
	yasmBinaryVFS  = Intern("$(B)/contrib/tools/yasm/yasm")
	yasmBinaryPath = yasmBinaryVFS.String()
)

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.IsSource() && srcVFS.Rel() != instance.Path+"/"+srcRel {
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

func composeASCmdArgs(instance ModuleInstance, outVFS, inVFS VFS, in ModuleCCInputs) []ANY {
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
	cmdArgs := make([]ANY, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, instance.Platform.CCArg, instance.Platform.TargetArg)
	cmdArgs = appendArgAny(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, argDashBBin)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags)
	cmdArgs = appendArgAny(cmdArgs, in.SFlags)
	cmdArgs = append(cmdArgs, argDashC, argDashO, vfsAny(outVFS), vfsAny(inVFS))
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

func composeASIncludes(in ModuleCCInputs) []ANY {
	out := make([]ANY, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))
	out = appendArgAny(out, ccIncludesPrefix)
	out = appendAddIncl(out, in.AddIncl, in.InclArgs)
	out = appendAddIncl(out, in.PeerAddInclGlobal, in.InclArgs)

	return out
}
