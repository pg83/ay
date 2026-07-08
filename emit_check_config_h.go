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
	cv := walkClosure(e.scanner, confVFS, d.cc.ScanCfg)
	env := envVarsVCS

	chRef := ctx.emit.emitNode(Node{
		Platform: ctx.target,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(),
			argSBuildScriptsCheckConfigHPy.any(),
			internV(instance.Path.relString(), "/", conf.string()).any(),
			(generatedVFS).any())),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(buildScriptsCheckConfigHPy, cv.self), cv.buckets...),
		Outputs:      na.vfsList(generatedVFS),
		KV:           &checkConfigHKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	var psc []ANY

	if p := d.perSrcCFlagsFor(generatedVFS.any()); p != nil {
		psc = *p
	}

	e.codegen.register(GeneratedFileInfo{
		OutputPath:  generatedVFS,
		ProducerRef: chRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(confVFS.rel().any())})},
		ClosureLeaves: e.ctx.na.vfsList(buildScriptsCheckConfigHPy),
		Compile:       e.ctx.na.compileSpec(CompileSpec{FlatOutput: d.flatSrc(generatedVFS.any()), CFlags: psc}),
	})

	e.enqueueSrc(SrcMeta{Source: generatedVFS.any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkCheckConfig})
}
