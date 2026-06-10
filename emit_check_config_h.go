package main

import (
	"path"
	"strings"
)

func emitCheckConfigH(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) []*sourceEmit {
	if len(d.checkConfigHeaders) == 0 {
		return nil
	}

	out := make([]*sourceEmit, 0, len(d.checkConfigHeaders))

	for _, conf := range d.checkConfigHeaders {
		confBase := strings.TrimSuffix(path.Base(conf), path.Ext(conf))
		generated := confBase + ".config.cpp"
		generatedVFS := Build(instance.Path + "/" + generated)

		confVFS := Source(instance.Path + "/" + conf)
		inputs := []VFS{buildScriptsCheckConfigHPy, confVFS}
		inputs = append(inputs, walkClosure(ctx, instance, confVFS, in)...)

		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		chRef, ok := ctx.checkConfigOutputs[generatedVFS]

		if !ok {
			chRef = ctx.emit.Emit(&Node{
				Platform: instance.Platform,
				Cmds: []Cmd{
					{
						CmdArgs: []STR{
							d.tc.Python3,
							argSBuildScriptsCheckConfigHPy.str(),
							internStr(instance.Path + "/" + conf),
							(generatedVFS).str(),
						},
						Env: env,
					},
				},
				Env:              env,
				Inputs:           inputs,
				Outputs:          []VFS{generatedVFS},
				KV:               KV{P: pkCH, PC: pcYellow},
				Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
				TargetProperties: TargetProperties{ModuleDir: instance.Path},
				usesResources:    []string{resourcePatternYMakePython3},
			})
			ctx.checkConfigOutputs[generatedVFS] = chRef
		}

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{chRef}
		ccIn.IncludeInputs = inputs

		ccRef, ccOut, _ := EmitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &sourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}
