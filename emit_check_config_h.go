package main

import (
	"path"
	"strings"
)

func emitCheckConfigH(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []*SourceEmit {
	na := ctx.na

	if len(d.checkConfigHeaders) == 0 {
		return nil
	}

	out := make([]*SourceEmit, 0, len(d.checkConfigHeaders))

	for _, conf := range d.checkConfigHeaders {
		confBase := strings.TrimSuffix(path.Base(conf.string()), path.Ext(conf.string()))
		generated := confBase + ".config.cpp"
		generatedVFS := build(instance.Path.rel() + "/" + generated)

		confVFS := source(instance.Path.rel() + "/" + conf.string())
		// The walk window leads with confVFS itself — no separate prepend.
		inputs := []VFS{buildScriptsCheckConfigHPy}
		inputs = append(inputs, walkClosure(ctx.scannerFor(instance), confVFS, in.ScanCfg)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		// Platform-independent codegen (fixed python, no toolchain flags);
		// upstream attributes such a command to the target platform (a cross
		// build shows the target ISA, never the host/tool ISA). Emit under
		// ctx.target so target and host instances collapse by uid. The
		// per-platform compile (emitCC below) keeps its instance platform.
		chRef := ctx.emit.emit(&Node{
			Platform: ctx.target,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.strList(d.tc.Python3,
				argSBuildScriptsCheckConfigHPy.str(),
				internStr(instance.Path.rel()+"/"+conf.string()),
				(generatedVFS).str())),
				Env: env}),
			Env:              env,
			Inputs:           na.inputList(inputs),
			Outputs:          na.vfsList(generatedVFS),
			KV:               KV{P: pkCH, PC: pcYellow},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
			Resources:        usesPython3,
		})

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{chRef}
		// CC input window: the generated source leads.
		ccIn.IncludeInputs = append([]VFS{generatedVFS}, inputs...)

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}
