package main

import (
	"path/filepath"
	"strings"
)

var (
	swigAddIncls = []VFS{source(swigLibRoot, "/python"), source(swigLibRoot)}
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

const swigLibRoot = "contrib/tools/swig/Lib"

type SwigSrc struct {
	Src    string
	Module string
}

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
		srcVFS := source(instance.Path.rel(), "/", stmt.Src)
		cOutVFS := build(instance.Path.rel(), "/", cOutRel)
		pyOutVFS := build(instance.Path.rel(), "/", pyOutRel)
		swigClosure := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, swigAddIncls, nil, includeScannerBasePaths(), instance.Path.rel()))
		inputs := na.inputList(na.vfsList(bldContribToolsSwigSwig, srcVFS), swigClosure)

		cmdArgs := na.chunkList(na.strList(swigBin.str()), swigConstArgs, na.strList(internStr(swigModuleName(stmt.Module)),
			argInterface.str(),
			internV(swigModuleName(stmt.Module), "_swg"),
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

		reg := ctx.codegenFor(instance)

		reg.register(&GeneratedFileInfo{
			OutputPath:     cOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  []NodeRef{swigRef},
			ParsedIncludes: collectSwigInducedIncludes(ctx, srcVFS, swigClosure),
			ClosureLeaves:  append(append([]VFS{}, swigClosure...), srcVFS),
			Compile:        &CompileSpec{FlatOutput: in.FlatOutput, CFlags: in.PerSourceCFlags},
		})

		reg.register(&GeneratedFileInfo{
			OutputPath:    pyOutVFS,
			ProducerRef:   swRef,
			GeneratorRefs: []NodeRef{swigRef},
			SourceInputs:  append([]VFS{cOutVFS, srcVFS}, swigClosure...),
		})

		if se := emitOneSource(ctx, instance, d, cOutVFS.str(), in); se != nil {
			out = append(out, se)
		}
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
