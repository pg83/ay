package main

import (
	"path"
	"strings"
)

var checkConfigHKV = KV{P: pkCH, PC: pcYellow}

func checkConfigHGeneratedVFS(modulePath string, conf ANY) VFS {
	confBase := strings.TrimSuffix(path.Base(conf.string()), path.Ext(conf.string()))

	return build(modulePath, "/", confBase, ".config.cpp")
}

func (e *EmitContext) emitCheckConfigHStmt(conf ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	generatedVFS := checkConfigHGeneratedVFS(instance.Path.relString(), conf)
	confVFS := source(instance.Path.relString(), "/", conf.string())
	env := envVarsVCS
	chRef := ctx.emit.reserve()
	scanner := e.scanner
	scanCtx := d.scanCtx
	python3 := d.tc.Python3

	pe := func() {
		cv := scanner.walkClosure(confVFS, scanCtx, scanDomainCC)

		e.emitReservedNode(Node{
			Platform: ctx.target,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(python3.any(),
				argSBuildScriptsCheckConfigHPy.any(),
				internV(instance.Path.relString(), "/", conf.string()).any(),
				generatedVFS.any())),
				Env: env}),
			Env:          env,
			Inputs:       na.inputList(na.vfsList(buildScriptsCheckConfigHPy, cv.self), cv.buckets...),
			Outputs:      na.vfsList(generatedVFS),
			KV:           &checkConfigHKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			Resources:    usesPython3,
		}, chRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:  generatedVFS,
		ProducerRef: chRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(confVFS.rel().any())})},
		ClosureLeaves: e.ctx.na.vfsList(buildScriptsCheckConfigHPy),
		OnUse:         pending,
	})

	e.enqueueSrc(SrcMeta{Source: generatedVFS.any(), Prio: stmtPrioDefault, Bucket: bkCheckConfig})
}
