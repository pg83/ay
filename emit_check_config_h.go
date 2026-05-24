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
		inputs := []VFS{Source("build/scripts/check_config_h.py"), confVFS}
		inputs = append(inputs, walkClosure(ctx, instance, confVFS, in)...)

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		}

		chRef, ok := ctx.checkConfigOutputs[generatedVFS]
		if !ok {
			chRef = ctx.emit.Emit(bindNodePlatform(&Node{
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
				Env:     env,
				Inputs:  inputs,
				Outputs: []VFS{generatedVFS},
				KV: map[string]interface{}{
					"p":  "CH",
					"pc": "yellow",
				},
				Platform: string(instance.Platform.Target),
				Requirements: map[string]interface{}{
					"cpu":     float64(1),
					"network": "restricted",
					"ram":     float64(32),
				},
				Tags: instance.Platform.Tags,
				TargetProperties: map[string]string{
					"module_dir": instance.Path,
				},
			}, instance.Platform))
			ctx.checkConfigOutputs[generatedVFS] = chRef
		}

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{chRef}
		ccIn.IncludeInputs = inputs

		ccRef, ccOut, _ := EmitCC(instance, generated, generatedVFS, ccIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{generatedVFS}, inputs...)

		out = append(out, &sourceEmit{
			Ref:          ccRef,
			OutPath:      ccOut,
			CcIns:        ccInputs,
			PrimaryCount: 1,
		})
	}

	return out
}
