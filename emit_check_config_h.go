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

		env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

		chRef, ok := ctx.checkConfigOutputs[generatedVFS]

		if !ok {
			chRef = ctx.emit.Emit(bindNodePlatform(withResources(&Node{
				Cmds: []Cmd{
					{
						CmdArgs: []string{
							instance.Platform.Tools.Python3,
							"$(S)/build/scripts/check_config_h.py",
							instance.Path + "/" + conf,
							generatedVFS.String(),
						},
						Env: env,
					},
				},
				Env:              env,
				Inputs:           inputs,
				Outputs:          []VFS{generatedVFS},
				KV:               KV{P: pkCH, PC: pcYellow},
				Platform:         string(instance.Platform.Target),
				Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
				Tags:             instance.Platform.Tags,
				TargetProperties: TargetProperties{ModuleDir: instance.Path},
			}, resourcePatternYMakePython3), instance.Platform))
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

// Path constants hoisted by `ay refac consts`.
var (
	buildScriptsCheckConfigHPy = Source("build/scripts/check_config_h.py")
)
