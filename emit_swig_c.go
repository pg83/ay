package main

import (
	"path/filepath"
	"strings"
)

// swigAddIncls mirrors the python Lib's ADDINCL(GLOBAL FOR swig …) declarations.
var swigAddIncls = []VFS{source(swigLibRoot + "/python"), source(swigLibRoot)}

// swigConstArgs is the constant span between the swig binary and the
// per-statement tail.
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
	na := ctx.na

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
		// implicit %includes come from the swig parser; the Lib dirs are FOR-swig
		// addincl data.
		swigClosure := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, swigAddIncls, nil, includeScannerBasePaths(), instance.Path.rel()))

		// swigClosure joins as its own chunk: referenced, not copied.
		inputs := na.inputList(na.vfsList(bldContribToolsSwigSwig, srcVFS), swigClosure)

		cmdArgs := na.chunkList(na.strList(swigBin.str()), swigConstArgs, na.strList(internStr(swigModuleName(stmt.Module)),
			argInterface.str(),
			internStr(swigModuleName(stmt.Module)+"_swg"),
			argDashO.str(),
			(cOutVFS).str(),
			(srcVFS).str()))

		swRef := ctx.emit.emit(&Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
				Env: EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}}),
			DepRefs:          []NodeRef{swigRef},
			Env:              EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
			Inputs:           inputs,
			Outputs:          na.vfsList(cOutVFS, pyOutVFS),
			KV:               KV{P: pkSW, PC: pcYellow},
			Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		})

		d.pySrcs = append(d.pySrcs, internStr(pyOutRel))
		// the generated .py is a build-root-relative PY_SRCS token, so its py3cc
		// module name is the full root-relative path.
		d.pySrcsFullName = append(d.pySrcsFullName, true)
		registerBoundGeneratedParsedOutput(ctx, instance, pkSW, cOutVFS, collectSwigInducedIncludes(ctx, srcVFS, swigClosure), swRef, []NodeRef{swigRef})
		registerBoundGeneratedParsedOutput(ctx, instance, pkSW, pyOutVFS, nil, swRef, []NodeRef{swigRef})
		// the .py's build-from closure reaches its py-bytecode consumers through
		// the registry's SourceInputs, not a per-module side map.
		codegenRegForInstance(ctx, instance).setSourceInputs(pyOutVFS, append([]VFS{cOutVFS, srcVFS}, swigClosure...))

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{swRef}
		cClosure := walkClosure(ctx.scannerFor(instance), cOutVFS, in.ScanCfg)
		incl := make([]VFS, 0, len(cClosure)+len(swigClosure)+1)
		incl = append(incl, cClosure...)
		incl = append(incl, swigClosure...)
		incl = append(incl, srcVFS)
		ccIn.IncludeInputs = dedupVFS(incl)

		ccRef, ccOut, _ := emitCC(instance, cOutRel, cOutVFS, ccIn, ctx.host, ctx.emit)
		out = append(out, &SourceEmit{Ref: ccRef, OutPath: ccOut})
	}

	return out
}

func swigTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, VFS) {
	return ctx.tool(argContribToolsSwig)
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
	// every closure member was parsed during the walk, so the bucket reads are
	// cache hits.
	swigParser := IncludeDirectiveParser(SwigIncludeDirectiveParser{})
	var out []IncludeDirective

	add := func(v VFS) {
		out = append(out, ctx.parsers.sourceParsedBuckets(v, swigParser).bucket(parsedIncludesCpp)...)
	}

	add(src)

	for _, v := range closure {
		add(v)
	}

	return out
}
