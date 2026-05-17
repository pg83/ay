package main

import (
	"os"
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
	libInputs := swigLibInputs(ctx.sourceRoot)

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
			cParsed = scanner.parsers.sourceParsedBuckets(srcVFS).bucket(parsedIncludesHCPP)
		}
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", cOutVFS, cParsed, swRef)
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", pyOutVFS, nil, swRef)

		ccIn := in
		ccIn.IsGenerated = true
		ccIn.HasGenerator = true
		ccIn.Generator = swRef
		ccIn.IncludeInputs = inputs

		ccRef, ccOut := EmitCC(instance, cOutRel, ccIn, ctx.host, ctx.emit)
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
	const swigPath = "contrib/tools/swig"

	swigInstance := NewToolInstance(ctx.host, swigPath)
	swigInstance.Flags = inferFlagsFromPath(swigPath, true)
	res := genModule(ctx, swigInstance)
	if res.LDPath != nil {
		return res.LDRef, res.LDPath.String()
	}

	return res.LDRef, Build("contrib/tools/swig/swig").String()
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

func swigLibInputs(sourceRoot string) []VFS {
	root := filepath.Join(sourceRoot, "contrib/tools/swig/Lib")
	var rels []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return nil
		}
		rels = append(rels, filepath.ToSlash(rel))

		return nil
	})
	sort.Strings(rels)

	out := make([]VFS, 0, len(rels))
	for _, rel := range rels {
		out = append(out, Source(rel))
	}

	return out
}
