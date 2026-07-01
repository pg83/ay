package main

import (
	"path"
	"strings"
)

var checkConfigHKV = KV{P: pkCH, PC: pcYellow}

func (e *EmitContext) emitCheckConfigH() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.checkConfigHeaders) == 0 {
		return
	}

	for _, conf := range d.checkConfigHeaders {
		confBase := strings.TrimSuffix(path.Base(conf.string()), path.Ext(conf.string()))
		generated := confBase + ".config.cpp"
		generatedVFS := build(instance.Path.rel(), "/", generated)
		confVFS := source(instance.Path.rel(), "/", conf.string())
		inputs := []VFS{buildScriptsCheckConfigHPy}

		inputs = append(inputs, walkClosure(e.scanner, confVFS, d.cc.ScanCfg)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		chRef := ctx.emit.emit(&Node{
			Platform: ctx.target,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(d.tc.Python3,
				argSBuildScriptsCheckConfigHPy.str(),
				internV(instance.Path.rel(), "/", conf.string()),
				(generatedVFS).str())),
				Env: env}),
			Env:          env,
			Inputs:       na.inputList(inputs),
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
			ParsedIncludes: []IncludeDirective{
				{kind: includeQuoted, target: internStr(confVFS.rel())},
			},
			ClosureLeaves: []VFS{buildScriptsCheckConfigHPy},
			Compile:       &CompileSpec{FlatOutput: d.flatSrc(generatedVFS.str()), CFlags: psc},
		})

		e.emitGenerated(generatedVFS.str(), SrcMeta{Prio: stmtPrioDefault, Generated: true})
	}
}
