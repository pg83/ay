package main

import (
	"path/filepath"
	"strings"
)

var (
	swigAddIncls = []VFS{source(swigLibRoot, "/python"), source(swigLibRoot)}
	swigCKV      = KV{P: pkSW, PC: pcYellow}
)

var swigConstArgs = []ANY{
	argIB.any(),
	argIS.any(),
	argISContribToolsSwigLibPython.any(),
	argISContribToolsSwigLib.any(),
	argPython.any(),
	argModule.any(),
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
		cv := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainSwig)
		inputs := na.inputList(na.vfsList(bldContribToolsSwigSwig, srcVFS), cv.buckets...)
		swigClosure := collectBucketVFS(ctx.na, cv.buckets, func(VFS) bool { return true })

		swRef := ctx.emit.reserve()
		moduleName := swigModuleName(stmt.Module)

		pe := func() {
			cmdArgs := na.chunkList(na.anyList(swigBin.any()), swigConstArgs, na.anyList(internStr(moduleName).any(),
				argInterface.any(),
				internV(moduleName, "_swg").any(),
				argDashO.any(),
				cOutVFS.any(),
				srcVFS.any()))

			ctx.emit.emitReservedNode(Node{
				Platform: instance.Platform,
				Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
					Env: envVarsVCS}),
				DepRefs:      na.refList(swigRef),
				Env:          envVarsVCS,
				Inputs:       inputs,
				Outputs:      na.vfsList(cOutVFS, pyOutVFS),
				KV:           &swigCKV,
				Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			}, swRef)
		}
		pending := e.ctx.na.pendingEmit(pe)

		e.register(GeneratedFileInfo{
			OutputPath:     cOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  e.ctx.na.refList(swigRef),
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: collectSwigInducedIncludes(ctx, srcVFS, swigClosure)},
			ClosureLeaves:  append(append([]VFS{}, swigClosure...), srcVFS),
			OnUse:          pending,
		})

		swigSourceInputs := na.vfs.alloc(2 + len(swigClosure))

		swigSourceInputs[0] = cOutVFS
		swigSourceInputs[1] = srcVFS

		swigN := 2 + copy(swigSourceInputs[2:], swigClosure)

		na.vfs.commit(swigN)

		swigSourceInputs = swigSourceInputs[:swigN:swigN]

		e.register(GeneratedFileInfo{
			OutputPath:    pyOutVFS,
			ProducerRef:   swRef,
			GeneratorRefs: e.ctx.na.refList(swigRef),
			SourceInputs:  swigSourceInputs,
			OnUse:         pending,
		})

		e.pySrcsReg = append(e.pySrcsReg, PySrc{
			Path:   pyOutVFS,
			Module: internStr(generatedPyResourceKey(instance.Path.relString(), d, pyOutRel)),
			Token:  internV("${ARCADIA_BUILD_ROOT}/", pyOutVFS.relString()).any(),
			Group:  pyGroupGenAux,
		})

		e.enqueueSrc(SrcMeta{Source: cOutVFS.any(), Prio: stmtPrioDefault, Bucket: bkSwig})
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
