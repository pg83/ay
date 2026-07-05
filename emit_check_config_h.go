package main

import (
	"path"
	"strings"
)

var checkConfigHKV = KV{P: pkCH, PC: pcYellow}

func checkConfigHGeneratedVFS(modulePath string, conf STR) VFS {
	confBase := strings.TrimSuffix(path.Base(conf.string()), path.Ext(conf.string()))

	return build(modulePath, "/", confBase, ".config.cpp")
}

func (e *EmitContext) emitCheckConfigHStmt(conf STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	generatedVFS := checkConfigHGeneratedVFS(instance.Path.rel(), conf)
	confVFS := source(instance.Path.rel(), "/", conf.string())
	cv := walkClosure(e.scanner, confVFS, d.cc.ScanCfg)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	chRef := ctx.emit.emitNode(Node{
		Platform: ctx.target,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(d.tc.Python3,
			argSBuildScriptsCheckConfigHPy.str(),
			internV(instance.Path.rel(), "/", conf.string()),
			(generatedVFS).str())),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(buildScriptsCheckConfigHPy, cv.self), cv.buckets...),
		Outputs:      na.vfsList(generatedVFS),
		KV:           &checkConfigHKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	var psc []ARG

	if p := d.perSrcCFlagsFor(generatedVFS.str()); p != nil {
		psc = *p
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:  generatedVFS,
		ProducerRef: chRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: []IncludeDirective{
			{kind: includeQuoted, target: internStr(confVFS.rel())},
		}},
		ClosureLeaves: []VFS{buildScriptsCheckConfigHPy},
		Compile:       &CompileSpec{FlatOutput: d.flatSrc(generatedVFS.str()), CFlags: psc},
	})

	e.enqueueSrc(SrcMeta{Source: generatedVFS.str(), Prio: stmtPrioDefault, Generated: true, Bucket: bkCheckConfig})
}
