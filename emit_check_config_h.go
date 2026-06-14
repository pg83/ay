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

		// check_config_h.py is platform-independent codegen (fixed python, no
		// toolchain flags), so upstream emits the producing command once — as a
		// plain, non-tool node — even when the owning library is also built for the
		// host (tool) platform. The two instances that reach it differ only by the
		// host platform's "tool" tag; pin Tags to empty so both emit byte-identical
		// nodes that collapse by uid. The per-platform compile of the generated
		// source (emitCC below) keeps its instance platform/tags.
		chRef := ctx.emit.emit(&Node{
			Platform: instance.Platform,
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
			Tags:             []STR{},
			Resources:        usesPython3,
		})

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{chRef}
		// IncludeInputs is the CC input window: the generated source leads.
		ccIn.IncludeInputs = append([]VFS{generatedVFS}, inputs...)

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}
