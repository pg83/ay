package main

import "strings"

type CudaSrcsStmt struct {
	DeclSeq       int
	Sources       []ANY
	Architectures string
	EmbedPTX      bool
}

type CudaSettings struct {
	Ready               bool
	Resources           []STR
	RootResources       []STR
	TargetResources     []STR
	NvccFlags           []ANY
	Root                string
	TargetRoot          string
	Std                 string
	ObjectSuffix        string
	Version             string
	SkipCompressedDebug bool
	Sanitize            bool
	SplitSanitize       bool
}

func (e *EmitContext) cudaSettings() *CudaSettings {
	c := &e.d.cuda

	if c.Ready {
		return c
	}

	env := Environment{s: &e.frame.ifEnv}
	root := commandTokenValue([]ANY{internStr(env.string(envCUDA_ROOT)).any()}, env)
	rootBound := resourceGlobalArgumentValue(root.Args, e.peers.ResourceGlobals)
	target := commandTokenValue([]ANY{internStr(env.string(envCUDA_TARGET_ROOT)).any()}, env)
	targetBound := resourceGlobalArgumentValue(target.Args, e.peers.ResourceGlobals)

	var flagUses []STR
	env.resourceUses = &flagUses

	flags := internAnys(splitShellWords(expandStmtToken(env.string(envNvccFlags), env)))
	flagsValue := resourceGlobalArgumentValue(flags, e.peers.ResourceGlobals)

	flags = flagsValue.Args

	rootUses := resourceKeyUnion(root.Resources, rootBound.Resources)
	targetUses := resourceKeyUnion(target.Resources, targetBound.Resources)

	*c = CudaSettings{
		Resources: resourceKeyUnion(rootUses, flagUses, flagsValue.Resources),
		Ready:     true, NvccFlags: e.ctx.na.anyList(flags...),
		Root: rootBound.Args[0].string(), TargetRoot: targetBound.Args[0].string(),
		RootResources: rootUses, TargetResources: targetUses,
		Version: env.string(envCUDA_VERSION), Std: env.string(envNvccStdVer),
		ObjectSuffix:        env.string(envNvccObjExt),
		SkipCompressedDebug: env.string(envCUDA_HOST_COMPILER_VERSION) == "14",
		Sanitize:            env.bool(envCUDA_SANITIZE), SplitSanitize: env.string(envCUDA_SANITIZE) != "",
	}

	return c
}

func cudaSourceBase(src string) string {
	if i := strings.LastIndexByte(src, '.'); i > strings.LastIndexByte(src, '/') {
		return src[:i]
	}

	return src
}
