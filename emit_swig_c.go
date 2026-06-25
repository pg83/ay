package main

import (
	"path/filepath"
	"strings"
)

var (
	swigAddIncls = []VFS{source(swigLibRoot + "/python"), source(swigLibRoot)}
	swigCKV      = KV{P: pkSW, PC: pcYellow}
)

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

		swigClosure := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, swigAddIncls, nil, includeScannerBasePaths(), instance.Path.rel()))

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
			DepRefs:      []NodeRef{swigRef},
			Env:          EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}},
			Inputs:       inputs,
			Outputs:      na.vfsList(cOutVFS, pyOutVFS),
			KV:           &swigCKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		})

		d.pySrcs = append(d.pySrcs, internStr(pyOutRel))

		d.pySrcsFullName = append(d.pySrcsFullName, true)
		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:    pkSW,
			OutputPath:     cOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  []NodeRef{swigRef},
			ParsedIncludes: collectSwigInducedIncludes(ctx, srcVFS, swigClosure),
		})
		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:    pkSW,
			OutputPath:     pyOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  []NodeRef{swigRef},
			ParsedIncludes: nil,
		})

		ctx.codegenFor(instance).setSourceInputs(pyOutVFS, append([]VFS{cOutVFS, srcVFS}, swigClosure...))

		ccIn := in
		ccIn.ExtraDepRefs = []NodeRef{swRef}
		cClosure := walkClosure(ctx.scannerFor(instance), cOutVFS, in.ScanCfg)
		incl := make([]VFS, 0, len(cClosure)+len(swigClosure)+1)
		incl = append(incl, cClosure...)
		incl = append(incl, swigClosure...)
		incl = append(incl, srcVFS)
		ccIn.IncludeInputs = dedupVFS(incl)

		ccRef, ccOut, _ := emitCC(instance, internStr(cOutRel), cOutVFS, ccIn, ctx.host, ctx.emit)
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
