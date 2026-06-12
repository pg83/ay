package main

import (
	"path/filepath"
	"strings"
)

// swigAddIncls mirrors Lib/python's ADDINCL(GLOBAL FOR swig …) declarations —
// the python contour is the one ay models (swig.conf _SWIG_PYTHON_C/_CPP).
var swigAddIncls = []VFS{source(swigLibRoot + "/python"), source(swigLibRoot)}

// swigConstArgs is the constant span of every SW command between the swig
// binary and the per-statement module/interface/output tail.
var swigConstArgs = []STR{
	argIB.str(),
	argIS.str(),
	argISContribToolsSwigLibPython.str(),
	argISContribToolsSwigLib.str(),
	argPython.str(),
	argModule.str(),
}

type SwigSrc struct {
	Src    string
	Module string
}

const swigLibRoot = "contrib/tools/swig/Lib"

func emitSwigC(ctx *GenCtx, instance ModuleInstance, d *ModuleData, in ModuleCCInputs) []*SourceEmit {
	if len(d.swigC) == 0 {
		return nil
	}

	swigRef, swigBin := swigTool(ctx, instance)

	out := make([]*SourceEmit, 0, len(d.swigC))

	for _, stmt := range d.swigC {
		prefix := swigOutputPrefix(stmt.Src, stmt.Module)
		cOutRel := prefix + ".swg.c"
		pyOutRel := prefix + ".py"
		srcVFS := source(instance.Path.rel() + "/" + stmt.Src)
		cOutVFS := build(instance.Path.rel() + "/" + cOutRel)
		pyOutVFS := build(instance.Path.rel() + "/" + pyOutRel)
		// The window walk: implicit %includes are the swig parser's own
		// directives, the Lib dirs are FOR-swig addincl data, the resolution
		// is the scanner's standard one (sysincl swig.yml included).
		swigClosure := walkClosureTail(ctx, instance, srcVFS, ModuleCCInputs{AddIncl: swigAddIncls, RootParser: SwigIncludeDirectiveParser{}})

		// swigClosure joins as its own chunk (referenced, not copied; read-only
		// after this — the later consumers copy out of it).
		inputs := InputChunks{{bldContribToolsSwigSwig, srcVFS}, swigClosure}

		cmdArgs := ArgChunks{
			{internStr(swigBin)},
			swigConstArgs,
			{
				internStr(swigModuleName(stmt.Module)),
				argInterface.str(),
				internStr(swigModuleName(stmt.Module) + "_swg"),
				argDashO.str(),
				(cOutVFS).str(),
				(srcVFS).str(),
			},
		}

		swRef := ctx.emit.emit(&Node{
			Platform: instance.Platform,
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
				},
			},
			DepRefs:          []NodeRef{swigRef},
			Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
			Inputs:           inputs,
			Outputs:          []VFS{cOutVFS, pyOutVFS},
			KV:               KV{P: pkSW, PC: pcYellow},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		})

		if d.pyGeneratedSrcs == nil {
			d.pyGeneratedSrcs = make(map[STR][]VFS)
		}

		d.pySrcs = append(d.pySrcs, internStr(pyOutRel))
		d.pyGeneratedSrcs[internStr(pyOutRel)] = append([]VFS{cOutVFS, srcVFS}, swigClosure...)
		registerBoundGeneratedParsedOutput(ctx, instance, pkSW, cOutVFS, collectSwigInducedIncludes(ctx, srcVFS, swigClosure), swRef, []NodeRef{swigRef})
		registerBoundGeneratedParsedOutput(ctx, instance, pkSW, pyOutVFS, nil, swRef, []NodeRef{swigRef})

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{swRef}
		cClosure := walkClosure(ctx, instance, cOutVFS, in)
		incl := make([]VFS, 0, len(cClosure)+len(swigClosure)+1)
		incl = append(incl, cClosure...)
		incl = append(incl, swigClosure...)
		incl = append(incl, srcVFS)
		ccIn.IncludeInputs = swigFilterExistingSources(ctx.fs, dedupVFS(incl))

		ccRef, ccOut, _ := emitCC(instance, cOutRel, cOutVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

func swigTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool(argContribToolsSwig)

	return ref, bin.string()
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

func collectSwigInducedIncludes(ctx *GenCtx, src VFS, closure []VFS) []IncludeDirective {
	// Every closure member was parsed during the walk, so the bucket reads
	// below are pure cache hits — the parse path (and its
	// deduper use) cannot run, and the shared deduper may host this set.
	deduper.reset()

	swigParser := IncludeDirectiveParser(SwigIncludeDirectiveParser{})
	var out []IncludeDirective

	add := func(v VFS) {
		for _, d := range ctx.parsers.sourceParsedBuckets(v, swigParser).bucket(parsedIncludesCpp) {
			if !deduper.add(directiveID(d)) {
				continue
			}

			out = append(out, d)
		}
	}

	add(src)

	for _, v := range closure {
		add(v)
	}

	return out
}

func swigResolveCandidates(fs FS, target, incRel string, roots []string) []string {
	if fs == nil {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(roots))
	add := func(rel string) {
		rel = cleanRel(filepath.ToSlash(filepath.Clean(rel)))

		if rel == "" || !fs.isFile(srcRootVFS, rel) {
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

func swigFilterExistingSources(fs FS, in []VFS) []VFS {
	if fs == nil {
		return in
	}

	out := make([]VFS, 0, len(in))

	for _, v := range in {
		if v.isSource() && !fs.isFile(srcRootVFS, v.rel()) {
			continue
		}

		out = append(out, v)
	}

	return out
}
