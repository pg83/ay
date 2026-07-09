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
	scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)
	python3 := d.tc.Python3

	pe := &PendingEmit{owner: ctx.instanceKey(instance), fn: func() {
		cv := walkClosure(scanner, confVFS, scanCfg)

		ctx.emit.emitReservedNode(Node{
			Platform: ctx.target,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(python3.any(),
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
		}, chRef)
	}}

	var psc []ANY

	if p := d.perSrcCFlagsFor(generatedVFS.any()); p != nil {
		psc = *p
	}

	info := e.codegen.register(GeneratedFileInfo{
		OutputPath:  generatedVFS,
		ProducerRef: chRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(confVFS.rel().any())})},
		ClosureLeaves: e.ctx.na.vfsList(buildScriptsCheckConfigHPy),
		Compile:       e.ctx.na.compileSpec(CompileSpec{FlatOutput: d.flatSrc(generatedVFS.any()), CFlags: psc}),
	})

	info.pending = pe

	e.noteOwn(pe)

	e.enqueueSrc(SrcMeta{Source: generatedVFS.any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkCheckConfig})
}
