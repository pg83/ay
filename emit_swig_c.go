package main

import (
	"path/filepath"
	"sort"
	"strings"
)

type swigSrc struct {
	Src    string
	Module string
}

func emitSwigC(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) []*sourceEmit {
	if len(d.swigC) == 0 {
		return nil
	}

	swigRef, swigBin := swigTool(ctx, instance)
	libInputs := swigLibInputs(ctx.fs)

	out := make([]*sourceEmit, 0, len(d.swigC))
	for _, stmt := range d.swigC {
		prefix := swigOutputPrefix(stmt.Src, stmt.Module)
		cOutRel := prefix + ".swg.c"
		pyOutRel := prefix + ".py"
		srcVFS := Source(instance.Path + "/" + stmt.Src)
		cOutVFS := Build(instance.Path + "/" + cOutRel)
		pyOutVFS := Build(instance.Path + "/" + pyOutRel)

		inputs := make([]VFS, 0, 2+len(libInputs))
		inputs = append(inputs, Build("contrib/tools/swig/swig"), srcVFS)
		inputs = append(inputs, libInputs...)

		cmdArgs := []string{
			swigBin,
			"-I$(B)",
			"-I$(S)",
			"-I$(S)/contrib/tools/swig/Lib/python",
			"-I$(S)/contrib/tools/swig/Lib",
			"-python",
			"-module",
			swigModuleName(stmt.Module),
			"-interface",
			swigModuleName(stmt.Module) + "_swg",
			"-o",
			cOutVFS.String(),
			srcVFS.String(),
		}

		swRef := ctx.emit.Emit(&Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env: map[string]string{
						"ARCADIA_ROOT_DISTBUILD": "$(S)",
					},
				},
			},
			DepRefs: []NodeRef{swigRef},
			Env:     map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"},
			Inputs:  inputs,
			Outputs: []VFS{cOutVFS, pyOutVFS},
			KV: map[string]string{
				"p":  "SW",
				"pc": "yellow",
			},
			Platform:     string(instance.Platform.Target),
			HostPlatform: instance.Platform.IsHost,
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
			Tags: instance.Platform.Tags,
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
		})
		if d.pyGeneratedSrcs == nil {
			d.pyGeneratedSrcs = make(map[string][]VFS)
		}
		d.pySrcs = append(d.pySrcs, pyOutRel)
		d.pyGeneratedSrcs[pyOutRel] = append([]VFS{cOutVFS, srcVFS}, libInputs...)
		var cParsed []includeDirective
		if scanner := ctx.scannerFor(instance); scanner != nil {
			cParsed = scanner.parsers.sourceParsedBuckets(srcVFS.Rel).bucket(parsedIncludesHCPP)
		}
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", cOutVFS, cParsed, swRef)
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", pyOutVFS, nil, swRef)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{swRef}
		ccIn.IncludeInputs = inputs

		ccRef, ccOut := EmitCC(instance, cOutRel, cOutVFS, ccIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{cOutVFS}, inputs...)

		out = append(out, &sourceEmit{
			Ref:          ccRef,
			OutPath:      ccOut,
			CcIns:        ccInputs,
			PrimaryCount: 1,
		})
	}

	return out
}

func swigTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/swig")
	return ref, bin.String()
}

func swigOutputPrefix(src, module string) string {
	dir := filepath.ToSlash(filepath.Dir(src))
	if dir == "." {
		return swigModuleName(module)
	}

	return dir + "/" + swigModuleName(module)
}

func swigModuleName(module string) string {
	if dot := strings.LastIndexByte(module, '.'); dot >= 0 {
		return module[dot+1:]
	}

	return module
}

func swigLibInputs(fs *FS) []VFS {
	var rels []string
	fs.Walk("contrib/tools/swig/Lib", func(rel string, isDir bool) {
		if isDir {
			return
		}
		rels = append(rels, rel)
	})
	sort.Strings(rels)

	out := make([]VFS, 0, len(rels))
	for _, rel := range rels {
		out = append(out, Source(rel))
	}

	return out
}
