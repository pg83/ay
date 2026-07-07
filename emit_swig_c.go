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

func (e *EmitContext) emitSwigC() {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na

	if len(d.swigC) == 0 {
		return
	}

	swigRef, swigBin := swigTool(ctx, instance)

	for _, stmt := range d.swigC {
		prefix := swigOutputPrefix(stmt.Src, stmt.Module)
		cOutRel := prefix + ".swg.c"
		pyOutRel := prefix + ".py"
		srcVFS := source(instance.Path.relString(), "/", stmt.Src)
		cOutVFS := build(instance.Path.relString(), "/", cOutRel)
		pyOutVFS := build(instance.Path.relString(), "/", pyOutRel)
		cv := walkClosure(e.scanner, srcVFS, newScanContext(ctx.parsers, swigAddIncls, nil, includeScannerBasePaths(), instance.Path.relString()))
		inputs := na.inputList(na.vfsList(bldContribToolsSwigSwig, srcVFS), cv.buckets...)
		swigClosure := collectBucketVFS(cv.buckets, func(VFS) bool { return true })

		cmdArgs := na.chunkListSTR(na.strList(swigBin.fullSTR()), swigConstArgs, na.strList(internStr(swigModuleName(stmt.Module)),
			argInterface.str(),
			internV(swigModuleName(stmt.Module), "_swg"),
			argDashO.str(),
			(cOutVFS).fullSTR(),
			(srcVFS).fullSTR()))

		swRef := ctx.emit.emitNode(Node{
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

		reg := e.codegen

		var psc []ARG

		if p := d.perSrcCFlagsFor(cOutVFS.fullSTR()); p != nil {
			psc = *p
		}

		reg.register(&GeneratedFileInfo{
			OutputPath:     cOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  []NodeRef{swigRef},
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: collectSwigInducedIncludes(ctx, srcVFS, swigClosure)},
			ClosureLeaves:  append(append([]VFS{}, swigClosure...), srcVFS),
			Compile:        &CompileSpec{FlatOutput: d.flatSrc(cOutVFS.fullSTR()), CFlags: psc},
		})

		reg.register(&GeneratedFileInfo{
			OutputPath:    pyOutVFS,
			ProducerRef:   swRef,
			GeneratorRefs: []NodeRef{swigRef},
			SourceInputs:  append([]VFS{cOutVFS, srcVFS}, swigClosure...),
		})

		e.pySrcsReg = append(e.pySrcsReg, PySrc{
			Path:   pyOutVFS,
			Module: internStr(generatedPyResourceKey(instance.Path.relString(), d, pyOutRel)),
			Token:  internV("${ARCADIA_BUILD_ROOT}/", pyOutVFS.relString()),
			Group:  pyGroupGenAux,
		})

		e.enqueueSrc(SrcMeta{Source: cOutVFS.fullSTR(), Prio: stmtPrioDefault, Generated: true, Bucket: bkSwig})
	}
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
