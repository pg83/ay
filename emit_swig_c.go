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
		inputs := na.inputs.alloc(2 + len(cv.bucketList()))[:2+len(cv.bucketList())]

		inputs[0] = na.vfsList(bldContribToolsSwigSwig)
		inputs[1] = na.vfsList(srcVFS)
		copy(inputs[2:], cv.bucketList())
		na.inputs.commit(len(inputs))
		inputChunks := InputChunks(inputs[:len(inputs):len(inputs)])
		swigClosure := collectBucketVFS(ctx.na, cv.bucketList(), func(VFS) bool { return true })

		swRef := ctx.emit.reserve()
		moduleName := swigModuleName(stmt.Module)

		pe := func() {
			cmdArgs := na.chunkList(na.anyList(swigBin.any()), swigConstArgs, na.anyList(internStr(moduleName).any(),
				argInterface.any(),
				internV(moduleName, "_swg").any(),
				argDashO.any(),
				cOutVFS.any(),
				srcVFS.any()))

			e.emitReservedNode(Node{
				Platform: instance.Platform,
				Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
					Env: envVarsVCS}),
				DepRefs: na.refList(swigRef),
				Env:     envVarsVCS,
				Inputs:  inputChunks,
				Outputs: na.vfsList(cOutVFS, pyOutVFS),
				KV:      &swigCKV,
			}, swRef)
		}
		pending := e.ctx.na.pendingEmit(pe)

		e.register(GeneratedFileInfo{
			OutputPath:     cOutVFS,
			ProducerRef:    swRef,
			GeneratorRefs:  e.ctx.na.refList(swigRef),
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: collectSwigInducedIncludes(e.scanner, srcVFS, swigClosure)},
			ClosureLeaves:  append(append([]VFS{}, swigClosure...), srcVFS),
			OnUse:          pending,
		})

		swigSourceInputs := na.vfs.alloc(1 + len(swigClosure))[:0]

		swigSourceInputs = append(swigSourceInputs, srcVFS)

		for _, bucket := range cv.bucketList() {
			if bucket[0].isSource() {
				swigSourceInputs = append(swigSourceInputs, bucket...)
			}
		}

		na.vfs.commit(len(swigSourceInputs))

		swigSourceInputs = swigSourceInputs[:len(swigSourceInputs):len(swigSourceInputs)]

		e.register(GeneratedFileInfo{
			OutputPath:    pyOutVFS,
			ProducerRef:   swRef,
			GeneratorRefs: e.ctx.na.refList(swigRef),
			SourceInputs:  swigSourceInputs,
			OnUse:         pending,
		})

		e.enqueueSrc(SrcMeta{
			Source: pyOutVFS.any(), Prio: stmtPrioDefault,
			PyMeta: e.addPyMeta(PySourceMeta{
				Module:      internStr(generatedPyResourceKey(instance.Path.relString(), d, pyOutRel)),
				Token:       internV("${ARCADIA_BUILD_ROOT}/", pyOutVFS.relString()).any(),
				ExtraInputs: na.vfsList(cOutVFS),
				Kind:        pySourceGenerated,
			}),
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

func collectSwigInducedIncludes(scanner *IncludeScanner, src VFS, closure []VFS) []IncludeDirective {
	swigParser := IncludeDirectiveParser(SwigIncludeDirectiveParser{})

	var out []IncludeDirective

	add := func(v VFS) {
		out = append(out, scanner.parsedBucketForInput(v, parsedIncludesCpp, swigParser)...)
	}

	add(src)

	for _, v := range closure {
		add(v)
	}

	return out
}
