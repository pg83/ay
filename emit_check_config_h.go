package main

import (
	"path"
	"strings"
)

func emitCheckConfigH(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []*SourceEmit {
	if len(d.checkConfigHeaders) == 0 {
		return nil
	}

	out := make([]*SourceEmit, 0, len(d.checkConfigHeaders))

	for _, conf := range d.checkConfigHeaders {
		confBase := strings.TrimSuffix(path.Base(conf), path.Ext(conf))
		generated := confBase + ".config.cpp"
		generatedVFS := build(instance.Path.rel() + "/" + generated)

		confVFS := source(instance.Path.rel() + "/" + conf)
		// The walk window leads with confVFS itself — no separate prepend.
		inputs := []VFS{buildScriptsCheckConfigHPy}
		inputs = append(inputs, walkClosure(ctx, instance, confVFS, in)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		chRef, ok := ctx.checkConfigOutputs[generatedVFS]

		if !ok {
			chRef = ctx.emit.emit(&Node{
				Platform: instance.Platform,
				Cmds: []Cmd{
					{
						CmdArgs: ArgChunks{[]STR{
							d.tc.Python3,
							argSBuildScriptsCheckConfigHPy.str(),
							internStr(instance.Path.rel() + "/" + conf),
							(generatedVFS).str(),
						}},
						Env: env,
					},
				},
				Env:              env,
				Inputs:           InputChunks{inputs},
				Outputs:          []VFS{generatedVFS},
				KV:               KV{P: pkCH, PC: pcYellow},
				Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
				usesResources:    []string{resourcePatternYMakePython3},
			})
			ctx.checkConfigOutputs[generatedVFS] = chRef
		}

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{chRef}
		// IncludeInputs is the CC input window: the generated source leads.
		ccIn.IncludeInputs = append([]VFS{generatedVFS}, inputs...)

		ccRef, ccOut, _ := emitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}
