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

var swigImplicitIncludes = []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg"}

const swigLibRoot = "contrib/tools/swig/Lib"

func emitSwigC(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) []*sourceEmit {
	if len(d.swigC) == 0 {
		return nil
	}

	swigRef, swigBin := swigTool(ctx, instance)

	out := make([]*sourceEmit, 0, len(d.swigC))
	for _, stmt := range d.swigC {
		prefix := swigOutputPrefix(stmt.Src, stmt.Module)
		cOutRel := prefix + ".swg.c"
		pyOutRel := prefix + ".py"
		srcVFS := Source(instance.Path + "/" + stmt.Src)
		cOutVFS := Build(instance.Path + "/" + cOutRel)
		pyOutVFS := Build(instance.Path + "/" + pyOutRel)
		swigClosure := swigIncludeClosure(ctx, srcVFS)

		inputs := make([]VFS, 0, 2+len(swigClosure))
		inputs = append(inputs, Build("contrib/tools/swig/swig"), srcVFS)
		inputs = append(inputs, swigClosure...)

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
			KV: map[string]interface{}{
				"p":  "SW",
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
		})
		if d.pyGeneratedSrcs == nil {
			d.pyGeneratedSrcs = make(map[string][]VFS)
		}
		d.pySrcs = append(d.pySrcs, pyOutRel)
		d.pyGeneratedSrcs[pyOutRel] = append([]VFS{cOutVFS, srcVFS}, swigClosure...)
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", cOutVFS, collectSwigInducedIncludes(ctx, srcVFS, swigClosure), swRef)
		registerBoundGeneratedParsedOutput(ctx, instance, "SW", pyOutVFS, nil, swRef)

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{swRef}
		cClosure := walkClosureWithSourceRel(ctx, instance, cOutVFS, srcVFS.Rel, in)
		incl := make([]VFS, 0, len(cClosure)+len(swigClosure)+1)
		incl = append(incl, cClosure...)
		incl = append(incl, swigClosure...)
		incl = append(incl, srcVFS)
		ccIn.IncludeInputs = swigFilterExistingSources(ctx.fs, dedupVFS(incl))

		ccRef, ccOut := EmitCC(instance, cOutRel, cOutVFS, ccIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{cOutVFS}, ccIn.IncludeInputs...)

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

func swigIncludeClosure(ctx *genCtx, src VFS) []VFS {
	if ctx == nil || ctx.fs == nil {
		return nil
	}

	roots := swigSearchRoots(ctx.fs)
	seen := map[string]struct{}{}
	var queue []string

	enqueue := func(target string, kind includeKind, incRel string) {
		candidates := swigResolveCandidates(ctx.fs, target, incRel, roots)
		if kind == includeQuoted {
			if len(candidates) > 0 {
				queue = append(queue, candidates[0])
			}
			return
		}
		queue = append(queue, candidates...)
	}

	for _, imp := range swigImplicitIncludes {
		enqueue(imp, includeSystem, src.Rel)
	}
	for _, d := range swigSourceParsedBuckets(ctx, src.Rel).bucket(parsedIncludesLocal) {
		enqueue(d.target, d.kind, src.Rel)
	}

	for len(queue) > 0 {
		rel := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		for _, d := range swigSourceParsedBuckets(ctx, rel).bucket(parsedIncludesLocal) {
			enqueue(d.target, d.kind, rel)
		}
	}

	order := make([]string, 0, len(seen))
	for rel := range seen {
		order = append(order, rel)
	}
	sort.Strings(order)

	out := make([]VFS, 0, len(order))
	for _, rel := range order {
		out = append(out, Source(rel))
	}

	return out
}

func collectSwigInducedIncludes(ctx *genCtx, src VFS, closure []VFS) []includeDirective {
	seen := map[includeDirective]struct{}{}
	var out []includeDirective

	add := func(rel string) {
		for _, d := range swigSourceParsedBuckets(ctx, rel).bucket(parsedIncludesHCPP) {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			out = append(out, d)
		}
	}

	add(src.Rel)
	for _, v := range closure {
		add(v.Rel)
	}

	return out
}

func swigSearchRoots(fs *FS) []string {
	if fs == nil {
		return nil
	}

	roots := []string{swigLibRoot}
	entries := fs.Listdir(swigLibRoot)
	if len(entries) == 0 {
		return roots
	}

	var subdirs []string
	for name, isDir := range entries {
		if !isDir {
			continue
		}
		subdirs = append(subdirs, filepath.ToSlash(filepath.Clean(swigLibRoot+"/"+name)))
	}
	sort.Strings(subdirs)

	return append(roots, subdirs...)
}

func swigResolveCandidates(fs *FS, target, incRel string, roots []string) []string {
	if fs == nil {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(roots))
	add := func(rel string) {
		rel = cleanRel(filepath.ToSlash(filepath.Clean(rel)))
		if rel == "" || !fs.IsFile(rel) {
			return
		}
		if _, ok := seen[rel]; ok {
			return
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}

	dir := filepath.ToSlash(filepath.Dir(incRel))
	if dir == "." {
		dir = ""
	}
	if dir != "" {
		add(dir + "/" + target)
	} else {
		add(target)
	}
	for _, root := range roots {
		add(root + "/" + target)
	}

	return out
}

func swigSourceParsedBuckets(ctx *genCtx, rel string) parsedIncludeSet {
	if ctx == nil || ctx.fs == nil {
		return nil
	}

	data, err := ctx.fs.Read(rel)
	if err != nil {
		return nil
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	// Keep SWIG's `.i` parsing local to the emitter path: the shared parser
	// registry intentionally leaves `.i` on the default C-like scanner.
	return swigIncludeDirectiveParser{}.Parse(rel, data)
}

func swigFilterExistingSources(fs *FS, in []VFS) []VFS {
	if fs == nil {
		return in
	}

	out := make([]VFS, 0, len(in))
	for _, v := range in {
		if v.IsSource() && !fs.IsFile(v.Rel) {
			continue
		}
		out = append(out, v)
	}

	return out
}
