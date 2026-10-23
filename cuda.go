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
	NvccFlags           []ANY
	Root                string
	TargetRoot          string
	Std                 string
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

	roots := resolveResourceGlobalArgs(expandStmtTokens([]ANY{
		internStr(env.string(envCUDA_ROOT)).any(),
		internStr(env.string(envCUDA_TARGET_ROOT)).any(),
	}, env), e.peers.ResourceGlobals)

	flags := internAnys(splitShellWords(expandStmtToken(env.string(envNvccFlags), env)))

	flags = resolveResourceGlobalArgs(flags, e.peers.ResourceGlobals)

	*c = CudaSettings{
		Ready: true, NvccFlags: e.ctx.na.anyList(flags...),
		Root: roots[0].string(), TargetRoot: roots[1].string(),
		Version: env.string(envCUDA_VERSION), Std: env.string(envNvccStdVer),
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
